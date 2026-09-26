//go:build integration

package server_test

import "go.uber.org/goleak"

// liveLeakOptions is empty in the integration build too: live tests close the
// Langfuse client's idle connections in their cleanup (liveSession), so a live
// run checks for leaks exactly like the default build (#41).
func liveLeakOptions() []goleak.Option { return nil }
