package langfuse_test

import (
	"net/http"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

// Ticket #45: the client's proxy function comes from resolved settings, not
// from the process environment, with Go's http.ProxyFromEnvironment semantics.
func TestProxyFromSettingsPicksTheProxyAsGoWouldForTheSameVariables(t *testing.T) {
	t.Parallel()
	proxy := langfuse.ProxyFromSettings("http://proxy.internal:3128", "http://plain.internal:3128", "langfuse.internal")
	tests := map[string]struct{ url, want string }{
		"an https host goes through HTTPS_PROXY": {"https://langfuse.test/api/public/health", "http://proxy.internal:3128"},
		"a NO_PROXY host is reached directly":    {"https://langfuse.internal/api/public/health", ""},
		"a loopback host is never proxied":       {"http://127.0.0.1:3000/api/public/health", ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}

			got, err := proxy(req)

			if err != nil {
				t.Fatalf("proxy(%s): %v", tc.url, err)
			}
			gotStr := "" // none
			if got != nil {
				gotStr = got.String()
			}
			if gotStr != tc.want {
				t.Errorf("proxy(%s) = %q, want %q", tc.url, gotStr, tc.want)
			}
		})
	}
}

func TestProxyFromSettingsWithNothingSetUsesNoProxy(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://langfuse.test/", nil)

	got, err := langfuse.ProxyFromSettings("", "", "")(req)

	if got != nil || err != nil {
		t.Errorf("proxy = %v, %v; want none", got, err)
	}
}
