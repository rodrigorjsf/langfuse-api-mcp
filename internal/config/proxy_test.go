package config_test

import (
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
)

// Ticket #59 (spec #58): the proxy variables from the environment, validated
// at startup and shown in the startup log without credentials.

func TestLoadShowsTheProxyAsSchemeHostAndPortWithItsSource(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		env  map[string]string
		want config.Proxy
	}{
		"http with a port": {
			env:  map[string]string{"HTTPS_PROXY": "http://proxy.example.com:8080"},
			want: config.Proxy{Endpoint: "http://proxy.example.com:8080", Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"https without a port": {
			env:  map[string]string{"HTTPS_PROXY": "https://proxy.example.com"},
			want: config.Proxy{Endpoint: "https://proxy.example.com:443", Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"http without a port": {
			env:  map[string]string{"HTTPS_PROXY": "http://proxy.example.com"},
			want: config.Proxy{Endpoint: "http://proxy.example.com:80", Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"socks5": {
			env:  map[string]string{"HTTPS_PROXY": "socks5://proxy.example.com:1081"},
			want: config.Proxy{Endpoint: "socks5://proxy.example.com:1081", Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"socks5h without a port": {
			env:  map[string]string{"HTTPS_PROXY": "socks5h://proxy.example.com"},
			want: config.Proxy{Endpoint: "socks5h://proxy.example.com:1080", Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"an IPv6 proxy": {
			env:  map[string]string{"HTTPS_PROXY": "http://[2001:db8::1]:3128"},
			want: config.Proxy{Endpoint: "http://[2001:db8::1]:3128", Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"credentials are left out": {
			env:  map[string]string{"HTTPS_PROXY": "http://proxyuser:hunter2@proxy.example.com:3128"}, //nolint:gosec // G101: a fake proxy credential
			want: config.Proxy{Endpoint: "http://proxy.example.com:3128", Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"the lower-case spelling": {
			env:  map[string]string{"https_proxy": "http://proxy.example.com:8080"},
			want: config.Proxy{Endpoint: "http://proxy.example.com:8080", Variable: "https_proxy", Origin: config.OriginEnvironment},
		},
		"upper case wins over lower case, as in Go": {
			env:  map[string]string{"HTTPS_PROXY": "http://upper.example.com:8080", "https_proxy": "http://lower.example.com:8080"},
			want: config.Proxy{Endpoint: "http://upper.example.com:8080", Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"an empty upper-case value counts as unset": {
			env:  map[string]string{"HTTPS_PROXY": "", "https_proxy": "http://lower.example.com:8080"},
			want: config.Proxy{Endpoint: "http://lower.example.com:8080", Variable: "https_proxy", Origin: config.OriginEnvironment},
		},
		// Langfuse hosts are https except loopback, which Go never proxies:
		// HTTP_PROXY is validated but never the proxy in use.
		"HTTP_PROXY alone is no proxy in use": {
			env:  map[string]string{"HTTP_PROXY": "http://proxy.example.com:8080"},
			want: config.Proxy{},
		},
		"nothing set": {env: map[string]string{}, want: config.Proxy{}},
		"empty values count as unset": {
			env:  map[string]string{"HTTPS_PROXY": "", "https_proxy": "", "HTTP_PROXY": "", "http_proxy": "", "NO_PROXY": ""},
			want: config.Proxy{},
		},
		"NO_PROXY set": {
			env:  map[string]string{"HTTPS_PROXY": "http://proxy.example.com:8080", "NO_PROXY": "langfuse.internal"},
			want: config.Proxy{Endpoint: "http://proxy.example.com:8080", Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment, NoProxy: true},
		},
		"no_proxy set, without a proxy": {
			env:  map[string]string{"no_proxy": "langfuse.internal"},
			want: config.Proxy{NoProxy: true},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := mustLoad(t, tc.env, config.File{})

			if cfg.Proxy != tc.want {
				t.Fatalf("Proxy = %+v, want %+v", cfg.Proxy, tc.want)
			}
		})
	}
}

// Dangerous parameters (MCP05:2025): a proxy value that is malformed, has no
// host, or uses another scheme stops startup naming the variable and its
// source, never the value, which may carry credentials.
func TestLoadRefusesAnInvalidProxyValueWithoutEchoingIt(t *testing.T) {
	t.Parallel()
	const credential = "hunter2"
	values := map[string]string{
		"malformed":                 "http://proxyuser:" + credential + "@proxy.example.com:port",
		"no host":                   "http://proxyuser:" + credential + "@",
		"no scheme":                 "proxyuser:" + credential + "@proxy.example.com:8080",
		"scheme htps":               "htps://proxyuser:" + credential + "@proxy.example.com:8080",
		"scheme file":               "file://proxyuser:" + credential + "@proxy.example.com/etc/passwd",
		"scheme javascript":         "javascript://proxyuser:" + credential + "@proxy.example.com/%0Aalert(1)",
		"scheme ftp":                "ftp://proxyuser:" + credential + "@proxy.example.com:21",
		"a newline":                 "http://proxyuser:" + credential + "@proxy.example.com:8080\n",
		"a NUL":                     "http://proxyuser:" + credential + "@proxy.example.com\x00:8080",
		"an escape character":       "http://proxyuser:" + credential + "@proxy.example.com:8080/\x1b[2J",
		"embedded whitespace":       "http://proxyuser:" + credential + "@proxy.example.com:8080/ x",
		"a tab":                     "http://proxyuser:" + credential + "@proxy.example.com:8080\t",
		"leading whitespace":        " http://proxyuser:" + credential + "@proxy.example.com:8080",
		"a zero-width space":        "http://proxyuser:" + credential + "@proxy.example.com\u200b:8080",
		"a bidi override":           "http://proxyuser:" + credential + "@proxy.example.com:8080/\u202e",
		"a non-breaking space":      "http://proxyuser:" + credential + "@proxy.example.com:8080/\u00a0",
		"a port but no host":        "http://" + credential + ":" + credential + "@:8080",
		"scheme javascript, opaque": "javascript:alert('" + credential + "')",
	}
	for _, variable := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		for name, value := range values {
			t.Run(variable+"/"+name, func(t *testing.T) {
				t.Parallel()

				_, err := load(map[string]string{variable: value}, config.File{})

				if err == nil {
					t.Fatalf("Load accepted %s=%q, want a startup error", variable, value)
				}
				msg := err.Error()
				if !strings.Contains(msg, variable) || !strings.Contains(msg, "environment") {
					t.Errorf("error %q does not name %s and its source (environment)", msg, variable)
				}
				if strings.Contains(msg, value) || strings.Contains(msg, credential) {
					t.Errorf("error %q echoes the value or its credentials", msg)
				}
			})
		}
	}
}
