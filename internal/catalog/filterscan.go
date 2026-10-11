package catalog

import (
	"errors"
	"slices"
	"strconv"
)

// ScannedFilter is one pair of the closed list of filters the server applies
// itself on a deployment whose Langfuse version does not serve them (ADR-0012
// amendment of 2026-10-10, ADR-0010 amendment): the operation, the filter
// parameter, and the field of each row of the list that the filter's value is
// compared with.
type ScannedFilter struct {
	OperationID string
	Parameter   string
	// RowField is the field of a row that equals the filter's value when the
	// row matches. It is never decided from a payload: an empty page shows no
	// field.
	RowField string
}

// scannedFilters is the closed list: exactly the two filters the campaign
// proved on Langfuse 3.80.0 (finding H1), which ignores each and answers the
// unfiltered list. Adding a pair is a decision of its own, with its row field.
// An operation description lists only served parameters; it shows these filters
// apart from them, on a deployment that scans them (ScannedParams, #199).
var scannedFilters = []ScannedFilter{
	{OperationID: "scores_get-many", Parameter: "traceId", RowField: "traceId"},
	{OperationID: "legacy_observationsV1_getMany", Parameter: "level", RowField: "level"},
}

// ScannedFilters returns the closed list of filter scan pairs.
func ScannedFilters() []ScannedFilter { return slices.Clone(scannedFilters) }

// ScannedParams returns the filters of the operation that the server applies
// itself on this deployment: the pairs of the closed list (ScannedFilters) for
// this operation whose parameter the deployment's version does not serve, as
// the catalog holds them (name, location, type, allowed values and bounds). It
// is empty for every other operation and on a deployment that serves the
// parameter or whose version was not detected. They are never in Params, so a
// parameter is in one list at most. The answer depends on the closed list and
// the version resolved at startup only.
func (o Operation) ScannedParams() []Param {
	var out []Param
	for _, f := range scannedFilters {
		if p, ok := o.scannedParam(f); ok {
			out = append(out, p)
		}
	}
	return out
}

// scannedParam returns the parameter of pair f when the pair belongs to this
// operation and the deployment's version does not serve the parameter.
func (o Operation) scannedParam(f ScannedFilter) (Param, bool) {
	if f.OperationID != o.ID {
		return Param{}, false
	}
	return o.unservedParam(f.Parameter)
}

// ErrPageWithFilterScan marks a page chosen together with a filter the server
// applies by reading the list from its first page; it also matches
// ErrInvalidParameter.
var ErrPageWithFilterScan = errors.New("page together with a scanned filter")

// Scan is what the executor needs to answer a call with a filter scan: the
// request of every page but the page number, and what to compare.
type Scan struct {
	// Request is the call's request without the filter and without a page: the
	// caller's other parameters, checked like any call's, with the page size at
	// its maximum. The executor adds the page number.
	Request Request
	// Parameter is the filter's name and RowField the field of each row its
	// value is compared with; both are catalog text.
	Parameter, RowField string
	// Value is the filter's value, checked like a value that is sent.
	Value string
}

// Scan reports whether the call needs a filter scan, and if so returns it: the
// call carries a filter of the closed list (ScannedFilters) that this
// deployment's version does not serve. Where the version serves the filter, or
// the call carries none, it reports false and the call is one ordinary request
// (Request). The call is checked as if the deployment served the filter, so the
// scanned value gets every check of a sent one (type, allowed values, bounds,
// control and invisible characters), and so do the other parameters. Every
// error wraps ErrInvalidParameter. A JSON null filter is no filter: it falls
// through to Request.
func (o Operation) Scan(params map[string]any) (Scan, bool, error) {
	f, p, ok := o.scannedFilter(params)
	if !ok {
		return Scan{}, false, nil
	}
	if params["page"] != nil {
		return Scan{}, false, sentinelError{ErrPageWithFilterScan, invalidf(
			"parameter page: not accepted together with %s: this deployment (%s) does not serve that filter, "+
				"so the server applies it by reading the list from its first page itself",
			p.Name, o.deploymentText())}
	}
	served := o
	served.Params = append(slices.Clone(o.Params), p)
	served.unserved = slices.DeleteFunc(slices.Clone(o.unserved), func(u Param) bool { return u.Name == p.Name })
	req, err := served.Request(params)
	if err != nil {
		return Scan{}, false, err
	}
	value := req.Query.Get(p.Name)
	req.Query.Del(p.Name)
	if o.HasQueryParam("limit") {
		req.Query.Set("limit", strconv.Itoa(MaxLimit))
	}
	return Scan{Request: req, Parameter: f.Parameter, RowField: f.RowField, Value: value}, true, nil
}

// scannedFilter returns the pair of the closed list, and the parameter it names,
// that the call carries and this deployment does not serve.
func (o Operation) scannedFilter(params map[string]any) (ScannedFilter, Param, bool) {
	for _, f := range scannedFilters {
		if params[f.Parameter] == nil {
			continue
		}
		if p, ok := o.scannedParam(f); ok {
			return f, p, true
		}
	}
	return ScannedFilter{}, Param{}, false
}
