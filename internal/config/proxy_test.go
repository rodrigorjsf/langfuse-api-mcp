package config_test

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"unicode"

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
		"malformed":                            "http://proxyuser:" + credential + "@proxy.example.com:port",
		"no host":                              "http://proxyuser:" + credential + "@",
		"no scheme":                            "proxyuser:" + credential + "@proxy.example.com:8080",
		"scheme htps":                          "htps://proxyuser:" + credential + "@proxy.example.com:8080",
		"scheme file":                          "file://proxyuser:" + credential + "@proxy.example.com/etc/passwd",
		"scheme javascript":                    "javascript://proxyuser:" + credential + "@proxy.example.com/%0Aalert(1)",
		"scheme ftp":                           "ftp://proxyuser:" + credential + "@proxy.example.com:21",
		"a newline":                            "http://proxyuser:" + credential + "@proxy.example.com:8080\n",
		"a NUL":                                "http://proxyuser:" + credential + "@proxy.example.com\x00:8080",
		"an escape character":                  "http://proxyuser:" + credential + "@proxy.example.com:8080/\x1b[2J",
		"embedded whitespace":                  "http://proxyuser:" + credential + "@proxy.example.com:8080/ x",
		"a tab":                                "http://proxyuser:" + credential + "@proxy.example.com:8080\t",
		"leading whitespace":                   " http://proxyuser:" + credential + "@proxy.example.com:8080",
		"a zero-width space":                   "http://proxyuser:" + credential + "@proxy.example.com\u200b:8080",
		"a bidi override":                      "http://proxyuser:" + credential + "@proxy.example.com:8080/\u202e",
		"a non-breaking space":                 "http://proxyuser:" + credential + "@proxy.example.com:8080/\u00a0",
		"an encoded bidi override in the host": "http://proxyuser:" + credential + "@proxy%E2%80%AE.example.com:8080",
		"an encoded zero-width space in the host": "http://proxyuser:" + credential + "@proxy%E2%80%8B.example.com:8080",
		"a port out of range":                     "http://proxyuser:" + credential + "@proxy.example.com:99999",
		"port zero":                               "http://proxyuser:" + credential + "@proxy.example.com:0",
		"a port but no host":                      "http://" + credential + ":" + credential + "@:8080",
		"scheme javascript, opaque":               "javascript:alert('" + credential + "')",
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

// FuzzLoadProxy checks that Load never panics on a proxy value, never echoes
// a refused value, and never shows userinfo or an invisible character in the
// endpoint it accepts (go.md: fuzz every config parser).
func FuzzLoadProxy(f *testing.F) {
	for _, seed := range []string{
		"", "http://proxy.example.com:8080", "socks5h://p", "https://u:p@p:443", "htps://p", "p:8080",
		"http://p%E2%80%AE:1", "http://[::1]:3128", "http://p:99999", "\x00", "http://u:p@",
	} {
		f.Add(seed)
	}
	// Every refusal message, one per reason: a value that is a substring of
	// one of them (e.g. "://p") is not an echo.
	var refusals strings.Builder
	for _, invalid := range []string{"a b", "http://", "http://p%E2%80%AE:1", "htps://p", "http://p:99999"} {
		if _, err := load(map[string]string{"HTTPS_PROXY": invalid}, config.File{}); err != nil {
			refusals.WriteString(err.Error() + "\n")
		}
	}
	f.Fuzz(func(t *testing.T, value string) {
		cfg, err := load(map[string]string{"HTTPS_PROXY": value}, config.File{})
		if err != nil {
			if strings.Contains(err.Error(), value) && !strings.Contains(refusals.String(), value) {
				t.Fatalf("Load(%q) error %q echoes the value", value, err)
			}
			return
		}
		if value == "" {
			if cfg.Proxy.Endpoint != "" {
				t.Fatalf("Load(\"\") endpoint = %q, want none", cfg.Proxy.Endpoint)
			}
			return
		}
		u, parseErr := url.Parse(cfg.Proxy.Endpoint)
		if parseErr != nil || u.User != nil || u.Port() == "" {
			t.Fatalf("Load(%q) endpoint = %q, want scheme://host:port without userinfo", value, cfg.Proxy.Endpoint)
		}
		for _, r := range cfg.Proxy.Endpoint {
			if !unicode.IsPrint(r) || unicode.IsSpace(r) {
				t.Fatalf("Load(%q) endpoint %q holds %U", value, cfg.Proxy.Endpoint, r)
			}
		}
	})
}

// Ticket #45 (spec #58): the config file's proxy keys, in either spelling,
// reach the Langfuse client. Load resolves each proxy variable as Go reads
// it, upper case over lower case, from the environment when it sets the
// variable in either spelling, else from the config file (ADR-0006).

func TestLoadKnowsTheLowerCaseProxyKeysButNoOtherLowerCaseKey(t *testing.T) {
	t.Parallel()
	content := "https_proxy=http://proxy.example.com:8080\n" +
		"http_proxy=http://proxy.example.com:8080\n" +
		"no_proxy=langfuse.internal\n" +
		"langfuse_ca_cert=/etc/corp/root.pem\n"

	_, ignored, err := config.Load(connectionEnv(nil), configFile(content))

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Only the proxy variables are read in either spelling, as Go reads them.
	want := []config.IgnoredKey{{Line: 4, Name: "langfuse_ca_cert"}}
	if !slices.Equal(ignored, want) {
		t.Fatalf("ignored keys = %+v, want %+v", ignored, want)
	}
}

// resolved is the proxy settings Load hands the executable, in plain strings.
type resolved struct{ HTTPS, HTTP, NoProxy string }

func settingsOf(cfg config.Config) resolved {
	s := cfg.ProxySettings
	return resolved{HTTPS: s.HTTPS.Reveal(), HTTP: s.HTTP.Reveal(), NoProxy: s.NoProxy}
}

func TestLoadResolvesEachProxyVariableFromTheEnvironmentElseTheConfigFile(t *testing.T) {
	t.Parallel()
	const (
		fileProxy = "http://file.example.com:8080"
		envProxy  = "http://env.example.com:8080"
	)
	fromFileProxy := func(variable string) config.Proxy {
		return config.Proxy{Endpoint: fileProxy, Variable: variable, Origin: config.OriginConfigFile}
	}
	tests := map[string]struct {
		env       map[string]string
		file      string
		want      resolved
		wantProxy config.Proxy
	}{
		"HTTPS_PROXY only in the file": {
			file: "HTTPS_PROXY=" + fileProxy + "\n",
			want: resolved{HTTPS: fileProxy}, wantProxy: fromFileProxy("HTTPS_PROXY"),
		},
		"lower-case https_proxy only in the file": {
			file: "https_proxy=" + fileProxy + "\n",
			want: resolved{HTTPS: fileProxy}, wantProxy: fromFileProxy("https_proxy"),
		},
		"both spellings in the file: the upper case wins": {
			file:      "https_proxy=http://lower.example.com:8080\nHTTPS_PROXY=" + fileProxy + "\n",
			want:      resolved{HTTPS: fileProxy},
			wantProxy: fromFileProxy("HTTPS_PROXY"),
		},
		"both spellings in the environment: the upper case wins": {
			env:       map[string]string{"HTTPS_PROXY": envProxy, "https_proxy": "http://lower.example.com:8080"},
			want:      resolved{HTTPS: envProxy},
			wantProxy: config.Proxy{Endpoint: envProxy, Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"the environment's HTTPS_PROXY wins over the file's https_proxy": {
			env:       map[string]string{"HTTPS_PROXY": envProxy},
			file:      "https_proxy=" + fileProxy + "\n",
			want:      resolved{HTTPS: envProxy},
			wantProxy: config.Proxy{Endpoint: envProxy, Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
		"the environment's https_proxy wins over the file's HTTPS_PROXY": {
			env:       map[string]string{"https_proxy": envProxy},
			file:      "HTTPS_PROXY=" + fileProxy + "\n",
			want:      resolved{HTTPS: envProxy},
			wantProxy: config.Proxy{Endpoint: envProxy, Variable: "https_proxy", Origin: config.OriginEnvironment},
		},
		"an empty environment value counts as unset": {
			env:  map[string]string{"HTTPS_PROXY": "", "https_proxy": ""},
			file: "HTTPS_PROXY=" + fileProxy + "\n",
			want: resolved{HTTPS: fileProxy}, wantProxy: fromFileProxy("HTTPS_PROXY"),
		},
		"HTTP_PROXY and no_proxy from the file": {
			file:      "HTTP_PROXY=" + fileProxy + "\nno_proxy=langfuse.internal\n",
			want:      resolved{HTTP: fileProxy, NoProxy: "langfuse.internal"},
			wantProxy: config.Proxy{NoProxy: true},
		},
		"the environment's NO_PROXY wins over the file's no_proxy, not over the file's proxy": {
			env:       map[string]string{"NO_PROXY": "langfuse.internal"},
			file:      "HTTPS_PROXY=" + fileProxy + "\nno_proxy=other.internal\n",
			want:      resolved{HTTPS: fileProxy, NoProxy: "langfuse.internal"},
			wantProxy: config.Proxy{Endpoint: fileProxy, Variable: "HTTPS_PROXY", Origin: config.OriginConfigFile, NoProxy: true},
		},
		"empty file values set nothing": {
			file: "HTTPS_PROXY=\nhttps_proxy=\nNO_PROXY=\n",
		},
		"nothing in the file": {
			env:       map[string]string{"HTTPS_PROXY": envProxy},
			want:      resolved{HTTPS: envProxy},
			wantProxy: config.Proxy{Endpoint: envProxy, Variable: "HTTPS_PROXY", Origin: config.OriginEnvironment},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := mustLoad(t, tc.env, configFile(tc.file))

			if got := settingsOf(cfg); got != tc.want {
				t.Errorf("proxy settings = %+v, want %+v", got, tc.want)
			}
			if cfg.Proxy != tc.wantProxy {
				t.Errorf("Proxy = %+v, want %+v", cfg.Proxy, tc.wantProxy)
			}
		})
	}
}

// The resolved proxy URLs carry proxy credentials: they never print.
func TestProxySettingsNeverPrintTheProxyCredentials(t *testing.T) {
	t.Parallel()
	const password = "hunter2"
	cfg := mustLoad(t, map[string]string{"HTTP_PROXY": "http://proxyuser:" + password + "@env.example.com:8080"},
		configFile("HTTPS_PROXY=http://proxyuser:"+password+"@proxy.example.com:8080\n"))

	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for name, v := range map[string]any{"ProxySettings": cfg.ProxySettings, "Config": cfg} {
			if got := fmt.Sprintf(verb, v); strings.Contains(got, password) {
				t.Errorf("Sprintf(%q, %s) = %q, holds the credential", verb, name, got)
			}
		}
	}
}

// Dangerous parameters (MCP05:2025): an invalid proxy value in the config file
// stops startup naming the file, the line and the variable, never the value,
// even when the environment sets that variable.
func TestLoadRefusesAnInvalidConfigFileProxyValueWithoutEchoingIt(t *testing.T) {
	t.Parallel()
	const credential = "hunter2"
	const value = "htps://proxyuser:" + credential + "@proxy.example.com:8080"
	for _, variable := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		for name, env := range map[string]map[string]string{
			"environment unset":             nil,
			"environment sets the variable": {variable: "http://env.example.com:8080"},
		} {
			t.Run(variable+"/"+name, func(t *testing.T) {
				t.Parallel()
				file := configFile("# corporate proxy\n" + variable + "=" + value + "\n")

				_, err := load(env, file)

				if err == nil {
					t.Fatalf("Load accepted %s=%q in the config file, want a startup error", variable, value)
				}
				msg := err.Error()
				for _, want := range []string{"config file", file.Path, "line 2", variable} {
					if !strings.Contains(msg, want) {
						t.Errorf("error %q does not name %q", msg, want)
					}
				}
				for _, leak := range []string{value, credential, "proxy.example.com"} {
					if strings.Contains(msg, leak) {
						t.Errorf("error %q echoes %q from the value", msg, leak)
					}
				}
			})
		}
	}
}
