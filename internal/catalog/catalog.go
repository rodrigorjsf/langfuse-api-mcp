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
	"unicode"
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
	// Tag is the operation's OpenAPI tag, the area it belongs to (Trace,
	// Prompts, Datasets…); the operation index groups by it.
	Tag string
	// DescriptionLine is the first line of the operation's OpenAPI
	// description, with invisible, bidirectional formatting and control
	// characters removed: the spec is third-party text.
	DescriptionLine string
	// Params are the operation's path and query parameters.
	Params []Param
}

// Param is one path or query parameter of an operation.
type Param struct {
	Name string
	// In is "path" or "query".
	In       string
	Required bool
	// Schema is the type the parameter's value must have.
	Schema Schema
}

// Schema is the part of a parameter's OpenAPI schema the catalog validates
// against: the type, the allowed values and, for a repeated query parameter,
// the type of each item.
type Schema struct {
	// Type is "string", "integer", "number", "boolean" or "array"; empty
	// accepts any scalar.
	Type string `json:"type"`
	// Nullable parameters accept a JSON null, which omits them.
	Nullable bool `json:"nullable"`
	// Enum lists the allowed values of a string parameter; empty allows any.
	Enum []string `json:"enum"`
	// Items is the schema of each value of an array parameter.
	Items *Schema `json:"items"`
	// Ref names a component schema (#/components/schemas/<name>); Load
	// resolves it into Type and Enum.
	Ref string `json:"$ref"`
	// Format refines Type, e.g. "date-time"; empty when the spec names none.
	Format string `json:"format"`
	// Minimum and Maximum bound a number; MinLength and MaxLength bound a
	// string's length. Nil when the spec gives no bound, except for the page
	// size of a list operation, which the catalog bounds itself (MaxLimit).
	Minimum   *float64 `json:"minimum"`
	Maximum   *float64 `json:"maximum"`
	MinLength *int     `json:"minLength"`
	MaxLength *int     `json:"maxLength"`
	// Default is the value used when the parameter is absent; nil when the
	// spec names none, DefaultLimit for the page size of a list operation.
	Default any `json:"default"`
}

// IsRead reports whether the operation is a read operation (HTTP GET).
func (o Operation) IsRead() bool { return o.Method == http.MethodGet }

// Load builds the catalog from the embedded Langfuse OpenAPI spec, minus the
// excluded operations.
func Load() (Catalog, error) { return load(openAPISpec) }

// load builds the catalog from an OpenAPI spec, minus the excluded operations.
func load(spec []byte) (Catalog, error) {
	var doc struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]Schema `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec, &doc); err != nil {
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
				OperationID string   `json:"operationId"`
				Tags        []string `json:"tags"`
				Description string   `json:"description"`
				Parameters  []Param  `json:"parameters"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				return Catalog{}, fmt.Errorf("embedded OpenAPI spec: %s %s: %w", m, path, err)
			}
			if op.OperationID == "" || slices.Contains(excluded, op.OperationID) {
				continue
			}
			for i := range op.Parameters {
				if err := resolve(&op.Parameters[i].Schema, doc.Components.Schemas); err != nil {
					return Catalog{}, fmt.Errorf("embedded OpenAPI spec: %s parameter %s: %w",
						op.OperationID, op.Parameters[i].Name, err)
				}
			}
			o := Operation{ID: op.OperationID, Method: m, Path: path, Params: op.Parameters}
			if len(op.Tags) > 0 {
				o.Tag = visible(op.Tags[0])
			}
			line, _, _ := strings.Cut(strings.TrimSpace(op.Description), "\n")
			o.DescriptionLine = strings.TrimSpace(visible(line))
			for i, p := range o.Params {
				if o.isListLimit(p) {
					// The catalog bounds the page size itself (Request).
					o.Params[i].Schema.Minimum, o.Params[i].Schema.Maximum = new(float64(1)), new(float64(MaxLimit))
					o.Params[i].Schema.Default = DefaultLimit
				}
			}
			cat.byID[o.ID] = o
		}
	}
	return cat, nil
}

// resolve replaces a component reference in s by the referenced schema's type
// and allowed values, keeping s's own nullability; it resolves array items too.
func resolve(s *Schema, components map[string]Schema) error {
	if s.Items != nil {
		if err := resolve(s.Items, components); err != nil {
			return err
		}
	}
	if s.Ref == "" {
		return nil
	}
	name, ok := strings.CutPrefix(s.Ref, "#/components/schemas/")
	target, found := components[name]
	if !ok || !found || target.Ref != "" || target.Items != nil {
		// Only scalar components (enums) are parameter types in the spec; any
		// other shape fails loudly rather than validating half of it.
		return fmt.Errorf("unresolvable schema reference %q", s.Ref)
	}
	s.Type, s.Enum, s.Nullable, s.Ref = target.Type, target.Enum, s.Nullable || target.Nullable, ""
	return nil
}

// visible drops the characters of s that hide or reorder text: control
// characters, the Unicode format category Cf (zero-width characters, bidi
// overrides and isolates, the byte order mark) and the tag block
// U+E0000–E007F. Invalid UTF-8 is dropped too. It mirrors sanitize.Text,
// which catalog may not import (ADR-0009).
func visible(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || (r >= 0xE0000 && r <= 0xE007F) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
}

// Lookup returns the in-scope operation with the given ID.
func (c Catalog) Lookup(id string) (Operation, bool) {
	op, ok := c.byID[id]
	return op, ok
}

// IsExcluded reports whether id is an operation of the Langfuse API that the
// catalog deliberately leaves out (ADR-0004).
func IsExcluded(id string) bool { return slices.Contains(excluded, id) }

// Operations returns every in-scope operation, sorted by ID.
func (c Catalog) Operations() []Operation {
	ops := make([]Operation, 0, len(c.byID))
	for _, op := range c.byID {
		ops = append(ops, op)
	}
	slices.SortFunc(ops, func(a, b Operation) int { return strings.Compare(a.ID, b.ID) })
	return ops
}
