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

// scannedFiltersIntro is the line of its own that opens the list of scanned
// filters in the text of an operation description (#199). Static text.
const scannedFiltersIntro = "Filters the server applies itself on this deployment:\n"

// scannedFilterNote is the note of every scanned filter in an operation
// description (#199): static text compiled into the binary, the same for every
// entry, with the scan's page limit. It holds no catalog text, no text of the
// call and no payload.
func scannedFilterNote() string {
	return "Send it in parameters of " + toolExecuteRead + " as any parameter. This deployment's Langfuse version " +
		"does not serve it, so the server reads the list itself, at most " + strconv.Itoa(workflows.MaxScanPages) +
		" pages, and returns only the rows that match; meta.filterScan says whether it read every row. " +
		"page is not accepted together with it."
}

// scannedFilterClause is the clause that follows the parameter list in the hint
// of a refused call to an operation with a filter the server applies itself
// (#199): static text and the filter's catalog name, never a name or a value of
// the call.
func scannedFilterClause(name string) string {
	return "the server also applies the filter " + name + " itself on this deployment: send it in parameters of " +
		toolExecuteRead + " as any parameter, never together with page"
}

// scanIncompleteHint is the hint of a scan that stopped at its page limit.
func scanIncompleteHint() string {
	return "the server applied the filter itself and stopped at its limit of " + strconv.Itoa(workflows.MaxScanPages) +
		" pages: the rows it did not read could match too, so this answer may be missing rows " +
		"(meta.filterScan says how many rows it read, and of how many exist when Langfuse counts them); narrow the query so that fewer rows exist, " +
		"and call again"
}

// scanReads is op as the filter scan reads it: from page 1 at the page size
// maximum, whatever limit or page the call names, so neither a lower limit nor
// paging asks for less, and the hints of its truncation and of its failures name
// neither (#183, #203).
func scanReads(op catalog.Operation) catalog.Operation {
	return op.WithoutQueryParam("limit").WithoutQueryParam("page").WithoutQueryParam("cursor")
}

// filterScan answers the call of op with a filter scan. A failed page ends it
// with the tool error of that failure.
func (ex executor) filterScan(ctx context.Context, called catalog.Operation, scan catalog.Scan, a *audit) (*mcp.CallToolResult, error) {
	op := scanReads(called)
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
			f = failureFields(op, err)
		}
		f.OperationID = truncate(op.ID)
		return errorResult(f)
	}
	note := ""
	if !res.Complete {
		note = scanIncompleteHint()
	}
	return jsonResult(sanitize.WrapNoted(op.ID, res.Payload, ex.maxResultBytes, remediesOf(op), note), false)
}
