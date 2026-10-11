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
