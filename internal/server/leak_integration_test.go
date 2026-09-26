//go:build integration

package server_test

import (
	"os"

	"go.uber.org/goleak"
)

// liveLeakOptions lets TestMain tolerate the idle keep-alive connections
// (HTTP/1.1 and HTTP/2) the live Langfuse client holds open after the tests,
// as it does in production; the client has no way to close them. Only when a
// live Langfuse is configured: without one, the integration build checks for
// leaks like the default build.
// A client that can close its idle connections would remove this: see #41.
func liveLeakOptions() []goleak.Option {
	if os.Getenv(envTestBaseURL) == "" {
		return nil
	}
	return []goleak.Option{
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
		// Langfuse Cloud answers over HTTP/2, whose idle connection keeps a read loop.
		goleak.IgnoreAnyFunction("net/http/internal/http2.(*ClientConn).readLoop"),
	}
}
