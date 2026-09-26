//go:build !integration

package server_test

import "go.uber.org/goleak"

// liveLeakOptions is empty in the default build: no live Langfuse client runs
// (see executor_integration_test.go).
func liveLeakOptions() []goleak.Option { return nil }
