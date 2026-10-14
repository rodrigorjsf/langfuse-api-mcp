package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Caps of a write operation's request body (spec #109). A body is checked
// against them before anything is sent, so one call cannot push an oversized
// or deeply nested payload to Langfuse. MaxBodyBytes is measured on the
// compacted JSON; MaxBodyDepth counts every object and array, the body itself
// being level 1.
const (
	MaxBodyBytes = 256 << 10
	MaxBodyDepth = 32
)

// ErrInvalidBody marks every error CheckBody returns: the body does not fit
// the operation. The message names the rule it broke, never the body.
var ErrInvalidBody = errors.New("invalid body")

// IsDestructive reports whether the operation changes or removes existing
// data (HTTP DELETE, PUT or PATCH): it runs only after the user confirms it.
func (o Operation) IsDestructive() bool {
	switch o.Method {
	case http.MethodDelete, http.MethodPut, http.MethodPatch:
		return true
	}
	return false
}

// DeletedTraceCount returns the number of trace IDs body deletes when the
// operation is trace_deleteMultiple, the one operation whose body lists what
// it deletes; ok is false for any other operation or a body without traceIds.
func (o Operation) DeletedTraceCount(body json.RawMessage) (n int, ok bool) {
	if o.ID != "trace_deleteMultiple" {
		return 0, false
	}
	var b struct {
		TraceIDs []json.RawMessage `json:"traceIds"`
	}
	if json.Unmarshal(body, &b) != nil || b.TraceIDs == nil {
		return 0, false
	}
	return len(b.TraceIDs), true
}

// CheckBody checks a caller's request body for the operation and returns it
// compacted, ready to send, or nil when there is no body to send. An absent
// body or a JSON null is no body. It refuses a body given to an operation
// that takes none, a missing required body, malformed JSON, a body over
// MaxBodyBytes or nested deeper than MaxBodyDepth, a body that is not a JSON
// object where the operation's schema expects one and, once those pass, a
// body that fails the operation's JSON Schema 2020-12 (#112). Every error
// wraps ErrInvalidBody and names the rule, never the value: a schema failure
// names the JSON location and the failed keyword.
func (o Operation) CheckBody(body json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(body)
	absent := len(trimmed) == 0 || string(trimmed) == "null"
	switch {
	case absent && o.Body != nil && o.Body.Required:
		return nil, invalidBodyf("a body is required by operation %s", o.ID)
	case absent:
		return nil, nil
	case o.Body == nil:
		return nil, invalidBodyf("operation %s takes no body", o.ID)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, invalidBodyf("not valid JSON")
	}
	out := compact.Bytes()
	if len(out) > MaxBodyBytes {
		return nil, invalidBodyf("exceeds %d bytes once compacted", MaxBodyBytes)
	}
	if nestingDepth(out) > MaxBodyDepth {
		return nil, invalidBodyf("nests deeper than %d levels of objects and arrays", MaxBodyDepth)
	}
	if shape := o.Body.Shape(); shape != "" && !acceptsTopLevel(o.Body.Schema, out[0]) {
		return nil, invalidBodyf("want %s, as the schema of operation %s asks", shape, o.ID)
	}
	if o.Body.compiled == nil {
		// BodyError told startup; a body is never checked against nothing.
		return nil, invalidBodyf("the body schema of operation %s does not compile, so no body can be checked against it", o.ID)
	}
	if err := o.Body.validate(out); err != nil {
		return nil, err
	}
	return out, nil
}

// invalidBodyf returns an error wrapping ErrInvalidBody, as invalidf does
// for a parameter.
func invalidBodyf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidBody}, args...)...)
}

// nestingDepth returns the deepest level of objects and arrays in the valid
// JSON document doc; brackets inside strings do not count.
func nestingDepth(doc []byte) int {
	depth, deepest := 0, 0
	inString, escaped := false, false
	for _, c := range doc {
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == '{' || c == '[':
			depth++
			deepest = max(deepest, depth)
		case c == '}' || c == ']':
			depth--
		}
	}
	return deepest
}

// Shape names what the top level of a body must be, as the refusal of a body
// and the hint after it say it: "a JSON object" where the schema accepts only
// an object, "a JSON object or a list" where it accepts only those two (the
// single or batch score of scores_create from Langfuse 4.55.0), and "" where it
// does not limit the top level to either, which this check then leaves to the
// schema.
func (b *RequestBody) Shape() string {
	if b == nil {
		return ""
	}
	switch topLevelKinds(b.Schema) {
	case kindObject:
		return "a JSON object"
	case kindObject | kindArray:
		return "a JSON object or a list"
	}
	return ""
}

// The top-level kinds of JSON value a body schema accepts, as bits.
const (
	kindObject = 1 << iota
	kindArray
	kindOther
	kindAny = kindObject | kindArray | kindOther
)

// acceptsTopLevel reports whether the schema accepts a body whose compacted
// JSON starts with the byte first.
func acceptsTopLevel(schema json.RawMessage, first byte) bool {
	kind := kindOther
	switch first {
	case '{':
		kind = kindObject
	case '[':
		kind = kindArray
	}
	return topLevelKinds(schema)&kind != 0
}

// topLevelKinds returns the kinds of JSON value the schema accepts at its top
// level: those its type names, narrowed by the union of its oneOf and of its
// anyOf alternatives and by every allOf alternative. A schema it cannot read
// accepts any kind, so the schema check decides.
func topLevelKinds(schema json.RawMessage) int {
	var s struct {
		Type  any               `json:"type"`
		OneOf []json.RawMessage `json:"oneOf"`
		AnyOf []json.RawMessage `json:"anyOf"`
		AllOf []json.RawMessage `json:"allOf"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return kindAny
	}
	kinds := typeKinds(s.Type)
	for _, union := range [][]json.RawMessage{s.OneOf, s.AnyOf} {
		if len(union) == 0 {
			continue
		}
		some := 0
		for _, a := range union {
			some |= topLevelKinds(a)
		}
		kinds &= some
	}
	for _, a := range s.AllOf {
		kinds &= topLevelKinds(a)
	}
	return kinds
}

// typeKinds returns the kinds a schema's type keyword names: a type name or a
// list of them; no type accepts any kind.
func typeKinds(t any) int {
	var names []any
	switch t := t.(type) {
	case string:
		names = []any{t}
	case []any:
		names = t
	default:
		return kindAny
	}
	kinds := 0
	for _, n := range names {
		switch n {
		case "object":
			kinds |= kindObject
		case "array":
			kinds |= kindArray
		default:
			kinds |= kindOther
		}
	}
	return kinds
}
