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
// maxMetricsQueryBytes long and nested at most maxMetricsQueryDepth objects
// and lists deep (the query object itself is depth 1).
const (
	maxMetricsQueryBytes = 16 << 10
	maxMetricsQueryDepth = 10
)

// metricsQueryKeys are the top-level keys of a metrics query and the JSON
// type each value must have ("object?" and "list?" also accept null).
var metricsQueryKeys = []struct{ name, kind string }{
	{"view", "string"},
	{"dimensions", "list"},
	{"metrics", "list"},
	{"filters", "list"},
	{"timeDimension", "object?"},
	{"fromTimestamp", "string"},
	{"toTimestamp", "string"},
	{"orderBy", "list?"},
	{"config", "object?"},
}

// metricsQuery checks a metrics query JSON and returns it re-encoded, with
// config.row_limit defaulted. The query must be one JSON object within the
// size and depth bounds, with only the keys of metricsQueryKeys, each of its
// type. Error messages name the key and the reason, never the caller's text.
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
	if err := dec.Decode(&q); err != nil || q == nil {
		return "", invalidf("parameter query: want a JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", invalidf("parameter query: want one JSON object, got data after it")
	}
	for _, name := range slices.Sorted(maps.Keys(q)) {
		v := q[name]
		i := slices.IndexFunc(metricsQueryKeys, func(k struct{ name, kind string }) bool { return k.name == name })
		if i < 0 {
			return "", invalidf("parameter query: unknown top-level key; the keys of a metrics query are: %s",
				metricsQueryKeyNames())
		}
		if want := metricsQueryKeys[i].kind; !hasJSONKind(v, want) {
			return "", invalidf("parameter query: %s: want %s, got %s", name, describeKind(want), kind(v))
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

// hasJSONKind reports whether a decoded value has the JSON type kind names.
func hasJSONKind(v any, want string) bool {
	nullable := strings.HasSuffix(want, "?")
	if v == nil {
		return nullable
	}
	switch strings.TrimSuffix(want, "?") {
	case "string":
		_, ok := v.(string)
		return ok
	case "list":
		_, ok := v.([]any)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	}
	return false
}

// describeKind names a metricsQueryKeys kind for an error message.
func describeKind(want string) string {
	name := map[string]string{"string": "a string", "list": "a list", "object": "an object"}[strings.TrimSuffix(want, "?")]
	if strings.HasSuffix(want, "?") {
		return name + " or null"
	}
	return name
}

// metricsQueryKeyNames lists the top-level keys of a metrics query.
func metricsQueryKeyNames() string {
	names := make([]string, 0, len(metricsQueryKeys))
	for _, k := range metricsQueryKeys {
		names = append(names, k.name)
	}
	return strings.Join(names, ", ")
}
