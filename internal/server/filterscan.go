package server

import (
	"context"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/workflows"
)

// A filter scan (ADR-0010 and ADR-0012 amendments of 2026-10-10) answers a list
// call whose filter the deployment's Langfuse version does not serve, for the
// pairs of the catalog's closed list: the executor reads the list without the
// filter and keeps the rows that match.

// Hints of a filter scan: static text and the page limit, never a value of the
// call or of the payload.
const (
	// pageWithScanHint answers a page chosen together with a scanned filter.
	pageWithScanHint = "call again without page: the server applies this filter itself by reading the list from its " +
		"first page, and says in meta.filterScan whether it read every row"
)

// scanIncompleteHint is the hint of a scan that stopped at its page limit.
func scanIncompleteHint() string {
	return "the server applied the filter itself and stopped at its limit of " + strconv.Itoa(workflows.MaxScanPages) +
		" pages: the rows it did not read could match too, so this answer may be missing rows " +
		"(meta.filterScan says how many rows it read, and of how many exist when Langfuse counts them); narrow the query so that fewer rows exist, " +
		"and call again"
}

// filterScan answers the call of op with a filter scan. A failed page ends it
// with the tool error of that failure.
func (ex executor) filterScan(ctx context.Context, op catalog.Operation, scan catalog.Scan, a *audit) (*mcp.CallToolResult, error) {
	res, err := workflows.FilterScan(ctx, ex.client, ex.redact, workflows.FilterScanInput{
		Path: scan.Request.Path, Query: scan.Request.Query,
		Parameter: scan.Parameter, RowField: scan.RowField, Value: scan.Value,
	})
	a.requests, a.status, a.bytes = res.Requests, res.Status, res.Bytes
	if err != nil {
		f, ok := langfuseErrorFields(err, op, ex.redact, ex.profile)
		if ok {
			a.status = f.HTTPStatus
		} else {
			a.cause = err.Error()
			f = failureFields(err)
		}
		f.OperationID = truncate(op.ID)
		return errorResult(f)
	}
	note := ""
	if !res.Complete {
		note = scanIncompleteHint()
	}
	// The scan reads from page 1 at the page size maximum: neither a lower limit
	// nor paging asks for less.
	remedies := remediesOf(op)
	remedies.Limit, remedies.Paging = 0, false
	return jsonResult(sanitize.WrapNoted(op.ID, res.Payload, ex.maxResultBytes, remedies, note), false)
}
