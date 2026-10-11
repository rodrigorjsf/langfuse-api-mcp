package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// LegacyObservationsOperationID is the catalog operation the trace tree reads
// on a deployment without the v4 read family: the legacy observations list
// (GET /api/public/observations, served with a traceId filter from 3.0.0).
const LegacyObservationsOperationID = "legacy_observationsV1_getMany"

// legacyObservationsPath is the path of LegacyObservationsOperationID below
// the host.
const legacyObservationsPath = "/api/public/observations"

// Legacy trace tree paging (ADR-0002 amendment of 2026-10-10, #187): at most
// MaxLegacyTracePages pages of LegacyTracePageSize observations, the page size
// cap of the legacy list, whatever the call asks (no argument reaches either
// number). The trace tree of Observations v2 keeps its 5 pages of 1000.
const (
	MaxLegacyTracePages = 10
	LegacyTracePageSize = 100
)

// GetLegacyTraceTree reads the observations of the trace traceID through the
// legacy observations list, page by page from page 1, for at most
// MaxLegacyTracePages pages, and orders them like GetTraceTree: the envelope
// and the ordering are the same, and a row holds the fields that deployment's
// Langfuse returns, not translated.
//
// The legacy list has no field selection and always returns input, output and
// metadata, so they are removed from every row here unless include asks for
// them. Each page is cleaned (sanitize.Payload) before its rows are read, so
// the keys that are removed are the keys the agent would read: a key written
// with a hidden character is cleaned to its name first and removed with it.
// traceID is sent only as a query parameter. A page that fails, or that is not
// a page of observation objects, ends the call with its error wrapped and the
// TraceTree's Requests, Status and Bytes set: no partial tree is returned.
func GetLegacyTraceTree(ctx context.Context, client *langfuse.Client, r sanitize.Redactor, traceID string, include Include) (TraceTree, error) {
	var tree TraceTree
	var rows []observation
	pagesRead := 0
	for page := 1; page <= MaxLegacyTracePages; page++ {
		query := url.Values{
			"traceId": {traceID},
			"limit":   {strconv.Itoa(LegacyTracePageSize)},
			"page":    {strconv.Itoa(page)},
		}
		body, err := tree.fetchPage(ctx, client, LegacyObservationsOperationID, legacyObservationsPath, page, query)
		if err != nil {
			return tree, err
		}
		clean, err := sanitize.Payload(body, r)
		if err != nil {
			return tree, fmt.Errorf("%s page %d: %w", LegacyObservationsOperationID, page, err)
		}
		got, totalPages, err := decodeLegacyPage(clean)
		if err != nil {
			return tree, fmt.Errorf("%s page %d: %w", LegacyObservationsOperationID, page, err)
		}
		for _, o := range got {
			include.dropUnrequested(o.fields)
		}
		rows = append(rows, got...)
		pagesRead = page
		tree.More = !endOfList(totalPages, len(got), page)
		if !tree.More {
			break
		}
	}
	if tree.More {
		tree.NextPage = pagesRead + 1
	}
	return tree, tree.finish(rows, traceMeta{Truncated: tree.More}, r)
}

// dropUnrequested removes from a row of the legacy list the field groups that
// include did not ask for: input and output, and metadata. It matches the
// top-level keys of the row by name, so what a row holds inside another field
// cannot keep a key.
func (in Include) dropUnrequested(fields map[string]json.RawMessage) {
	if !in.IO {
		delete(fields, "input")
		delete(fields, "output")
	}
	if !in.Metadata {
		delete(fields, "metadata")
	}
}

// decodeLegacyPage decodes one cleaned page of the legacy observations list:
// its rows, which must be objects, and meta.totalPages, nil when Langfuse gave
// none.
func decodeLegacyPage(body json.RawMessage) ([]observation, *int, error) {
	var page struct {
		Data []json.RawMessage `json:"data"`
		Meta struct {
			TotalPages *int `json:"totalPages"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &page); err != nil || page.Data == nil {
		return nil, nil, errUnexpectedPage
	}
	rows, err := decodeObservations(page.Data)
	return rows, page.Meta.TotalPages, err
}
