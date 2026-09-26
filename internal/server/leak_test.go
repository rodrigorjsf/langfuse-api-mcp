package server_test

import (
	"testing"

	"go.uber.org/goleak"
)

// leakOptions lists the goroutines TestMain tolerates. Only the integration
// build adds any: the keep-alive connections the Langfuse client holds open to
// the live Langfuse, which it cannot close from a test.
var leakOptions []goleak.Option

// TestMain fails the package's tests when a goroutine outlives them
// (.claude/rules/go.md: every goroutine has an exit path).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, leakOptions...)
}
