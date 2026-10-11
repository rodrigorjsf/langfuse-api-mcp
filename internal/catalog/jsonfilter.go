package catalog

import (
	"errors"
	"strconv"
)

// jsonFilters is the closed list of the filter parameters whose value is a JSON
// text: a list of conditions that Langfuse parses (#195). The release specs
// describe each as a JSON string of filter conditions
// (docs/research/langfuse.md, section 1.15). A filter parameter
// that is on neither this list nor plainFilters fails a test, so a new one is
// never classified by its name alone.
var jsonFilters = map[operationParam]bool{
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
var plainFilters = map[operationParam]bool{
	{"scim_listUsers", "filter"}: true,
}

// isJSONFilter reports whether p is a query parameter of o that holds a JSON
// filter (jsonFilters).
func (o Operation) isJSONFilter(p Param) bool {
	return p.In == "query" && jsonFilters[operationParam{o.ID, p.Name}]
}

// ErrJSONFilter marks a JSON filter that the server refuses: not a string, too
// large or deep, not one JSON list, or holding a control or invisible
// character; it also matches ErrInvalidParameter. The server answers it with a
// hint built from JSONFilterEncoding.
var ErrJSONFilter = errors.New("malformed JSON filter")

// filterInvalidf is invalidf for a refused JSON filter: the error also matches
// ErrJSONFilter.
func filterInvalidf(format string, args ...any) error {
	return sentinelError{ErrJSONFilter, invalidf(format, args...)}
}

// jsonFilter checks the value of the JSON filter parameter called name and
// returns the text to send: the list it decodes to, written again as compact
// JSON (encodeJSON), so that the text sent is the one that was checked. The
// value must hold no control or invisible character as raw text (tab, line feed
// and carriage return are the blanks between the tokens of a pretty-printed
// filter), be at most maxJSONTextBytes long and nested at most maxJSONTextDepth
// levels deep, be the JSON text of one list with nothing after it, and hold no
// control or invisible character in a decoded string or object key, which finds
// one written as a JSON escape. A condition's columns, operators and keys are
// Langfuse's to check. Error messages name the rule or the bound, never the
// value.
func jsonFilter(name, raw string) (string, error) {
	// A raw hidden character is refused first, before the text is read, as it was
	// when the server passed a filter on as text.
	if holdsHiddenText(raw, true) {
		return "", filterInvalidf("parameter %s: %s", name, hiddenTextRule)
	}
	if len(raw) > maxJSONTextBytes {
		return "", filterInvalidf("parameter %s: want at most %d bytes of JSON, got %d bytes", name, maxJSONTextBytes, len(raw))
	}
	if tooDeep(raw) {
		return "", filterInvalidf("parameter %s: want JSON nested at most %d objects or lists deep", name, maxJSONTextDepth)
	}
	var v any
	// The decode error is dropped on purpose: its text quotes the caller's
	// input, which an error message never repeats.
	trailing, err := decodeJSON(raw, &v)
	if err != nil {
		return "", filterInvalidf("parameter %s: want a string holding the JSON text of one list", name)
	}
	if _, ok := v.([]any); !ok {
		return "", filterInvalidf("parameter %s: want a string holding the JSON text of one list, got %s", name, kind(v))
	}
	if trailing {
		return "", filterInvalidf("parameter %s: want one JSON list, got data after it", name)
	}
	if holdsHiddenJSONText(v) {
		return "", filterInvalidf("parameter %s: %s", name, hiddenTextRule)
	}
	text, err := encodeJSON(v)
	if err != nil {
		return "", filterInvalidf("parameter %s: cannot be re-encoded as JSON", name)
	}
	return text, nil
}

// JSONFilterEncoding says how a JSON filter is sent and the rules it has to
// meet: the one wording the hint of a refused filter uses. Static text; it
// never holds a filter.
func JSONFilterEncoding() string {
	return "one string whose value is the JSON text of a list of conditions, such as [{...},{...}]: " +
		"not a list, and the value itself has no backslash before its quotes; at most " + strconv.Itoa(maxJSONTextBytes) +
		" bytes and " + strconv.Itoa(maxJSONTextDepth) + " levels deep, with no control or invisible character in any " +
		"string or key, written as itself or as a JSON escape"
}
