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
	if out[0] != '{' && expectsObject(o.Body.Schema) {
		return nil, invalidBodyf("want a JSON object, as the schema of operation %s asks", o.ID)
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

// expectsObject reports whether the JSON schema only accepts an object: its
// type is object, or every alternative of its oneOf, anyOf or allOf does.
func expectsObject(schema json.RawMessage) bool {
	var s struct {
		Type  any               `json:"type"`
		OneOf []json.RawMessage `json:"oneOf"`
		AnyOf []json.RawMessage `json:"anyOf"`
		AllOf []json.RawMessage `json:"allOf"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return false
	}
	if s.Type == "object" {
		return true
	}
	for _, alternatives := range [][]json.RawMessage{s.OneOf, s.AnyOf, s.AllOf} {
		if len(alternatives) == 0 {
			continue
		}
		all := true
		for _, a := range alternatives {
			all = all && expectsObject(a)
		}
		if all {
			return true
		}
	}
	return false
}
