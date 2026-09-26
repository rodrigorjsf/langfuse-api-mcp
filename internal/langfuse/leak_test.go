package langfuse_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's tests when a goroutine outlives them
// (.claude/rules/go.md: every goroutine has an exit path).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
