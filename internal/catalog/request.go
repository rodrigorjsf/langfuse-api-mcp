package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
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

// ErrParameterNotServed marks a parameter that is one of the operation's but
// that the deployment's Langfuse version does not declare (#173); it also
// matches ErrInvalidParameter. The message names the parameter, the operation,
// the deployment's version and the release that serves the parameter, taken
// from the catalog, never from the caller.
var ErrParameterNotServed = errors.New("parameter not served by the deployment's version")

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
		if o.hasParam(name) {
			continue
		}
		if p, ok := o.unservedParam(name); ok {
			return Request{}, sentinelError{ErrParameterNotServed, invalidf(
				"parameter %s: operation %s does not serve it on this deployment (%s); Langfuse serves it %s",
				strconv.Quote(p.Name), o.ID, o.deploymentText(), servedRange(p))}
		}
		return Request{}, invalidf("parameter %s: unknown parameter for operation %s; its parameters are: %s",
			strconv.Quote(truncate(name)), o.ID, o.paramNames())
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
			switch {
			case o.isMetricsQuery(p):
				// A metrics query that is not a string (an object, a list, a
				// number) gets the metrics query refusal, whose hint says how
				// the query is sent as a string (#106).
				return Request{}, queryInvalidf("parameter %s: %s", p.Name, err.Error())
			case o.isJSONFilter(p):
				// The same for a JSON filter (#195).
				return Request{}, filterInvalidf("parameter %s: %s", p.Name, err.Error())
			}
			return Request{}, invalidf("parameter %s: %s", p.Name, err.Error())
		}
		if o.isListLimit(p) {
			if _, ok := parsePageSize(values[0], MaxLimit); !ok {
				return Request{}, sentinelError{ErrLimitOutOfRange, invalidf("parameter %s: want an integer from 1 to %d",
					p.Name, MaxLimit)}
			}
		}
		if err := p.inBounds(v, values); err != nil {
			return Request{}, invalidf("parameter %s: %s", p.Name, err.Error())
		}
		switch {
		case o.isMetricsQuery(p):
			// metricsQuery checks the text of the strings it decodes.
			q, err := metricsQuery(values[0])
			if err != nil {
				return Request{}, err
			}
			values = []string{q}
		case o.isJSONFilter(p):
			text, err := jsonFilter(p.Name, values[0])
			if err != nil {
				return Request{}, err
			}
			values = []string{text}
		case p.In == "query":
			if err := p.checkText(values); err != nil {
				return Request{}, invalidf("parameter %s: %s", p.Name, err.Error())
			}
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
// a Folder name), a "." or ".." segment, a control or invisible character, or
// a leading http/https scheme. A hidden character is refused, never removed:
// removing it would look up an ID the caller did not send (#179). The
// messages name the rule, never the value.
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
	case strings.ContainsFunc(v, hidden):
		return errors.New(hiddenTextRule)
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

// sentinelError is a refusal that also matches a more specific sentinel: a page
// size outside its range (limit or config.row_limit) or a malformed metrics
// query. It matches its sentinel and, through the error it wraps,
// ErrInvalidParameter.
type sentinelError struct {
	sentinel error
	error
}

func (e sentinelError) Unwrap() error        { return e.error }
func (e sentinelError) Is(target error) bool { return target == e.sentinel }

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

// isPageNumber reports whether p is the page number of a list operation: the
// "page" query parameter. Pages start at 1.
func isPageNumber(p Param) bool {
	return p.In == "query" && p.Name == "page"
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidParameter}, args...)...)
}

// HasQueryParam reports whether the operation takes the query parameter called
// name on this deployment (a served parameter, #173).
func (o Operation) HasQueryParam(name string) bool {
	return slices.ContainsFunc(o.Params, func(p Param) bool { return p.In == "query" && p.Name == name })
}

// WithoutQueryParam returns the operation as if the deployment did not serve the
// query parameter called name, for a tool that wraps the operation but takes no
// such argument (the hints of its failures must not recommend it). The
// operation itself is unchanged.
func (o Operation) WithoutQueryParam(name string) Operation {
	o.Params = slices.DeleteFunc(slices.Clone(o.Params), func(p Param) bool { return p.In == "query" && p.Name == name })
	return o
}

// HasTimeFilter reports whether the operation takes a query parameter that
// bounds time (the spec gives it the format date-time), which narrows what a
// list returns.
func (o Operation) HasTimeFilter() bool {
	return slices.ContainsFunc(o.Params, func(p Param) bool { return p.In == "query" && p.Schema.Format == "date-time" })
}

func (o Operation) hasParam(name string) bool {
	return slices.ContainsFunc(o.Params, func(p Param) bool { return p.Name == name })
}

// unservedParam returns the operation's parameter called name that the
// deployment's version does not declare.
func (o Operation) unservedParam(name string) (Param, bool) {
	i := slices.IndexFunc(o.unserved, func(p Param) bool { return p.Name == name })
	if i < 0 {
		return Param{}, false
	}
	return o.unserved[i], true
}

// deploymentText names the deployment's Langfuse version for a refusal.
func (o Operation) deploymentText() string {
	if o.deployment == "" {
		return "Langfuse version not detected, so the newest release's parameters are served"
	}
	return "Langfuse " + o.deployment
}

// servedRange says in which releases Langfuse serves p: "from 3.133.0",
// "up to before 3.100.0" or both.
func servedRange(p Param) string {
	switch {
	case p.Removed == "":
		return "from " + p.Introduced
	case p.Introduced == "" || p.Introduced == SupportedFloor:
		return "up to before " + p.Removed
	}
	return "from " + p.Introduced + " up to before " + p.Removed
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

// checkText checks the text of the rendered values of a query parameter
// (#179): none may hold a control or invisible character, and where the schema
// says date-time each must be an RFC 3339 timestamp. The message names the
// rule or the format, never the value; a repeated parameter's message names
// the item.
func (p Param) checkText(values []string) error {
	s := p.Schema
	if s.Type == "array" {
		s = Schema{}
		if p.Schema.Items != nil {
			s = *p.Schema.Items
		}
	}
	for i, value := range values {
		err := s.checkText(value)
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

// hiddenTextRule is the refusal of a value that holds a control or invisible
// character (see holdsHiddenText). It names the rule, never the value.
const hiddenTextRule = "must not contain control or invisible characters"

// holdsHiddenText reports whether s holds a control character or a character
// that hides or reorders text (see hidden). With keepWhitespace, tab, line feed
// and carriage return are not counted.
func holdsHiddenText(s string, keepWhitespace bool) bool {
	if keepWhitespace {
		return strings.ContainsFunc(s, hiddenOutsideWhitespace)
	}
	return strings.ContainsFunc(s, hidden)
}

// rfc3339 is the shape of an RFC 3339 timestamp (section 5.6: date, "T", time
// with seconds, an optional fraction and a "Z" or numeric offset). time.Parse
// accepts looser forms (a one-digit hour, a comma before the fraction), so the
// shape is checked first and time.Parse then checks the ranges.
var rfc3339 = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)

// checkText is Param.checkText for one rendered value.
func (s Schema) checkText(v string) error {
	if holdsHiddenText(v, false) {
		return errors.New(hiddenTextRule)
	}
	if s.Format == "date-time" {
		if _, err := time.Parse(time.RFC3339, v); err != nil || !rfc3339.MatchString(v) {
			return errors.New("want a date-time in RFC 3339 format, such as 2026-10-10T12:00:00Z")
		}
	}
	return nil
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
