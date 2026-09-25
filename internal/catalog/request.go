package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Request is what the executor sends for one operation: the method, the
// escaped path below the host and the query.
type Request struct {
	Method string
	// Path is the operation's path with every path parameter percent-encoded.
	Path  string
	Query url.Values
}

// Request builds the operation's request from the caller's parameters, a map
// of parameter name to value. A value is a string, a number (float64 or
// json.Number), a bool, or, for a repeated query parameter, a list of those.
// Path parameters are percent-encoded into the path; every other parameter
// goes to the query.
func (o Operation) Request(params map[string]any) (Request, error) {
	req := Request{Method: o.Method, Path: o.Path, Query: url.Values{}}
	for _, p := range o.Params {
		if p.In != "path" {
			continue
		}
		v, ok := params[p.Name]
		if !ok {
			return Request{}, fmt.Errorf("parameter %s: required path parameter is missing", p.Name)
		}
		s, err := scalar(v)
		if err != nil {
			return Request{}, fmt.Errorf("parameter %s: %w", p.Name, err)
		}
		req.Path = strings.ReplaceAll(req.Path, "{"+p.Name+"}", url.PathEscape(s))
	}
	for name, v := range params {
		if o.isPathParam(name) {
			continue
		}
		values, ok := v.([]any)
		if !ok {
			values = []any{v}
		}
		for _, item := range values {
			s, err := scalar(item)
			if err != nil {
				return Request{}, fmt.Errorf("parameter %s: %w", name, err)
			}
			req.Query.Add(name, s)
		}
	}
	return req, nil
}

func (o Operation) isPathParam(name string) bool {
	for _, p := range o.Params {
		if p.In == "path" && p.Name == name {
			return true
		}
	}
	return false
}

// scalar renders one parameter value as it appears in a URL.
func scalar(v any) (string, error) {
	switch v := v.(type) {
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	}
	return "", fmt.Errorf("want a string, number or boolean, got %T", v)
}
