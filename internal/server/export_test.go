package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// WithClock replaces the clock that dates confirmation states, so a test can
// make one expire (spec #109: time is a true external).
func WithClock(now func() time.Time) Option { return func(o *options) { o.now = now } }

// LimitHint, NotServedHint and TooLargeHint are the hints that name a way of
// paging, built from an operation, so that a test can give them an operation
// the catalog does not hold (one that pages by both page and cursor).
func LimitHint(op catalog.Operation) string     { return limitHint(op) }
func NotServedHint(op catalog.Operation) string { return notServedHintFor(op) }
func TooLargeHint(op catalog.Operation) string  { return tooLargeHintFor(op) }

// TimeoutHint is the hint of a read of op that timed out, built from an operation
// the test chooses (#194).
func TimeoutHint(op catalog.Operation) string { return slowHintFor(op, false) }

// FailureOf returns the code and the hint of the tool error for a Langfuse
// request of op that failed with err, for a failure the client of a test cannot
// read, such as a canceled call: the executor's own failureOf, which the
// generic executor, the filter scan and the trace tree share.
func FailureOf(op catalog.Operation, err error) (code, hint string) {
	f, _ := executor{}.failureOf(op, err, &audit{})
	return f.Code, f.Hint
}

// TruncationHint is the hint of a page of op that lost rows to the smallest
// result size cap: the reference the hints of a response too large and of a slow
// call must agree with (#203). It is built from an operation the test chooses,
// so a walk can take it from every operation of a deployment profile.
func TruncationHint(op catalog.Operation) string {
	const smallestResultCap = 16384
	rows := make([]map[string]string, 400)
	for i := range rows {
		rows[i] = map[string]string{"id": strconv.Itoa(i), "input": strings.Repeat("a", 100)}
	}
	payload, err := json.Marshal(map[string]any{"data": rows, "meta": map[string]any{}})
	if err != nil {
		panic(err) // rows of strings always marshal
	}
	return sanitize.Wrap(op.ID, payload, smallestResultCap, remediesOf(op)).Hint
}

// TraceTreeFailureOf returns the code and the hint of the tool error for a read
// of the trace tree on a deployment with profile, over cat, that failed with
// err, for a failure the client of a test cannot read, such as a canceled call.
func TraceTreeFailureOf(cat catalog.Catalog, profile langfuse.DeploymentProfile, err error) (code, hint string) {
	ex := executor{catalog: cat, profile: profile}
	src, served := ex.selectTraceTreeSource()
	if !served {
		panic("TraceTreeFailureOf: the profile reads no trace tree")
	}
	f := ex.traceTreeFailure(src, err, &audit{})
	return f.Code, f.Hint
}

// ScanOperation is op as the filter scan reads it, which the hints of a failed
// scan are built from (#203).
func ScanOperation(op catalog.Operation) catalog.Operation { return scanReads(op) }

// WriteFailureOf returns the code and the hint of the tool error for a write of
// op that failed with err before Langfuse answered, for a failure the client of
// a test cannot read, such as a canceled call: the executor's own
// requestFailure, the translation execute_write runs.
func WriteFailureOf(op catalog.Operation, err error) (code, hint string) {
	f := executor{}.requestFailure(op, catalog.Request{}, err, &audit{})
	return f.Code, f.Hint
}
