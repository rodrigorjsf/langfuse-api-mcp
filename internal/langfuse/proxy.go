package langfuse

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"golang.org/x/net/http/httpproxy"
)

// ProxyFromSettings returns a proxy function for Options.Proxy from resolved
// proxy settings (HTTPS_PROXY, HTTP_PROXY and NO_PROXY values; "" means
// unset), with the semantics of http.ProxyFromEnvironment: the same httpproxy
// code Go vendors behind it. It reads nothing from the process environment:
// Go caches that environment the first time anything asks, and the MCP SDK
// asks during package initialization, before main could apply a config-file
// proxy (ADR-0006, #61).
func ProxyFromSettings(httpsProxy, httpProxy, noProxy string) func(*http.Request) (*url.URL, error) {
	proxy := (&httpproxy.Config{HTTPSProxy: httpsProxy, HTTPProxy: httpProxy, NoProxy: noProxy}).ProxyFunc()
	return func(req *http.Request) (*url.URL, error) { return proxy(req.URL) }
}

// proxyConnectError: the proxy answered the CONNECT request for the tunnel to
// Langfuse with a status other than 200. It carries the status code only: the
// proxy wrote the reason phrase and body, which are untrusted and never kept.
type proxyConnectError struct{ status int }

func (e proxyConnectError) Error() string {
	return "the proxy answered the CONNECT request with HTTP status " + strconv.Itoa(e.status)
}

// checkProxyConnect is the transport's OnProxyConnectResponse. It runs before
// Go's own non-200 check, whose error is untyped and quotes the proxy's reason
// phrase, and returns proxyConnectError instead, which classify can match by
// type (#32).
func checkProxyConnect(_ context.Context, _ *url.URL, _ *http.Request, res *http.Response) error {
	if res.StatusCode != http.StatusOK {
		return proxyConnectError{status: res.StatusCode}
	}
	return nil
}
