package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The metrics operations take their page size as config.row_limit inside the
// JSON string of their "query" parameter, not as a "limit" query parameter.
// A query without one gets DefaultRowLimit, Langfuse's own default; a
// row_limit outside 1..MaxRowLimit, Langfuse's own range, is refused.
const (
	DefaultRowLimit = 100
	MaxRowLimit     = 1000
)

// ErrRowLimitOutOfRange marks a metrics config.row_limit that is not an
// integer from 1 to MaxRowLimit; it also matches ErrInvalidParameter.
var ErrRowLimitOutOfRange = errors.New("row_limit out of range")

// ErrMetricsQuery marks a metrics query JSON that is not a well-formed
// metrics query: too large or deep, not one JSON object, an unknown top-level
// key or a key of the wrong type. It also matches ErrInvalidParameter.
var ErrMetricsQuery = errors.New("malformed metrics query")

// MetricsOperationID is the Metrics v2 operation: the one whose query has
// static guidance (ParamGuidance).
const MetricsOperationID = "metrics_metrics"

// metricsOperations are the operations whose "query" parameter is a metrics
// query JSON.
var metricsOperations = map[string]bool{
	MetricsOperationID:         true,
	"legacy_metricsV1_metrics": true,
}

// isMetricsQuery reports whether p is the JSON query of a metrics operation.
func (o Operation) isMetricsQuery(p Param) bool {
	return metricsOperations[o.ID] && p.In == "query" && p.Name == "query"
}

// isMetricsV2Query reports whether p is the JSON query of MetricsOperationID;
// the legacy v1 metrics operation is left out on purpose, as it has no
// guidance.
func (o Operation) isMetricsV2Query(p Param) bool {
	return o.ID == MetricsOperationID && o.isMetricsQuery(p)
}

// A metrics query JSON is bounded before it is decoded: at most
// maxMetricsQueryBytes long (the first query-JSON size cap; the M3 workflow
// payload-query guard, #42, is to reuse it) and nested at most maxMetricsQueryDepth objects
// and lists deep (the query object itself is depth 1).
const (
	maxMetricsQueryBytes = 16 << 10
	maxMetricsQueryDepth = 10
)

// jsonType is the JSON type a top-level value of a metrics query must have.
type jsonType struct {
	// name is "a string", "a list" or "an object", as error messages say it.
	name string
	// nullable also accepts JSON null.
	nullable bool
}

// has reports whether a decoded value has the type.
func (t jsonType) has(v any) bool {
	if v == nil {
		return t.nullable
	}
	return kind(v) == t.name
}

func (t jsonType) String() string {
	if t.nullable {
		return t.name + " or null"
	}
	return t.name
}

var (
	jsonString = jsonType{name: "a string"}
	jsonList   = jsonType{name: "a list"}
)

// metricsQueryKeys are the top-level keys of a metrics query and the JSON
// type of each value.
var metricsQueryKeys = map[string]jsonType{
	"config":        {name: "an object", nullable: true},
	"dimensions":    jsonList,
	"filters":       jsonList,
	"fromTimestamp": jsonString,
	"metrics":       jsonList,
	"orderBy":       {name: "a list", nullable: true},
	"timeDimension": {name: "an object", nullable: true},
	"toTimestamp":   jsonString,
	"view":          jsonString,
}

// metricsQuery checks a metrics query JSON and returns it re-encoded, with
// config.row_limit defaulted. The query must be one JSON object within the
// size and depth bounds, with only the keys of metricsQueryKeys, each of its
// type. Error messages name the key (a known one) and the reason, never the
// caller's text. Re-encoding sorts the keys and keeps only the last of a
// duplicated key, the one that was checked.
// Langfuse checks the rest (required keys, allowed views and measures).
func metricsQuery(raw string) (string, error) {
	if len(raw) > maxMetricsQueryBytes {
		return "", queryInvalidf("parameter query: want at most %d bytes of JSON, got %d bytes", maxMetricsQueryBytes, len(raw))
	}
	if err := checkDepth(raw); err != nil {
		return "", err
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var q map[string]any
	// The decode error is dropped on purpose: its text quotes the caller's
	// input, which an error message never repeats.
	if err := dec.Decode(&q); err != nil || q == nil {
		return "", queryInvalidf("parameter query: want a string holding the JSON text of one object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", queryInvalidf("parameter query: want one JSON object, got data after it")
	}
	for _, name := range slices.Sorted(maps.Keys(q)) {
		v := q[name]
		want, known := metricsQueryKeys[name]
		if !known {
			return "", queryInvalidf("parameter query: unknown top-level key; the keys of a metrics query are: %s",
				MetricsQueryKeys())
		}
		if !want.has(v) {
			return "", queryInvalidf("parameter query: %s: want %s, got %s", name, want, kind(v))
		}
	}
	config, _ := q["config"].(map[string]any)
	if config == nil {
		config = map[string]any{}
		q["config"] = config
	}
	rowLimit := DefaultRowLimit
	if v, ok := config["row_limit"]; ok {
		// Only an integer literal counts: a float such as 1000.0 or 1e3 is
		// refused like 1.5, never rounded, and a non-number (a string, null,
		// true) is refused outright.
		literal, isNumber := v.(json.Number)
		n, ok := parsePageSize(string(literal), MaxRowLimit)
		if !isNumber || !ok {
			return "", markedError{ErrRowLimitOutOfRange,
				invalidf("parameter query: config.row_limit: want an integer from 1 to %d, got %s", MaxRowLimit, kind(v))}
		}
		rowLimit = n
	}
	config["row_limit"] = json.Number(strconv.Itoa(rowLimit))
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(q); err != nil {
		return "", queryInvalidf("parameter query: cannot be re-encoded as JSON")
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// queryInvalidf is invalidf for a malformed metrics query: the error also
// matches ErrMetricsQuery.
func queryInvalidf(format string, args ...any) error {
	return markedError{ErrMetricsQuery, invalidf(format, args...)}
}

// checkDepth refuses JSON nested deeper than maxMetricsQueryDepth, before it
// is decoded into memory.
func checkDepth(raw string) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil // malformed JSON is refused by the decode that follows
		}
		switch tok {
		case json.Delim('{'), json.Delim('['):
			depth++
			if depth > maxMetricsQueryDepth {
				return queryInvalidf("parameter query: want JSON nested at most %d objects or lists deep", maxMetricsQueryDepth)
			}
		case json.Delim('}'), json.Delim(']'):
			depth--
			if depth == 0 {
				return nil
			}
		}
	}
}

// MetricsQueryEncoding says how the query of a metrics operation is sent:
// one string holding the JSON text, never an object and never escaped twice.
// It is the one wording both refusals' hints and the guidance use, so they
// cannot drift and never push a model from one wrong shape to the other
// (#106). Static text: it never holds the caller's query.
const MetricsQueryEncoding = `one string whose value is the JSON text of the query object, ` +
	`such as {"view":"observations",…}: not an object, and the value itself has no backslash before its quotes`

// MetricsQueryKeys lists the top-level keys of a metrics query, the ones the
// validator accepts, sorted and separated by ", ".
func MetricsQueryKeys() string {
	return strings.Join(slices.Sorted(maps.Keys(metricsQueryKeys)), ", ")
}

// metricsQueryExample is the worked example of the metrics query guidance:
// the total cost per day of the observations of traces named checkout. It is
// checked against the Langfuse Metrics v2 docs (docs/research/langfuse.md,
// section 4.5), and a test proves the validator accepts it. Its dates are
// fixed, as static text must be, and lie in January 2025 so that they read
// as an example and never coincide with "the last 7 days" of a run: a model
// without a clock copied the earlier window of the eval's run week (#105).
const metricsQueryExample = `{"view":"observations","metrics":[{"measure":"totalCost","aggregation":"sum"}],` +
	`"filters":[{"column":"traceName","operator":"=","value":"checkout","type":"string"}],` +
	`"timeDimension":{"granularity":"day"},"fromTimestamp":"2025-01-01T00:00:00Z","toTimestamp":"2025-01-08T00:00:00Z"}`

// ParamGuidance returns static guidance on how to fill parameter p of the
// operation, or "" when there is none (#104). Only the query of
// metrics_metrics has guidance: the Langfuse spec types it as a plain string,
// so nothing else tells an agent its shape. The text is compiled into the
// binary, never built from API data, and its key list is the validator's
// own (metricsQueryKeys), so the two cannot drift.
func (o Operation) ParamGuidance(p Param) string {
	if !o.isMetricsV2Query(p) {
		return ""
	}
	keys := slices.Sorted(maps.Keys(metricsQueryKeys))
	typed := make([]string, len(keys))
	for i, k := range keys {
		typed[i] = k + " (" + metricsQueryKeys[k].String() + ")"
	}
	return "Send query as " + MetricsQueryEncoding + ". Its top-level keys: " + strings.Join(typed, ", ") +
		"; any other key is refused. " +
		"Langfuse requires view, metrics, fromTimestamp and toTimestamp (ISO 8601 date-times). " +
		"view: observations, scores-numeric, scores-boolean or scores-categorical. " +
		`metrics: a list of {"measure": "totalCost", "aggregation": "sum"}; aggregation is sum, avg, count, max, min, ` +
		"p50, p75, p90, p95, p99 or histogram. " +
		`dimensions: a list of {"field": "providedModelName"}. ` +
		`filters: a list of {"column": "traceName", "operator": "=", "value": "checkout", "type": "string"}. ` +
		`timeDimension: {"granularity": "day"}; granularity is auto, minute, hour, day, week or month. ` +
		`orderBy: a list of {"field": "sum_totalCost", "direction": "desc"}. ` +
		`config: {"row_limit": ` + strconv.Itoa(DefaultRowLimit) + `}; row_limit is from 1 to ` +
		strconv.Itoa(MaxRowLimit) + ", default " + strconv.Itoa(DefaultRowLimit) + ". " +
		"Example, the total cost per day of the observations of traces named checkout, " +
		"for the week to 2025-01-08 (set fromTimestamp and toTimestamp to the window you need): " + metricsQueryExample
}

// HasGuidance reports whether any parameter of the operation has guidance.
func (o Operation) HasGuidance() bool {
	return slices.ContainsFunc(o.Params, func(p Param) bool { return o.ParamGuidance(p) != "" })
}
