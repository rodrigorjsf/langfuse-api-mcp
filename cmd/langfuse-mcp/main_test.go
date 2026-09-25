// Package main, not main_test: TestMain must call the unexported main() in the child process.
package main

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// runMainEnv marks a child process started by this test binary: instead of
// running the tests, the child runs the real main(), so the test observes the
// executable's behavior at the process seam (exit code), not internals.
const runMainEnv = "LANGFUSE_MCP_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		os.Exit(0) // main returned normally: that is a clean exit
	}
	os.Exit(m.Run())
}

func TestExecutableStartsAndExitsCleanly(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^$") //nolint:gosec // G204: exe is this test binary, not external input

	cmd.Env = append(os.Environ(), runMainEnv+"=1")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\noutput:\n%s", err, out)
	}
}
