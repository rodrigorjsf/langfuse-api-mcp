package langfuse_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.uber.org/goleak"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

// Issue #41: the client releases its idle keep-alive connections on demand,
// so tests and shutdown leave no transport goroutine behind. Seam: the client
// against an httptest.Server, over HTTP/1.1 and over HTTP/2 (Langfuse Cloud
// answers over HTTP/2).
//
// Not parallel: goleak compares against the goroutines alive when the test
// starts, which other tests running at the same time would change. Top-level
// parallel tests only start once the sequential ones are done.
//
//nolint:paralleltest // see above
func TestNoTransportGoroutineSurvivesCloseIdleConnections(t *testing.T) {
	for _, tc := range []struct {
		name  string
		http2 bool
	}{
		{name: "HTTP/1.1", http2: false},
		{name: "HTTP/2", http2: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var protoMajor atomic.Int32
			fake := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				protoMajor.Store(int32(r.ProtoMajor)) //nolint:gosec // G115: 1 or 2
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{}`) // a failed write shows up as a client error in the test
			}))
			fake.EnableHTTP2 = tc.http2
			fake.StartTLS()
			t.Cleanup(fake.Close)
			// Snapshot after the fake started: its listener is not the client's.
			before := goleak.IgnoreCurrent()

			roots := x509.NewCertPool()
			roots.AddCert(fake.Certificate())
			opts := limitsOptions(t, fake.URL)
			opts.TLS = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
			client := langfuse.New(opts)
			if _, err := client.Do(context.Background(), http.MethodGet, "/api/public/health", nil); err != nil {
				t.Fatalf("request: %v", err)
			}
			if want := map[bool]int32{false: 1, true: 2}[tc.http2]; protoMajor.Load() != want {
				t.Fatalf("the request used HTTP/%d, want HTTP/%d", protoMajor.Load(), want)
			}

			client.CloseIdleConnections()

			goleak.VerifyNone(t, before)
		})
	}
}
