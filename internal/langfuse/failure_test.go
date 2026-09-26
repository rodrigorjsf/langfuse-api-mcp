package langfuse_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

// Seam: the langfuse client (.claude/rules/architecture.md). execute_read only
// sends GETs, so the write side of the retry rule is proved here.

func TestAWriteThatFailsOnTheNetworkIsNeverRetried(t *testing.T) {
	t.Parallel()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() }) // ends the accept loop
	var accepted atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			_ = conn.(*net.TCPConn).SetLinger(0) // close with a reset, not an orderly shutdown
			_ = conn.Close()                     // the client sees the reset either way
		}
	}()
	host, err := url.Parse("http://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := langfuse.New(langfuse.Options{Host: host, PublicKey: "pk-lf-test", SecretKey: "sk-lf-test"}) //nolint:gosec // G101: fake keys for a fake host

	_, err = client.Do(context.Background(), http.MethodDelete, "/api/public/traces/trace-1", nil)

	if !errors.Is(err, langfuse.ErrNetwork) {
		t.Fatalf("error = %v, want one wrapping ErrNetwork", err)
	}
	if n := accepted.Load(); n != 1 {
		t.Errorf("the host saw %d connections for a DELETE, want 1: writes are not retried", n)
	}
}
