package server

import (
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
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

// NarrowHint is the hint of a slow call of op, built from an operation the
// test chooses (#194).
func NarrowHint(op catalog.Operation) string { return narrowHintFor(op) }

// FailureOf returns the code and the hint of the tool error for a Langfuse
// request of op that failed with err, for a failure the client of a test cannot
// read, such as a canceled call.
func FailureOf(op catalog.Operation, err error) (code, hint string) {
	f := failureFields(op, err)
	return f.Code, f.Hint
}
