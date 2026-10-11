package catalog

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

// filterParam names the filter query parameter of one operation.
type filterParam struct{ operationID, param string }

// jsonFilters is the closed list of the filter parameters whose value is a JSON
// text: a list of conditions that Langfuse parses (#195). Each is described in
// the release specs as "a JSON string containing an array of filter
// conditions" (docs/research/langfuse.md, section 1.15). A filter parameter
// that is on neither this list nor plainFilters fails a test, so a new one is
// never classified by its name alone.
var jsonFilters = map[filterParam]bool{
	{"trace_list", "filter"}:                    true,
	{"legacy_observationsV1_getMany", "filter"}: true,
	{"observations_getMany", "filter"}:          true,
	{"scores_get-many", "filter"}:               true,
	{"experiments_list", "filter"}:              true,
	{"experiments_listItems", "filter"}:         true,
	{"prompts_list", "filter"}:                  true,
}

// plainFilters is the closed list of the filter parameters that are not a JSON
// text, so that they are checked as any query parameter value: the SCIM filter
// expression of scim_listUsers (`userName eq "value"`).
var plainFilters = map[filterParam]bool{
	{"scim_listUsers", "filter"}: true,
}

// isJSONFilter reports whether p is a query parameter of o that holds a JSON
// filter (jsonFilters).
func (o Operation) isJSONFilter(p Param) bool {
	return p.In == "query" && jsonFilters[filterParam{o.ID, p.Name}]
}

// ErrJSONFilter marks a JSON filter that the server refuses: not a string, too
// large or deep, not one JSON list, or holding a control or invisible
// character; it also matches ErrInvalidParameter. The server answers it with
// JSONFilterHint.
var ErrJSONFilter = errors.New("malformed JSON filter")

// filterInvalidf is invalidf for a refused JSON filter: the error also matches
// ErrJSONFilter.
func filterInvalidf(format string, args ...any) error {
	return sentinelError{ErrJSONFilter, invalidf(format, args...)}
}

// jsonFilter checks the value of a JSON filter and returns the text to send:
// the list it decodes to, written again as compact JSON (encodeJSON), so that
// the text sent is the one that was checked. The value must be at most
// maxJSONTextBytes long and nested at most maxJSONTextDepth levels deep, hold
// no control or invisible character (written as itself, or as a JSON escape in
// a string or an object key), and be the JSON text of one list with nothing
// after it. A condition's columns, operators and keys are Langfuse's to check.
// Error messages name the rule or the bound, never the value.
func jsonFilter(raw string) (string, error) {
	if len(raw) > maxJSONTextBytes {
		return "", filterInvalidf("parameter filter: want at most %d bytes of JSON, got %d bytes", maxJSONTextBytes, len(raw))
	}
	// A raw hidden character is refused before the text is read; tab, line feed
	// and carriage return are the whitespace of a pretty-printed filter.
	if holdsHiddenText(raw, true) {
		return "", filterInvalidf("parameter filter: %s", hiddenTextRule)
	}
	if tooDeep(raw) {
		return "", filterInvalidf("parameter filter: want JSON nested at most %d objects or lists deep", maxJSONTextDepth)
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	// The decode error is dropped on purpose: its text quotes the caller's
	// input, which an error message never repeats.
	if err := dec.Decode(&v); err != nil {
		return "", filterInvalidf("parameter filter: want a string holding the JSON text of one list")
	}
	if _, ok := v.([]any); !ok {
		return "", filterInvalidf("parameter filter: want a string holding the JSON text of one list, got %s", kind(v))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", filterInvalidf("parameter filter: want one JSON list, got data after it")
	}
	// The escapes were decoded above, so a hidden character is found written
	// either way: the server reads the text as Langfuse will.
	if holdsHiddenJSONText(v) {
		return "", filterInvalidf("parameter filter: %s", hiddenTextRule)
	}
	text, err := encodeJSON(v)
	if err != nil {
		return "", filterInvalidf("parameter filter: cannot be re-encoded as JSON")
	}
	return text, nil
}

// JSONFilterHint is the hint of a refused JSON filter: how a filter is sent and
// the rules it has to meet. Static text; it never holds the filter, and it is
// not the hint of a refused metrics query.
func JSONFilterHint() string {
	return "send filter as one string whose value is the JSON text of a list of conditions, such as [{...},{...}]: " +
		"not a list, and the value itself has no backslash before its quotes; at most " + strconv.Itoa(maxJSONTextBytes) +
		" bytes and " + strconv.Itoa(maxJSONTextDepth) + " levels deep, with no control or invisible character in any " +
		"string or key, written as itself or as a JSON escape"
}
