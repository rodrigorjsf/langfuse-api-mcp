package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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
// startup log only: the executable builds the Langfuse client's proxy
// function from ProxySettings. It never holds the proxy's credentials.
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

// The proxy variables Go's http.ProxyFromEnvironment reads, each spelling
// pair in Go's order: upper case, then lower case.
var (
	httpsProxyPair = [2]string{EnvHTTPSProxy, EnvHTTPSProxyLower}
	httpProxyPair  = [2]string{EnvHTTPProxy, EnvHTTPProxyLower}
	noProxyPair    = [2]string{EnvNoProxy, EnvNoProxyLower}
)

// ProxySettings are the proxy variables in effect, one value per variable,
// resolved as Go's http.ProxyFromEnvironment resolves them (upper case over
// lower case) over the environment and the config file together: the
// environment wins for a variable it sets in either spelling (ADR-0006, #45).
// The executable builds the Langfuse client's proxy function from them, so a
// config-file proxy never has to reach the process environment. The proxy
// URLs may carry credentials, so they are Secrets; "" means unset.
type ProxySettings struct {
	HTTPS   Secret // HTTPS_PROXY, else https_proxy
	HTTP    Secret // HTTP_PROXY, else http_proxy
	NoProxy string // NO_PROXY, else no_proxy; passed through as Go reads it
}

// loadProxy validates every proxy variable set in env or in the config file
// (fromFile, read from path at lines) and returns the proxy in use and the
// settings in effect. A variable the environment sets in either spelling is
// read from the environment only, so the environment wins over the file while
// Go's upper-over-lower order applies within each source (ADR-0006, #45). The
// proxy in use is HTTPS_PROXY, else https_proxy; HTTP_PROXY is validated,
// since Go would read it, but never in use: Langfuse hosts are https except
// loopback, and Go never proxies loopback. An empty value counts as unset, as
// in Go.
func loadProxy(env, fromFile map[string]string, lines map[string]int, path string) (Proxy, ProxySettings, error) {
	for _, pair := range [...][2]string{httpsProxyPair, httpProxyPair} { // NO_PROXY is passed through as Go reads it
		for _, name := range pair {
			// Validate both sources, even a file value the environment overrides.
			if err := validateProxy(env[name], fmt.Sprintf("%s (%s)", name, OriginEnvironment)); err != nil {
				return Proxy{}, ProxySettings{}, err
			}
			if err := validateProxy(fromFile[name], fmt.Sprintf("config file %s line %d: %s", path, lines[name], name)); err != nil {
				return Proxy{}, ProxySettings{}, err
			}
		}
	}

	// resolve returns a variable's value in effect, the spelling it was read
	// under and where: from env when env sets either spelling, else from the
	// file; upper case first within the source.
	resolve := func(pair [2]string) (Setting, string) {
		source, origin := fromFile, OriginConfigFile
		if env[pair[0]] != "" || env[pair[1]] != "" {
			source, origin = env, OriginEnvironment
		}
		for _, name := range pair {
			if v := source[name]; v != "" {
				return Setting{Value: v, Origin: origin}, name
			}
		}
		return Setting{}, ""
	}
	https, httpsName := resolve(httpsProxyPair)
	plainHTTP, _ := resolve(httpProxyPair)
	noProxy, _ := resolve(noProxyPair)

	p := Proxy{NoProxy: noProxy.Value != ""}
	if https.Value != "" {
		p.Endpoint, _ = proxyEndpoint(https.Value) // validated above
		p.Variable, p.Origin = httpsName, https.Origin
	}
	return p, ProxySettings{HTTPS: Secret{https.Value}, HTTP: Secret{plainHTTP.Value}, NoProxy: noProxy.Value}, nil
}

// validateProxy returns an error starting with label (the variable and where
// it was read) when value is set and not a valid proxy URL; never the value:
// it may carry credentials.
func validateProxy(value, label string) error {
	if value == "" {
		return nil
	}
	if _, err := proxyEndpoint(value); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

// proxyEndpoint returns the proxy URL value as scheme://host:port, or an
// error that never quotes value.
func proxyEndpoint(value string) (string, error) {
	const example = "such as http://proxy.internal:3128"
	if !printable(value) {
		return "", errors.New("the value holds whitespace, control or invisible characters; want a proxy URL " + example)
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("want a proxy URL with a host, " + example)
	}
	// url.Parse percent-decodes the host, so an encoded host could smuggle an
	// invisible character or invalid UTF-8 into the startup log. A host that
	// appears verbatim in the (printable) value was not decoded.
	if !strings.Contains(value, "//"+u.Host) && !strings.Contains(value, "@"+u.Host) {
		return "", errors.New("the proxy host is percent-encoded; want a plain proxy URL " + example)
	}
	port, ok := defaultProxyPorts[u.Scheme]
	if !ok {
		return "", errors.New("want a proxy URL with scheme http, https, socks5 or socks5h, " + example)
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "", errors.New("want a proxy port from 1 to 65535, " + example)
		}
		port = p
	}
	return u.Scheme + "://" + net.JoinHostPort(u.Hostname(), port), nil
}

// printable reports whether s is valid UTF-8 holding no whitespace, control or invisible
// (format, such as zero-width or bidi) characters.
func printable(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
