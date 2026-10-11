package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// MaxScanPages is the most pages one filter scan reads (ADR-0012 amendment of
// 2026-10-10), whatever the call asks: no argument reaches it.
const MaxScanPages = 10

// FilterScanInput is a filter scan to run: a list operation read page by page
// without the filter that its deployment does not serve.
type FilterScanInput struct {
	// Path is the operation's path below the host.
	Path string
	// Query is the query of every page but the page number, which the scan adds;
	// it holds no filter.
	Query url.Values
	// Parameter names the filter, for the summary; RowField is the field of each
	// row that Value is compared with. Both are catalog text.
	Parameter, RowField string
	// Value is the filter's value, already checked like a value that is sent.
	Value string
}

// FilterScanResult is the answer to a filter scan, and what reading it cost.
type FilterScanResult struct {
	// Payload is a page-shaped JSON object: "data" lists the rows whose field
	// equals the value, in the order Langfuse sent them, and "meta.filterScan"
	// is the summary. Langfuse's own pagination metadata is not in it: it
	// described the unfiltered list, not these rows. The rows are sanitized and
	// redacted; the summary is static text and numbers.
	Payload json.RawMessage
	// Complete is set when the scan reached the end of the list.
	Complete bool
	// Requests is the number of Langfuse requests made, retries and the failed
	// one included.
	Requests int
	// Status is the HTTP status of the last Langfuse answer; 0 without one.
	Status int
	// Bytes is the size of every Langfuse response body read.
	Bytes int
}

// errUnexpectedScanPage marks an answer that is not a page of objects.
var errUnexpectedScanPage = errors.New("the answer is not a page of row objects")

// scanSummary is what a filter scan tells the agent about itself: that the
// server applied the filter, whether it read every row, and how many rows it
// read of how many exist.
type scanSummary struct {
	AppliedBy string `json:"appliedBy"`
	Parameter string `json:"parameter"`
	RowField  string `json:"rowField"`
	Complete  bool   `json:"complete"`
	PagesRead int    `json:"pagesRead"`
	RowsRead  int    `json:"rowsRead"`
	// RowsExist is Langfuse's count of the rows of the unfiltered list; absent
	// when Langfuse gave none and the scan did not reach the end.
	RowsExist   *int `json:"rowsExist,omitempty"`
	RowsMatched int  `json:"rowsMatched"`
}

// scanPayload is the page-shaped payload of a filter scan.
type scanPayload struct {
	Data []json.RawMessage `json:"data"`
	Meta struct {
		FilterScan scanSummary `json:"filterScan"`
	} `json:"meta"`
}

// FilterScan reads the list in.Path page by page, from page 1, for at most
// MaxScanPages pages, and keeps the rows whose RowField equals Value. It stops
// at the end of the list: the page that Langfuse's meta.totalPages says is the
// last, or, when it gives none, the first empty page. Each page is sanitized
// and redacted before its rows are compared, so a row is compared as the agent
// would read it; the matching rows are returned as such, only data. A failed
// page ends the scan with its error wrapped, the FilterScanResult's Requests,
// Status and Bytes set and no payload: nothing partial is presented as an
// answer.
func FilterScan(ctx context.Context, client *langfuse.Client, r sanitize.Redactor, in FilterScanInput) (FilterScanResult, error) {
	var res FilterScanResult
	out := scanPayload{Data: []json.RawMessage{}}
	sum := &out.Meta.FilterScan
	*sum = scanSummary{AppliedBy: "server", Parameter: in.Parameter, RowField: in.RowField}
	exist := -1 // Langfuse's count of rows, -1 until a page gives one
	for page := 1; page <= MaxScanPages; page++ {
		query := url.Values{}
		for name, values := range in.Query {
			query[name] = append([]string(nil), values...)
		}
		query.Set("page", strconv.Itoa(page))
		resp, err := client.Do(ctx, http.MethodGet, in.Path, query, nil)
		res.Requests += resp.Attempts
		res.Status, res.Bytes = resp.Status, res.Bytes+len(resp.Body)
		if err != nil {
			return res, fmt.Errorf("filter scan page %d: %w", page, err)
		}
		clean, err := sanitize.Payload(resp.Body, r)
		if err != nil {
			return res, fmt.Errorf("filter scan page %d: %w", page, err)
		}
		rows, meta, err := decodeScanPage(clean)
		if err != nil {
			return res, fmt.Errorf("filter scan page %d: %w", page, err)
		}
		sum.PagesRead, sum.RowsRead = page, sum.RowsRead+len(rows)
		for _, row := range rows {
			if value, ok := rowField(row.fields, in.RowField); ok && value == in.Value {
				out.Data = append(out.Data, row.raw)
			}
		}
		if meta.totalItems != nil {
			exist = *meta.totalItems
		}
		if endOfList(meta, len(rows), page) {
			sum.Complete = true
			break
		}
	}
	sum.RowsMatched = len(out.Data)
	switch {
	case exist >= 0:
		sum.RowsExist = &exist
	case sum.Complete:
		read := sum.RowsRead
		sum.RowsExist = &read
	}
	payload, err := encodeScanPayload(out)
	if err != nil {
		return res, fmt.Errorf("filter scan: %w", err)
	}
	res.Payload, res.Complete = payload, sum.Complete
	return res, nil
}

// scanRow is one row of a page: the row as sanitized JSON, and its fields.
type scanRow struct {
	raw    json.RawMessage
	fields map[string]json.RawMessage
}

// scanMeta is the part of a page's meta that a scan reads.
type scanMeta struct {
	totalItems, totalPages *int
}

// decodeScanPage decodes one sanitized page: its rows, which must be objects,
// and Langfuse's counts.
func decodeScanPage(body json.RawMessage) ([]scanRow, scanMeta, error) {
	var page struct {
		Data []json.RawMessage `json:"data"`
		Meta struct {
			TotalItems *int `json:"totalItems"`
			TotalPages *int `json:"totalPages"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &page); err != nil || page.Data == nil {
		return nil, scanMeta{}, errUnexpectedScanPage
	}
	rows := make([]scanRow, 0, len(page.Data))
	for _, raw := range page.Data {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			return nil, scanMeta{}, errUnexpectedScanPage
		}
		rows = append(rows, scanRow{raw: raw, fields: fields})
	}
	return rows, scanMeta{totalItems: page.Meta.TotalItems, totalPages: page.Meta.TotalPages}, nil
}

// rowField returns the field of a row as a string; ok is false when the row has
// no such field or it is not a string.
func rowField(fields map[string]json.RawMessage, name string) (string, bool) {
	raw := fields[name]
	var s string
	// A JSON null would decode to "" without an error: only a string is a value.
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// endOfList reports whether page, which held rows rows, is where the list ends.
// Langfuse's meta.totalPages decides when it is there (it holds when the server
// caps the page size below the one asked for). Without it only an empty page
// proves the end: a short page may be a smaller page size, and calling the
// scan complete on it could present a partial answer as complete.
func endOfList(meta scanMeta, rows, page int) bool {
	if meta.totalPages != nil {
		return page >= *meta.totalPages
	}
	return rows == 0
}

// encodeScanPayload returns out as compact JSON. Its rows were sanitized with
// the page they came from and its summary is static, so nothing is left to
// clean; HTML is not escaped, as in every payload (sanitize.Payload).
func encodeScanPayload(out scanPayload) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
