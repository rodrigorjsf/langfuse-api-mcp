// Package catalog is the set of in-scope Langfuse operations the server can
// execute, read from the OpenAPI spec embedded in the binary.
//
// It is pure data: it knows operation IDs, methods, path templates and
// parameters, and builds the request an operation needs from the caller's
// parameters (ADR-0010). It never performs I/O.
//
// In M1 the catalog is the embedded spec minus the ADR-0004 exclusions. The
// version-aware union catalog of ADR-0012 replaces this source in M3.
package catalog

import (
	_ "embed" // the Langfuse OpenAPI spec is compiled into the binary
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

//go:embed spec/langfuse-openapi.json
var openAPISpec []byte

// excluded holds the operation IDs never exposed on any deployment (ADR-0004):
// trace ingestion, and organization admin mutations (projects, API keys,
// memberships, SCIM users).
var excluded = []string{
	"ingestion_batch",            // POST /api/public/ingestion
	"opentelemetry_exportTraces", // POST /api/public/otel/v1/traces
	"projects_create",
	"projects_update",
	"projects_delete",
	"projects_createApiKey",
	"projects_deleteApiKey",
	"organizations_updateOrganizationMembership",
	"organizations_deleteOrganizationMembership",
	"organizations_updateProjectMembership",
	"organizations_deleteProjectMembership",
	"scim_createUser",
	"scim_deleteUser",
}

// Catalog is the set of in-scope operations, built once at startup and shared
// read-only.
type Catalog struct {
	byID map[string]Operation
}

// Operation is one method+path pair of the Langfuse public API.
type Operation struct {
	// ID is the OpenAPI operationId.
	ID string
	// Method is the HTTP method, upper case (GET, POST, …).
	Method string
	// Path is the path template below the host, e.g. /api/public/traces/{traceId}.
	Path string
	// Params are the operation's path and query parameters.
	Params []Param
}

// Param is one path or query parameter of an operation.
type Param struct {
	Name string
	// In is "path" or "query".
	In       string
	Required bool
}

// IsRead reports whether the operation is a read operation (HTTP GET).
func (o Operation) IsRead() bool { return o.Method == http.MethodGet }

// Load builds the catalog from the embedded Langfuse OpenAPI spec, minus the
// excluded operations.
func Load() (Catalog, error) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(openAPISpec, &doc); err != nil {
		return Catalog{}, fmt.Errorf("embedded OpenAPI spec: %w", err)
	}
	cat := Catalog{byID: map[string]Operation{}}
	for path, item := range doc.Paths {
		for method, raw := range item {
			m := strings.ToUpper(method)
			if !slices.Contains([]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}, m) {
				continue // path-level keys such as "parameters"
			}
			var op struct {
				OperationID string  `json:"operationId"`
				Parameters  []Param `json:"parameters"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				return Catalog{}, fmt.Errorf("embedded OpenAPI spec: %s %s: %w", m, path, err)
			}
			if op.OperationID == "" || slices.Contains(excluded, op.OperationID) {
				continue
			}
			cat.byID[op.OperationID] = Operation{ID: op.OperationID, Method: m, Path: path, Params: op.Parameters}
		}
	}
	return cat, nil
}

// Lookup returns the in-scope operation with the given ID.
func (c Catalog) Lookup(id string) (Operation, bool) {
	op, ok := c.byID[id]
	return op, ok
}

// Operations returns every in-scope operation, sorted by ID.
func (c Catalog) Operations() []Operation {
	ops := make([]Operation, 0, len(c.byID))
	for _, op := range c.byID {
		ops = append(ops, op)
	}
	slices.SortFunc(ops, func(a, b Operation) int { return strings.Compare(a.ID, b.ID) })
	return ops
}
