package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Request is what the executor sends for one operation: the method, the
// escaped path below the host and the query.
type Request struct {
	Method string
	// Path is the operation's path with every path parameter percent-encoded.
	Path  string
	Query url.Values
	// FolderName is set when a path parameter holds a Folder name: a value
	// with "/" on a folder-capable parameter, sent as %2F.
	FolderName bool
}

// ErrInvalidParameter marks every error Request returns: the caller's
// parameters do not fit the operation. The message names the parameter and
// the reason.
var ErrInvalidParameter = errors.New("invalid parameter")

// ErrLimitOutOfRange marks a limit outside 1..MaxLimit on a list operation;
// it also matches ErrInvalidParameter.
var ErrLimitOutOfRange = errors.New("limit out of range")

// List operations — the read operations with a "limit" query parameter — get
// DefaultLimit when the caller names no limit, and refuse a limit above
// MaxLimit. MaxLimit is the lowest page size cap Langfuse enforces on a list
// operation (scores and legacy routes answer 400 above 100), so an accepted
// limit is never refused upstream; it also keeps a page small.
const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// maxNameInMessage bounds how much of a caller-supplied parameter name an
// error message repeats.
const maxNameInMessage = 64

// Request builds the operation's request from the caller's parameters, a map
// of parameter name to value, after validating them against the operation:
// every parameter must be one of the operation's, every required one must be
// present, and every value must have the parameter's type and lie within
// the numeric and length bounds its schema gives. A value is a
// string, a number (float64 or json.Number), a bool, or, for a repeated query
// parameter, a list of those; JSON null omits a nullable parameter. Path
// parameters are percent-encoded into the path; the others go to the query.
// Every error wraps ErrInvalidParameter.
func (o Operation) Request(params map[string]any) (Request, error) {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !o.hasParam(name) {
			return Request{}, invalidf("parameter %s: unknown parameter for operation %s; its parameters are: %s",
				strconv.Quote(truncate(name)), o.ID, o.paramNames())
		}
	}

	req := Request{Method: o.Method, Path: o.Path, Query: url.Values{}}
	for _, p := range o.Params {
		v, present := params[p.Name]
		if present && v == nil && p.Schema.Nullable {
			present = false // null omits a nullable parameter
		}
		if !present && o.isListLimit(p) {
			req.Query.Set(p.Name, strconv.Itoa(DefaultLimit))
			continue
		}
		if !present {
			if p.Required {
				return Request{}, invalidf("parameter %s: required parameter is missing", p.Name)
			}
			continue
		}
		values, err := p.values(v)
		if err != nil {
			return Request{}, invalidf("parameter %s: %s", p.Name, err.Error())
		}
		if o.isListLimit(p) {
			if _, ok := parsePageSize(values[0], MaxLimit); !ok {
				return Request{}, rangeError{ErrLimitOutOfRange, invalidf("parameter %s: want an integer from 1 to %d",
					p.Name, MaxLimit)}
			}
		}
		if err := p.inBounds(v, values); err != nil {
			return Request{}, invalidf("parameter %s: %s", p.Name, err.Error())
		}
		if o.isMetricsQuery(p) {
			q, err := metricsQuery(values[0])
			if err != nil {
				return Request{}, err
			}
			values = []string{q}
		}
		if p.In == "path" {
			if err := safePathValue(values[0], o.takesFolderName(p)); err != nil {
				return Request{}, invalidf("parameter %s: %s", p.Name, err.Error())
			}
			req.FolderName = req.FolderName || strings.Contains(values[0], "/")
			req.Path = strings.ReplaceAll(req.Path, "{"+p.Name+"}", url.PathEscape(values[0]))
			continue
		}
		for _, s := range values {
			req.Query.Add(p.Name, s)
		}
	}
	return req, nil
}

// safePathValue refuses a path parameter value that could change which
// resource is requested or where the request goes, even before it is
// percent-encoded: an empty value, a "\\", a "/" (unless the parameter takes
// a Folder name), a "." or ".." segment, a control character, or a leading
// http/https scheme. The messages name the rule, never the value.
//
// A Folder name may hold "/" between non-empty segments, which rules out a
// leading or trailing "/" and "//" (so also "scheme://host"); url.PathEscape
// then sends every "/" as %2F, inside one path segment. Two dots inside a
// segment ("v1..2") are harmless and pass. Other "name:" prefixes pass on
// purpose: IDs such as "user:42" are legitimate, and after percent-encoding a
// scheme cannot name a host.
func safePathValue(v string, folderName bool) error {
	lower := strings.ToLower(v)
	switch {
	case v == "":
		return errors.New("must not be empty")
	case strings.Contains(v, `\`):
		return errors.New(`must not contain "\"`)
	case strings.Contains(v, "/") && !folderName:
		return errors.New(`must not contain "/": a path parameter is a single ID or name`)
	case strings.HasPrefix(lower, "http:") || strings.HasPrefix(lower, "https:"):
		return errors.New("must not be a URL: pass the ID or name only")
	case strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return errors.New("must not contain control characters")
	}
	for segment := range strings.SplitSeq(v, "/") {
		switch segment {
		case "":
			return errors.New(`a Folder name must not start or end with "/" or contain "//": ` +
				`its folders and name are non-empty segments separated by single "/"`)
		case ".", "..":
			return errors.New(`must not be or contain a "." or ".." segment: those are relative path segments`)
		}
	}
	return nil
}

// rangeError is a page size outside its range (limit or config.row_limit);
// it matches its sentinel and, through the error it wraps,
// ErrInvalidParameter.
type rangeError struct {
	sentinel error
	error
}

func (e rangeError) Unwrap() error        { return e.error }
func (e rangeError) Is(target error) bool { return target == e.sentinel }

// parsePageSize parses s as a page size: a decimal integer from 1 to upper.
func parsePageSize(s string, upper int) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 1 && n <= upper
}

// isListLimit reports whether p is the page size of a list operation: the
// "limit" query parameter of a read. The metrics operations bound their page
// size as config.row_limit inside their JSON query instead (metricsQuery).
func (o Operation) isListLimit(p Param) bool {
	return o.IsRead() && p.In == "query" && p.Name == "limit"
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidParameter}, args...)...)
}

func (o Operation) hasParam(name string) bool {
	return slices.ContainsFunc(o.Params, func(p Param) bool { return p.Name == name })
}

// paramNames lists the operation's parameter names, or "none".
func (o Operation) paramNames() string {
	if len(o.Params) == 0 {
		return "none"
	}
	names := make([]string, 0, len(o.Params))
	for _, p := range o.Params {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// values renders the parameter's value as it appears in a URL: one string, or
// one per item of a repeated query parameter. A single value for a repeated
// parameter is a list of one.
func (p Param) values(v any) ([]string, error) {
	if p.Schema.Type != "array" {
		s, err := p.Schema.scalar(v)
		if err != nil {
			return nil, err
		}
		return []string{s}, nil
	}
	items := listItems(v)
	item := Schema{}
	if p.Schema.Items != nil {
		item = *p.Schema.Items
	}
	out := make([]string, 0, len(items))
	for i, it := range items {
		s, err := item.scalar(it)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		out = append(out, s)
	}
	return out, nil
}

// listItems returns the items of a repeated parameter's value: the list, or
// a list of one for a single value. values and inBounds both walk it, so the
// rendered values and the raw items line up.
func listItems(v any) []any {
	if items, ok := v.([]any); ok {
		return items
	}
	return []any{v}
}

// scalar checks one value against the schema's type and allowed values and
// renders it as it appears in a URL.
func (s Schema) scalar(v any) (string, error) {
	var out string
	switch s.Type {
	case "string":
		str, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("want a string, got %s", kind(v))
		}
		out = str
	case "integer":
		n, ok := number(v)
		if !ok || n != math.Trunc(n) || math.Abs(n) > 1<<53 {
			return "", fmt.Errorf("want an integer, got %s", kind(v))
		}
		out = strconv.FormatFloat(n, 'f', -1, 64)
	case "number":
		n, ok := number(v)
		if !ok {
			return "", fmt.Errorf("want a number, got %s", kind(v))
		}
		out = strconv.FormatFloat(n, 'f', -1, 64)
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return "", fmt.Errorf("want a boolean, got %s", kind(v))
		}
		out = strconv.FormatBool(b)
	default:
		str, err := anyScalar(v)
		if err != nil {
			return "", err
		}
		out = str
	}
	if len(s.Enum) > 0 && !slices.Contains(s.Enum, out) {
		return "", fmt.Errorf("want one of %s", strings.Join(s.Enum, ", "))
	}
	return out, nil
}

// inBounds checks the parameter's rendered values against the bounds its
// spec gives (#80): Minimum and Maximum for a number, MinLength and MaxLength,
// in runes, for a string; the items' bounds for a repeated parameter. v is the
// caller's value the values were rendered from: a schema without a type
// bounds each value by its JSON kind (#83). It runs after the list limit
// check, so a limit keeps its own error. The message names the bound, never
// the value.
func (p Param) inBounds(v any, values []string) error {
	s := p.Schema
	raw := []any{v}
	if s.Type == "array" {
		if s.Items == nil {
			return nil
		}
		s = *s.Items
		raw = listItems(v)
	}
	for i, value := range values {
		err := s.withinBounds(value, raw[i])
		if err == nil {
			continue
		}
		if p.Schema.Type == "array" {
			return fmt.Errorf("item %d: %w", i, err)
		}
		return err
	}
	return nil
}

// withinBounds checks one rendered value against the schema's bounds. A
// schema without a type takes the kind of raw, the value before rendering:
// a JSON number meets the numeric bounds, a string the length bounds, and a
// boolean none.
func (s Schema) withinBounds(v string, raw any) error {
	kind := s.Type
	if kind == "" {
		if _, ok := raw.(string); ok {
			kind = "string"
		} else if _, ok := number(raw); ok {
			kind = "number"
		}
	}
	switch kind {
	case "integer", "number":
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return errors.New("want a number") // unreachable: scalar renders finite numbers only
		}
		if (s.Minimum == nil || n >= *s.Minimum) && (s.Maximum == nil || n <= *s.Maximum) {
			return nil
		}
		return errors.New("want a value " + boundText(s.Minimum, s.Maximum, formatBound))
	case "string":
		n := utf8.RuneCountInString(v)
		if (s.MinLength == nil || n >= *s.MinLength) && (s.MaxLength == nil || n <= *s.MaxLength) {
			return nil
		}
		return errors.New("want a length " + boundText(s.MinLength, s.MaxLength, strconv.Itoa) + " characters")
	}
	return nil
}

// boundText renders a closed or half-open range: "from lo to hi", "of at
// least lo" or "of at most hi" ("of any size" without bounds).
func boundText[T any](lo, hi *T, format func(T) string) string {
	switch {
	case lo != nil && hi != nil:
		return "from " + format(*lo) + " to " + format(*hi)
	case lo != nil:
		return "of at least " + format(*lo)
	case hi != nil:
		return "of at most " + format(*hi)
	}
	return "of any size"
}

func formatBound(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// number returns the value of a JSON number.
func number(v any) (float64, bool) {
	switch v := v.(type) {
	case json.Number:
		f, err := v.Float64()
		return f, err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
	case float64:
		return v, !math.IsInf(v, 0) && !math.IsNaN(v)
	}
	return 0, false
}

// anyScalar renders a value of a parameter whose schema names no type.
func anyScalar(v any) (string, error) {
	switch v := v.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	}
	if n, ok := number(v); ok {
		return strconv.FormatFloat(n, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("want a string, number or boolean, got %s", kind(v))
}

// kind names the JSON type of a decoded value for an error message.
func kind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case json.Number, float64:
		return "a number"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	}
	return fmt.Sprintf("%T", v)
}

// truncate bounds a caller-supplied string repeated in an error message.
func truncate(s string) string {
	if len(s) <= maxNameInMessage {
		return s
	}
	return strings.ToValidUTF8(s[:maxNameInMessage], "") + "…"
}
