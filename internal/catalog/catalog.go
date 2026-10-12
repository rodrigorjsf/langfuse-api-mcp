// Package catalog is the set of in-scope Langfuse operations the server can
// execute, read from the union catalog embedded in the binary.
//
// It is pure data: it knows operation IDs, methods, path templates and
// parameters, and builds the request an operation needs from the caller's
// parameters (ADR-0010). It never performs I/O.
//
// The union catalog (ADR-0012) holds every operation of the Langfuse release
// specs since v3.0.0, each with its version range and operation family;
// scripts/gen-union-catalog.py generates it. The embedded JSON still holds the
// ADR-0004 exclusions: Load removes them. Resolve narrows the rest to the
// operations one deployment profile serves.
package catalog

import (
	"bytes"
	_ "embed" // the Langfuse OpenAPI spec is compiled into the binary
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/google/jsonschema-go/jsonschema"
)

//go:embed spec/langfuse-union-catalog.json
var unionCatalog []byte

// excluded holds the operation IDs never exposed on any deployment (ADR-0004
// and its M4 amendment): trace ingestion, organization admin mutations
// (projects, API keys, memberships, SCIM users), media uploads and writes
// whose body carries a third-party credential. Load removes them.
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
	// ADR-0004 amendment (M4): a media upload is a PUT of the bytes to a
	// presigned URL this server never makes, so these two only leave
	// half-registered media behind.
	"media_getUploadUrl",
	"media_patch",
	// ADR-0004 amendment (M4): their bodies carry a third-party credential,
	// which would pass through the model's context as a tool argument.
	"llmConnections_upsert",
	"blobStorageIntegrations_upsertBlobStorageIntegration",
}

// Catalog is the set of in-scope operations, built once at startup and shared
// read-only. Lookup, Operations and Search see the operations of one
// resolution (Resolve); the union stays behind them for the next one.
type Catalog struct {
	union union
	byID  map[string]Operation
	// bodyErr is why the body of an operation byID offers does not compile,
	// nil when every one does (BodyError).
	bodyErr error
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
	// Params are the operation's served parameters: the path and query
	// parameters the deployment's version declares (Resolve, #173). Before
	// the catalog is resolved for a deployment it is those of the newest known
	// release.
	Params []Param
	// unserved are the operation's parameters the deployment's version does not
	// declare, with their ranges: Request refuses them naming the release that
	// serves them. Empty until Resolve.
	unserved []Param
	// deployment is the deployment's plain version, "" when not detected.
	deployment string
	// Introduced is the first Langfuse version whose spec lists the operation
	// (SupportedFloor when it predates the union catalog), or the earlier floor the
	// docs state; empty when unknown.
	Introduced string
	// Removed is the first Langfuse version whose spec no longer lists the
	// operation; empty while the newest known spec still lists it.
	Removed string
	// Family is the operation family the deployment's migration mode gates
	// the operation by (ADR-0012 §2); empty when no mode gates it.
	Family Family
	// Body is the operation's JSON request body for the deployment's version
	// (Resolve, #202); nil when that version takes none. Before the catalog is
	// resolved for a deployment it is the newest release's.
	Body *RequestBody
	// prefers is the ID of the operation the union catalog's summary names as
	// the one to prefer over this one ("legacy: prefer X when it is
	// available"); "" when it names none. plainLine is the operation's line
	// without that mention, which Resolve uses where the catalog does not
	// offer prefers. On a deployment with both read families the preferred
	// operation can return less than this one (#212).
	prefers, plainLine string
	// otherFamilyPreferred is prefers when, on the deployment profile the catalog
	// was resolved for, this legacy read operation's preferred operation is a read
	// operation the catalog offers, of another operation family than the legacy
	// one, and both families are on (Resolve, #212); "" otherwise.
	otherFamilyPreferred string
}

// RequestBody is the JSON request body of a write operation, from the release
// spec of the deployment's version (#81, #202). CheckBody, the body gate of
// execute_write, checks a body against it.
type RequestBody struct {
	// Required reports whether the operation needs a body.
	Required bool
	// Schema is the body's JSON Schema 2020-12 document, $refs inlined; the
	// generator converts the release spec's OpenAPI 3.0 dialect (#111). It is
	// third-party text, so every string in it is cleaned of hidden characters.
	Schema json.RawMessage
	// compiled is Schema compiled once (#112); CheckBody validates a body
	// against it. Nil when Schema does not compile (BodyError). Shared by every
	// resolution; it is compiled once, behind a sync.Once of the stored body.
	compiled *jsonschema.Resolved
}

// operationParam names one parameter of one operation. It is the key of the
// closed lists that say something of a parameter by name: folderCapable,
// staticGuidance, jsonFilters, plainFilters and notTimeWindows.
type operationParam struct{ operationID, param string }

// Param is one path or query parameter of an operation.
type Param struct {
	Name string
	// In is "path" or "query".
	In       string
	Required bool
	// Schema is the type the parameter's value must have.
	Schema Schema
	// Introduced is the first Langfuse version whose spec lists the parameter
	// (the operation's own when it was there from the start); empty when the
	// catalog gives no range.
	Introduced string `json:"x-introduced"`
	// Removed is the first Langfuse version whose spec no longer lists the
	// parameter while it lists the operation; empty while the newest known
	// spec lists it.
	Removed string `json:"x-removed"`
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
	// size of a list operation, which the catalog bounds itself (MaxLimit),
	// and the page number, whose minimum 1 the catalog gives (#179).
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

// PreferredOfOtherFamily returns the ID of the operation this operation's line
// prefers, when this is a read operation of the legacy family and, on the
// deployment profile the catalog was resolved for, that operation is a read
// operation the catalog offers, belongs to another operation family than the
// legacy one, and both families are on (#212). The preferred operation then
// reads other data than this one and can return less of what was written
// through the legacy APIs. "" when any of that fails. It is decided by the
// catalog's own data and the profile alone, never by a caller or a payload.
func (o Operation) PreferredOfOtherFamily() string { return o.otherFamilyPreferred }

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
	if doc.Oldest != "" && doc.Oldest != SupportedFloor {
		return Catalog{}, fmt.Errorf("embedded union catalog: oldest version %q is not the supported floor %s", doc.Oldest, SupportedFloor)
	}
	var u union
	var err error
	if u.newest, err = optionalVersion(doc.Newest); err != nil {
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
				// RequestBody is the one body of a plain OpenAPI spec;
				// RequestBodies are the union catalog's: every distinct body
				// the release specs gave the operation, with its releases (#202).
				RequestBody    *bodyDocument  `json:"requestBody"`
				RequestBodies  []bodyDocument `json:"x-request-bodies"`
				NewestBodyOnly bool           `json:"x-newest-body-only"`
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
			bodies, err := storeBodies(o.ID, op.RequestBody, op.RequestBodies, op.NewestBodyOnly)
			if err != nil {
				return Catalog{}, fmt.Errorf("embedded union catalog: %w", err)
			}
			r, err := rangeOf(o)
			if err != nil {
				return Catalog{}, fmt.Errorf("embedded union catalog: %s: %w", o.ID, err)
			}
			paramSpans := make([]span, len(o.Params))
			for i, p := range o.Params {
				if paramSpans[i], err = parameterRange(p); err != nil {
					return Catalog{}, fmt.Errorf("embedded union catalog: %s parameter %s: %w", o.ID, p.Name, err)
				}
			}
			if len(op.Tags) > 0 {
				o.Tag = visible(op.Tags[0])
			}
			o.DescriptionLine = indexLine(op.Summary, op.Description)
			o.prefers, o.plainLine = splitPreferred(op.Summary, op.Description, o.DescriptionLine)
			for i, p := range o.Params {
				switch {
				case o.isListLimit(p):
					// The catalog bounds the page size itself (Request).
					o.Params[i].Schema.Minimum, o.Params[i].Schema.Maximum = new(float64(1)), new(float64(MaxLimit))
					o.Params[i].Schema.Default = DefaultLimit
				case isPageNumber(p):
					// Pages start at 1: Langfuse answers 400 below it, so the
					// server refuses it before any request (#179).
					o.Params[i].Schema.Minimum = new(float64(1))
				}
			}
			u.ops = append(u.ops, rangedOperation{Operation: o, span: r, paramSpans: paramSpans, bodies: bodies})
		}
	}
	// A version not detected resolves the newest body of every operation: a
	// schema that does not compile stops the load.
	c := Catalog{union: u}.Resolve(Profile{Families: AllFamilies()})
	if err := c.BodyError(); err != nil {
		return Catalog{}, fmt.Errorf("embedded union catalog: %w", err)
	}
	return c, nil
}

// maxIndexLineRunes bounds an operation's line in the operation index.
const maxIndexLineRunes = 200

// markdownLink matches a Markdown link or image; its text is kept.
var markdownLink = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

// indexLine returns an operation's line in the operation index: the summary
// the union catalog gives a deprecated operation (x-summary: what it does
// and its replacement, #79), else the first line of its description. Either
// is third-party text: hidden characters are removed, a Markdown link keeps
// only its text, bold markers (** and __) are removed, and a line over
// maxIndexLineRunes is cut with an ellipsis. Single * and _ and backticks
// stay; __ goes even inside a code span (no catalog line holds one today).
// Instruction-like text shorter than that passes through;
// search_operations and describe_operation frame the lines as third-party
// text instead (#82).
func indexLine(summary, description string) string {
	line := summary
	if strings.TrimSpace(line) == "" {
		line, _, _ = strings.Cut(strings.TrimSpace(description), "\n")
	}
	line = strings.TrimSpace(withoutBoldMarkers(markdownLink.ReplaceAllString(visible(line), "$1")))
	if r := []rune(line); len(r) > maxIndexLineRunes {
		line = string(r[:maxIndexLineRunes-1]) + "…"
	}
	return line
}

// preferredSuffix matches the mention a deprecated operation's summary ends
// with: the operation to prefer (scripts/gen-union-catalog.py, summarize). Only
// an operation ID matches; a path the notice wrote is not one.
var preferredSuffix = regexp.MustCompile(` \(legacy: prefer ([A-Za-z0-9_-]+) when it is available\)$`)

// splitPreferred returns the operation a summary names as the one to prefer
// and the operation's index line without that mention (" (legacy)" instead);
// both are "" and line when the summary names none.
func splitPreferred(summary, description, line string) (prefers, plainLine string) {
	clean := visible(summary)
	m := preferredSuffix.FindStringSubmatchIndex(clean)
	if m == nil {
		return "", line
	}
	return clean[m[2]:m[3]], indexLine(clean[:m[0]]+" (legacy)", description)
}

// cleanSchema returns the JSON schema raw with every string in it cleaned of
// hidden characters: keys by visible, values by visibleText. A compromised
// upstream spec cannot smuggle hidden instructions through a body schema.
func cleanSchema(raw json.RawMessage) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keeps numbers exactly as the spec wrote them
	var schema any
	if err := dec.Decode(&schema); err != nil {
		return nil, err
	}
	cleaned, err := cleanValue(schema)
	if err != nil {
		return nil, err
	}
	return json.Marshal(cleaned)
}

// cleanValue is v with visible applied to every string it holds. Two keys of
// one object that only hidden characters told apart are an error: keeping
// either would depend on map order.
func cleanValue(v any) (any, error) {
	switch v := v.(type) {
	case string:
		return visibleText(v), nil
	case []any:
		for i := range v {
			x, err := cleanValue(v[i])
			if err != nil {
				return nil, err
			}
			v[i] = x
		}
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			x, err := cleanValue(x)
			if err != nil {
				return nil, err
			}
			k := visible(k)
			if _, dup := out[k]; dup {
				return nil, fmt.Errorf("schema key %q appears twice once hidden characters are removed", k)
			}
			out[k] = x
		}
		return out, nil
	}
	return v, nil
}

// boldMarker matches a Markdown bold marker: a run of two or more * or _.
var boldMarker = regexp.MustCompile(`\*\*+|__+`)

// withoutBoldMarkers removes every ** and __ from s. Removing one run can join
// two others ("*__*"), so it repeats until none is left; each pass shortens s.
func withoutBoldMarkers(s string) string {
	for boldMarker.MatchString(s) {
		s = boldMarker.ReplaceAllString(s, "")
	}
	return s
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
// which catalog may not import (ADR-0009), except that it does not remove a
// terminal escape sequence whole (#193): its text is the embedded OpenAPI spec,
// not Langfuse data read at run time.
func visible(s string) string {
	return strings.Map(func(r rune) rune {
		if hidden(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
}

// visibleText is visible for multi-line text: tab, line feed and carriage
// return are kept, as sanitize.Text keeps them in a payload.
func visibleText(s string) string {
	return strings.Map(func(r rune) rune {
		if hiddenOutsideWhitespace(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
}

// hiddenOutsideWhitespace is hidden without tab, line feed and carriage
// return, which are visible text where a value may span lines.
func hiddenOutsideWhitespace(r rune) bool {
	return hidden(r) && r != '\t' && r != '\n' && r != '\r'
}

// hidden reports whether r hides or reorders text (see visible).
// Like sanitize's, it covers the variation selectors and the other
// default-ignorable code points (#156).
func hidden(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || (r >= 0xE0000 && r <= 0xE007F) ||
		unicode.In(r, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
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
