package server

import "time"

// WithClock replaces the clock that dates confirmation states, so a test can
// make one expire (spec #109: time is a true external).
func WithClock(now func() time.Time) Option { return func(o *options) { o.now = now } }
