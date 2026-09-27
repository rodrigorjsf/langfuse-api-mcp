// Package workflows holds the logic of the workflow tools: read flows that
// take several Langfuse calls. It uses the Langfuse client and sanitize, and
// knows nothing about MCP registration; the server registers each flow as a
// tool and translates its errors (ADR-0008, ADR-0009).
package workflows

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// ObservationsOperationID is the catalog operation the trace tree reads: the
// Observations v2 list (GET /api/public/v2/observations).
const ObservationsOperationID = "observations_getMany"

// observationsPath is the path of ObservationsOperationID below the host.
const observationsPath = "/api/public/v2/observations"

// Trace tree paging (ADR-0002 amendment): at most MaxTracePages pages of
// TracePageSize observations, the Observations v2 page size cap, so that one
// huge trace cannot spend the rate limit.
const (
	MaxTracePages = 5
	TracePageSize = 1000
)

// traceFields are the field groups every trace tree row carries: names,
// levels, status messages, timing, usage, model, cost and latency.
const traceFields = "core,basic,time,usage,model,metrics"

// Include selects the optional field groups of a trace tree.
type Include struct {
	// IO adds each observation's input and output.
	IO bool
	// Metadata adds each observation's metadata.
	Metadata bool
}

// fields returns the Observations v2 field groups for include.
func (in Include) fields() string {
	f := traceFields
	if in.IO {
		f += ",io"
	}
	if in.Metadata {
		f += ",metadata"
	}
	return f
}

// TraceTree is one trace as a trace tree, and what fetching it cost.
type TraceTree struct {
	// Payload is a page-shaped JSON object, sanitized and redacted:
	// "data" lists the observations in depth-first pre-order, each with its
	// depth and, for an extra root whose parent is missing, orphan: true;
	// "meta" holds observations (the row count), truncated (pages remain)
	// and, when truncated, the cursor of the next page.
	Payload json.RawMessage
	// Empty is set when the trace has no observations.
	Empty bool
	// More is set when pages remain after MaxTracePages.
	More bool
	// Requests is the number of Langfuse requests made, retries and the
	// failed one included.
	Requests int
	// Status is the HTTP status of the last Langfuse answer; 0 without one.
	Status int
	// Bytes is the size of every Langfuse response body read.
	Bytes int
}

// errUnexpectedPage marks an Observations v2 answer that is not a page.
var errUnexpectedPage = errors.New("the observations answer is not a page of observation objects")

// GetTraceTree reads every observation of the trace traceID through
// Observations v2, following meta.cursor for at most MaxTracePages pages, and
// orders them into a trace tree: roots and siblings by startTime ascending,
// children right after their parent, depth from 0. An observation whose
// parent is not among the rows becomes an extra root marked orphan. The
// payload is cleaned by sanitize.Payload and redacted by r. traceID is sent
// only as a query parameter. A failed request returns its error wrapped, with
// the TraceTree's Requests, Status and Bytes set.
func GetTraceTree(ctx context.Context, client *langfuse.Client, r sanitize.Redactor, traceID string, include Include) (TraceTree, error) {
	var tree TraceTree
	var rows []observation
	cursor := ""
	for page := 1; page <= MaxTracePages; page++ {
		query := url.Values{"traceId": {traceID}, "limit": {strconv.Itoa(TracePageSize)}, "fields": {include.fields()}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		resp, err := client.Do(ctx, http.MethodGet, observationsPath, query)
		tree.Requests += resp.Attempts
		tree.Status, tree.Bytes = resp.Status, tree.Bytes+len(resp.Body)
		if err != nil {
			return tree, fmt.Errorf("%s page %d: %w", ObservationsOperationID, page, err)
		}
		got, next, err := decodePage(resp.Body)
		if err != nil {
			return tree, fmt.Errorf("%s page %d: %w", ObservationsOperationID, page, err)
		}
		rows, cursor = append(rows, got...), next
		if cursor == "" {
			break
		}
	}
	tree.Empty, tree.More = len(rows) == 0, cursor != ""
	payload, err := json.Marshal(tracePage{
		Data: preOrder(rows),
		Meta: traceMeta{Observations: len(rows), Truncated: tree.More, Cursor: cursorIf(tree.More, cursor)},
	})
	if err != nil {
		return tree, err
	}
	tree.Payload, err = sanitize.Payload(payload, r)
	return tree, err
}

// cursorIf returns cursor when more pages remain, else "".
func cursorIf(more bool, cursor string) string {
	if more {
		return cursor
	}
	return ""
}

// tracePage is the page-shaped trace tree payload.
type tracePage struct {
	Data []map[string]json.RawMessage `json:"data"`
	Meta traceMeta                    `json:"meta"`
}

type traceMeta struct {
	Observations int    `json:"observations"`
	Truncated    bool   `json:"truncated"`
	Cursor       string `json:"cursor,omitempty"`
}

// observation is one Observations v2 row: its fields as Langfuse sent them,
// plus what ordering needs.
type observation struct {
	fields map[string]json.RawMessage
	id     string // "" when the row has no string id
	parent string // "" for a root
	start  time.Time
	// hasStart is false when startTime is missing or not a timestamp.
	hasStart bool
}

// decodePage decodes one Observations v2 page: its rows and the next cursor,
// "" when it is the last page.
func decodePage(body json.RawMessage) ([]observation, string, error) {
	var page struct {
		Data []json.RawMessage `json:"data"`
		Meta struct {
			Cursor *string `json:"cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", errUnexpectedPage
	}
	rows := make([]observation, 0, len(page.Data))
	for _, raw := range page.Data {
		var o observation
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&o.fields); err != nil || o.fields == nil {
			return nil, "", errUnexpectedPage
		}
		o.id = stringField(o.fields["id"])
		o.parent = stringField(o.fields["parentObservationId"])
		o.start, o.hasStart = timeField(o.fields["startTime"])
		rows = append(rows, o)
	}
	next := ""
	if page.Meta.Cursor != nil {
		next = *page.Meta.Cursor
	}
	return rows, next, nil
}

// stringField returns raw as a string, or "" when it is not one.
func stringField(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// timeField parses raw as an RFC 3339 timestamp.
func timeField(raw json.RawMessage) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, stringField(raw))
	return t, err == nil
}

// byStart orders observations by startTime ascending; a row without a
// startTime comes last; ties go by id, then by the order Langfuse sent.
func byStart(rows []observation) func(a, b int) int {
	return func(a, b int) int {
		x, y := rows[a], rows[b]
		switch {
		case x.hasStart && y.hasStart:
			if c := x.start.Compare(y.start); c != 0 {
				return c
			}
		case x.hasStart != y.hasStart:
			if x.hasStart {
				return -1
			}
			return 1
		}
		if c := strings.Compare(x.id, y.id); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	}
}

// preOrder returns the rows in depth-first pre-order, each with its depth,
// and orphan: true on an extra root whose parent is missing; it writes both
// into the rows' own field maps. A row caught in
// a parent cycle is never reached from a root; it too becomes an orphan root,
// so no row is dropped.
func preOrder(rows []observation) []map[string]json.RawMessage {
	present := make(map[string]bool, len(rows))
	for _, o := range rows {
		if o.id != "" {
			present[o.id] = true
		}
	}
	children := make(map[string][]int)
	var roots []int
	for i, o := range rows {
		if o.parent == "" || !present[o.parent] {
			roots = append(roots, i)
			continue
		}
		children[o.parent] = append(children[o.parent], i)
	}
	order := byStart(rows)
	slices.SortFunc(roots, order)
	for _, kids := range children {
		slices.SortFunc(kids, order)
	}

	out := make([]map[string]json.RawMessage, 0, len(rows))
	visited := make([]bool, len(rows))
	var visit func(i, depth int, orphan bool)
	visit = func(i, depth int, orphan bool) {
		visited[i] = true
		o := rows[i]
		o.fields["depth"] = json.RawMessage(strconv.Itoa(depth))
		if orphan {
			o.fields["orphan"] = json.RawMessage("true")
		}
		out = append(out, o.fields)
		if o.id == "" {
			return
		}
		for _, c := range children[o.id] {
			if !visited[c] {
				visit(c, depth+1, false)
			}
		}
	}
	for _, i := range roots {
		visit(i, 0, rows[i].parent != "")
	}
	rest := make([]int, 0, len(rows)-len(out))
	for i := range rows {
		if !visited[i] {
			rest = append(rest, i)
		}
	}
	slices.SortFunc(rest, order)
	for _, i := range rest {
		if !visited[i] {
			visit(i, 0, true)
		}
	}
	return out
}
