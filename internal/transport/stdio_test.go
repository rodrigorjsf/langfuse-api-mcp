package transport

import (
	"errors"
	"io"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's tests when a goroutine outlives them
// (.claude/rules/go.md: every goroutine has an exit path).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// The stdio handshake as a host sees it is tested at the built executable
// (cmd/langfuse-mcp, spec #172 ticket #191). These tests pin the one contract
// of the held reader the executable cannot reach: the SDK requires that Close
// unblocks a Read that waits for input.

func TestAHeldReaderPassesTheSourceThroughOnceReleased(t *testing.T) {
	t.Parallel()
	held := &heldReader{ReadCloser: io.NopCloser(strings.NewReader("probe")), open: make(chan struct{}), closed: make(chan struct{})}
	held.release()
	held.release() // releasing twice is harmless

	got, err := io.ReadAll(held)
	if err != nil || string(got) != "probe" {
		t.Fatalf("read = %q, %v, want %q", got, err, "probe")
	}
}

func TestClosingAHeldReaderEndsAReadThatIsStillWaiting(t *testing.T) {
	t.Parallel()
	held := &heldReader{ReadCloser: io.NopCloser(strings.NewReader("never read")), open: make(chan struct{}), closed: make(chan struct{})}
	ended := make(chan error, 1)
	go func() {
		_, err := held.Read(make([]byte, 8))
		ended <- err
	}()

	if err := held.Close(); err != nil {
		t.Fatalf("close = %v, want nil", err)
	}
	if err := held.Close(); err != nil { // closing twice is harmless
		t.Fatalf("second close = %v, want nil", err)
	}
	if err := <-ended; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("read of a held reader closed while waiting = %v, want %v", err, io.ErrClosedPipe)
	}
}
