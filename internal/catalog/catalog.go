// Package catalog is the set of in-scope Langfuse operations the server can
// execute, read from the union catalog embedded in the binary.
//
// It is pure data: it knows operation IDs, methods, path templates and
// parameters, and builds the request an operation needs from the caller's
// parameters (ADR-0010). It never performs I/O.
//
// The union catalog (ADR-0012) holds every operation of the Langfuse release
// specs since v3.0.0, each with its version range and operation family, minus
// the ADR-0004 exclusions; scripts/gen-union-catalog.py generates it. Resolve
// narrows it to the operations one deployment profile serves.
package catalog

import (
	_ "embed" // the Langfuse OpenAPI spec is compiled into the binary
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

//go:embed spec/langfuse-union-catalog.json
var unionCatalog []byte

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
// read-only. Lookup, Operations and Search see the operations of one
// resolution (Resolve); the union stays behind them for the next one.
type Catalog struct {
	union union
	byID  map[string]Operation
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
	// DescriptionLine is the operation's line in the operation index: the
	// union catalog's x-summary of a deprecated operation (what it does and
	// the operation to prefer), else the first line of its OpenAPI
	// description. The spec is third-party text, so indexLine cleans it.
	DescriptionLine string
	// Params are the operation's path and query parameters.
	Params []Param
	// Introduced is the first Langfuse version whose spec lists the operation
	// ("3.0.0" when it predates the union catalog), or the earlier floor the
	// docs state; empty when unknown.
	Introduced string
	// Removed is the first Langfuse version whose spec no longer lists the
	// operation; empty while the newest known spec still lists it.
	Removed string
	// Family is the operation family the deployment's migration mode gates
	// the operation by (ADR-0012 §2); empty when no mode gates it.
	Family Family
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

// Load builds the catalog from the embedded union catalog, minus the excluded
// operations. Until it is resolved for a deployment profile, it offers every
// operation, as Resolve does for an unknown version with every family on.
func Load() (Catalog, error) { return load(unionCatalog) }

// load builds the catalog from a union catalog, an OpenAPI document whose
// operations carry x-introduced, x-removed and x-family, minus the excluded
// operations. A plain OpenAPI spec loads too: its operations have no range.
func load(spec []byte) (Catalog, error) {
	var doc struct {
		Oldest     string                                `json:"x-oldest-version"`
		Newest     string                                `json:"x-newest-version"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]Schema `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec, &doc); err != nil {
		return Catalog{}, fmt.Errorf("embedded union catalog: %w", err)
	}
	var u union
	var err error
	if u.oldest, err = optionalVersion(doc.Oldest); err == nil {
		u.newest, err = optionalVersion(doc.Newest)
	}
	if err != nil {
		return Catalog{}, fmt.Errorf("embedded union catalog: %w", err)
	}
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
				Summary     string   `json:"x-summary"`
				Parameters  []Param  `json:"parameters"`
				Introduced  string   `json:"x-introduced"`
				Removed     string   `json:"x-removed"`
				Family      Family   `json:"x-family"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				return Catalog{}, fmt.Errorf("embedded union catalog: %s %s: %w", m, path, err)
			}
			if op.OperationID == "" || slices.Contains(excluded, op.OperationID) {
				continue
			}
			for i := range op.Parameters {
				if err := resolve(&op.Parameters[i].Schema, doc.Components.Schemas); err != nil {
					return Catalog{}, fmt.Errorf("embedded union catalog: %s parameter %s: %w",
						op.OperationID, op.Parameters[i].Name, err)
				}
			}
			o := Operation{
				ID: op.OperationID, Method: m, Path: path, Params: op.Parameters,
				Introduced: op.Introduced, Removed: op.Removed, Family: op.Family,
			}
			r, err := rangeOf(o)
			if err != nil {
				return Catalog{}, fmt.Errorf("embedded union catalog: %s: %w", o.ID, err)
			}
			if len(op.Tags) > 0 {
				o.Tag = visible(op.Tags[0])
			}
			o.DescriptionLine = indexLine(op.Summary, op.Description)
			for i, p := range o.Params {
				if o.isListLimit(p) {
					// The catalog bounds the page size itself (Request).
					o.Params[i].Schema.Minimum, o.Params[i].Schema.Maximum = new(float64(1)), new(float64(MaxLimit))
					o.Params[i].Schema.Default = DefaultLimit
				}
			}
			u.ops = append(u.ops, rangedOperation{Operation: o, span: r})
		}
	}
	return Catalog{union: u}.Resolve(Profile{Families: AllFamilies()}), nil
}

// maxIndexLineRunes bounds an operation's line in the operation index.
const maxIndexLineRunes = 200

// markdownLink matches a Markdown link or image; its text is kept.
var markdownLink = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

// indexLine returns an operation's line in the operation index: the summary
// the union catalog gives a deprecated operation (x-summary: what it does
// and its replacement, #79), else the first line of its description. Either
// is third-party text: hidden characters are removed, a Markdown link keeps
// only its text, and a line over maxIndexLineRunes is cut with an ellipsis.
// Instruction-like text shorter than that passes through (see #82).
func indexLine(summary, description string) string {
	line := summary
	if strings.TrimSpace(line) == "" {
		line, _, _ = strings.Cut(strings.TrimSpace(description), "\n")
	}
	line = strings.TrimSpace(markdownLink.ReplaceAllString(visible(line), "$1"))
	if r := []rune(line); len(r) > maxIndexLineRunes {
		line = string(r[:maxIndexLineRunes-1]) + "…"
	}
	return line
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
