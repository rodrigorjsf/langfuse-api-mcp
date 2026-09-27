package langfuse_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

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
	client := langfuse.New(langfuse.Options{Host: host, Keys: langfuse.NewKeyPair("pk-lf-test", "sk-lf-test")}) //nolint:gosec // G101: fake keys for a fake host

	_, err = client.Do(context.Background(), http.MethodDelete, "/api/public/traces/trace-1", nil)

	if !errors.Is(err, langfuse.ErrNetwork) {
		t.Fatalf("error = %v, want one wrapping ErrNetwork", err)
	}
	if n := accepted.Load(); n != 1 {
		t.Errorf("the host saw %d connections for a DELETE, want 1: writes are not retried", n)
	}
}

// closingHost accepts connections, reads each request, writes reply (maybe
// nothing) and closes the connection in an orderly way, like a host or proxy
// that drops the connection after the request. It counts the connections.
func closingHost(t *testing.T, reply string) (*url.URL, *atomic.Int32) {
	t.Helper()
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
			// Read the whole request first: closing with unread bytes would
			// send a reset instead of the end-of-stream under test.
			if _, err := http.ReadRequest(bufio.NewReader(conn)); err == nil {
				_, _ = conn.Write([]byte(reply)) // the client sees whatever arrived
			}
			_ = conn.Close() // an orderly close: the client reads end-of-stream
		}
	}()
	host, err := url.Parse("http://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return host, &accepted
}

func noWait(context.Context, time.Duration) error { return nil }

// #91: a reset that lands after the request write reaches http.Client.Do as
// *url.Error{Err: io.EOF}, captured under -race load. A host that closes the
// connection before answering yields the same error deterministically.
func TestAHostThatDropsTheConnectionBeforeAnsweringIsANetworkErrorAndTheReadIsRetried(t *testing.T) {
	t.Parallel()
	host, accepted := closingHost(t, "")
	client := langfuse.New(langfuse.Options{Host: host, Keys: langfuse.NewKeyPair("pk-lf-test", "sk-lf-test"), Wait: noWait}) //nolint:gosec // G101: fake keys for a fake host

	_, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces/trace-1", nil)

	if !errors.Is(err, langfuse.ErrNetwork) {
		t.Fatalf("error = %v, want one wrapping ErrNetwork", err)
	}
	if n := accepted.Load(); n != 3 {
		t.Errorf("the host saw %d connections, want 3: the first attempt and two retries", n)
	}
}

func TestABodyCutShortAfterTheAnswerStartedIsNotANetworkErrorAndIsNotRetried(t *testing.T) {
	t.Parallel()
	host, accepted := closingHost(t, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"data\":")
	client := langfuse.New(langfuse.Options{Host: host, Keys: langfuse.NewKeyPair("pk-lf-test", "sk-lf-test"), Wait: noWait}) //nolint:gosec // G101: fake keys for a fake host

	_, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces/trace-1", nil)

	if err == nil {
		t.Fatal("a truncated body returned no error")
	}
	for _, class := range []error{langfuse.ErrNetwork, langfuse.ErrTimeout, langfuse.ErrCanceled,
		langfuse.ErrUntrustedCertificate, langfuse.ErrCertificateRejected} {
		if errors.Is(err, class) {
			t.Fatalf("error = %v, wraps %v; a truncated body belongs to no failure class", err, class)
		}
	}
	if n := accepted.Load(); n != 1 {
		t.Errorf("the host saw %d connections, want 1: a truncated body is not retried", n)
	}
}
