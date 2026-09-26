package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"unicode"
)

// Names of the proxy variables, in the order Go's http.ProxyFromEnvironment
// reads them: upper case first, then lower case (ADR-0006).
const (
	EnvHTTPSProxy      = "HTTPS_PROXY"
	EnvHTTPSProxyLower = "https_proxy"
	EnvHTTPProxy       = "HTTP_PROXY"
	EnvHTTPProxyLower  = "http_proxy"
	EnvNoProxy         = "NO_PROXY"
	EnvNoProxyLower    = "no_proxy"
)

// Proxy is the proxy the server's Langfuse requests go through, for the
// startup log only: the executable hands the Langfuse client
// http.ProxyFromEnvironment, which reads the same variables. It never holds
// the proxy's credentials.
type Proxy struct {
	// Endpoint is the proxy as scheme://host:port, the scheme's default port
	// filled in; "" when no proxy is set.
	Endpoint string
	// Variable is the variable Endpoint was read from: HTTPS_PROXY or https_proxy.
	Variable string
	// Origin is where Variable was read.
	Origin Origin
	// NoProxy is true when NO_PROXY or no_proxy is set.
	NoProxy bool
}

// defaultProxyPorts are the ports Go's transport uses for a proxy URL
// without one.
var defaultProxyPorts = map[string]string{"http": "80", "https": "443", "socks5": "1080", "socks5h": "1080"}

// loadProxy validates every proxy variable set in env and returns the proxy
// in use. Langfuse hosts are https except loopback, and Go never proxies
// loopback, so the proxy in use is HTTPS_PROXY, else https_proxy; the
// HTTP_PROXY spellings are validated all the same, since Go would read them.
// An empty value counts as unset, as in Go.
func loadProxy(env map[string]string) (Proxy, error) {
	var p Proxy
	for _, name := range []string{EnvHTTPSProxy, EnvHTTPSProxyLower, EnvHTTPProxy, EnvHTTPProxyLower} {
		value := env[name]
		if value == "" {
			continue
		}
		endpoint, err := proxyEndpoint(value)
		if err != nil {
			// Never the value: a proxy URL may carry credentials.
			return Proxy{}, fmt.Errorf("%s (%s): %w", name, OriginEnvironment, err)
		}
		if p.Endpoint == "" && (name == EnvHTTPSProxy || name == EnvHTTPSProxyLower) {
			p.Endpoint, p.Variable, p.Origin = endpoint, name, OriginEnvironment
		}
	}
	p.NoProxy = env[EnvNoProxy] != "" || env[EnvNoProxyLower] != ""
	return p, nil
}

// proxyEndpoint returns the proxy URL value as scheme://host:port, or an
// error that never quotes value.
func proxyEndpoint(value string) (string, error) {
	for _, r := range value {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return "", errors.New("the value holds whitespace, control or invisible characters; want a proxy URL " +
				"such as http://proxy.internal:3128")
		}
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("want a proxy URL with a host, such as http://proxy.internal:3128")
	}
	port, ok := defaultProxyPorts[u.Scheme]
	if !ok {
		return "", errors.New("want a proxy URL with scheme http, https, socks5 or socks5h, such as http://proxy.internal:3128")
	}
	if p := u.Port(); p != "" {
		port = p
	}
	return u.Scheme + "://" + net.JoinHostPort(u.Hostname(), port), nil
}
