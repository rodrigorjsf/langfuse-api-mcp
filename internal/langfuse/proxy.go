package langfuse

import (
	"net/http"
	"net/url"

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
