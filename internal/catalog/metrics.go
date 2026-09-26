package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math"
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

// rowLimitError is a row_limit outside 1..MaxRowLimit; it matches
// ErrRowLimitOutOfRange and, through the error it wraps, ErrInvalidParameter.
type rowLimitError struct{ error }

func (e rowLimitError) Unwrap() error      { return e.error }
func (rowLimitError) Is(target error) bool { return target == ErrRowLimitOutOfRange }

// metricsOperations are the operations whose "query" parameter is a metrics
// query JSON.
var metricsOperations = map[string]bool{
	"metrics_metrics":          true,
	"legacy_metricsV1_metrics": true,
}

// isMetricsQuery reports whether p is the JSON query of a metrics operation.
func (o Operation) isMetricsQuery(p Param) bool {
	return metricsOperations[o.ID] && p.In == "query" && p.Name == "query"
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

// metricsQueryKeys are the top-level keys of a metrics query, sorted, and
// the JSON type of each value.
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
		return "", invalidf("parameter query: want at most %d bytes of JSON, got %d bytes", maxMetricsQueryBytes, len(raw))
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
		return "", invalidf("parameter query: want a JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", invalidf("parameter query: want one JSON object, got data after it")
	}
	for _, name := range slices.Sorted(maps.Keys(q)) {
		v := q[name]
		want, known := metricsQueryKeys[name]
		if !known {
			return "", invalidf("parameter query: unknown top-level key; the keys of a metrics query are: %s",
				metricsQueryKeyNames())
		}
		if !want.has(v) {
			return "", invalidf("parameter query: %s: want %s, got %s", name, want, kind(v))
		}
	}
	config, _ := q["config"].(map[string]any)
	if config == nil {
		config = map[string]any{}
		q["config"] = config
	}
	rowLimit := DefaultRowLimit
	if v, ok := config["row_limit"]; ok {
		n, isNumber := number(v)
		if !isNumber || n != math.Trunc(n) || n < 1 || n > MaxRowLimit {
			return "", rowLimitError{invalidf("parameter query: config.row_limit: want an integer from 1 to %d, got %s",
				MaxRowLimit, kind(v))}
		}
		rowLimit = int(n)
	}
	config["row_limit"] = json.Number(strconv.Itoa(rowLimit))
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(q); err != nil {
		return "", invalidf("parameter query: cannot be re-encoded as JSON")
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
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
				return invalidf("parameter query: want JSON nested at most %d objects or lists deep", maxMetricsQueryDepth)
			}
		case json.Delim('}'), json.Delim(']'):
			depth--
			if depth == 0 {
				return nil
			}
		}
	}
}

// metricsQueryKeyNames lists the top-level keys of a metrics query.
func metricsQueryKeyNames() string {
	return strings.Join(slices.Sorted(maps.Keys(metricsQueryKeys)), ", ")
}
