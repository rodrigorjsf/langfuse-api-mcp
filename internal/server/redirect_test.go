package server_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// redirectingLangfuse answers the first request with a 302 to the URL target
// returns, and records every request it receives.
func redirectingLangfuse(t *testing.T, target func(self *url.URL) string) (*httptest.Server, <-chan received) {
	t.Helper()
	seen := make(chan received, 8)
	var self *url.URL
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, hasAuth := r.BasicAuth()
		seen <- received{method: r.Method, path: r.URL.EscapedPath(), basicAuth: hasAuth}
		if strings.HasSuffix(r.URL.Path, "/redirected") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`)) // a failed write shows up as a tool error in the test
			return
		}
		http.Redirect(w, r, target(self), http.StatusFound)
	}))
	t.Cleanup(fake.Close)
	u, err := url.Parse(fake.URL)
	if err != nil {
		t.Fatalf("parse fake Langfuse URL: %v", err)
	}
	self = u
	return fake, seen
}

func TestExecuteReadRefusesARedirectToAnotherSchemeHostOrPort(t *testing.T) {
	t.Parallel()
	// other records whatever reaches the second server: nothing may.
	other, otherSeen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	tests := map[string]func(self *url.URL) string{
		"another host and port": func(*url.URL) string { return other.URL + "/api/public/traces" },
		"another host": func(self *url.URL) string {
			return "http://localhost:" + self.Port() + "/api/public/traces/redirected"
		},
		"another scheme": func(self *url.URL) string {
			return "https://" + self.Host + "/api/public/traces/redirected"
		},
		"another port": func(self *url.URL) string {
			return "http://" + self.Hostname() + ":" + other.URL[strings.LastIndex(other.URL, ":")+1:] + "/api/public/traces"
		},
	}
	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := redirectingLangfuse(t, target)
			cs := connect(t, fake)

			got := toolErrorFieldsOf(t, callExecuteRead(t, cs, map[string]any{"operationId": "trace_list"}))

			if got.Code != "redirect_refused" || got.OperationID != "trace_list" || got.Hint == "" {
				t.Errorf("error = %+v, want redirect_refused with a hint", got)
			}
			if first := receivedOne(t, seen); !first.basicAuth {
				t.Errorf("the first request carried no Basic auth")
			}
			select {
			case again := <-seen:
				t.Errorf("the redirect was followed: %s %s", again.method, again.path)
			default:
			}
		})
	}
	t.Cleanup(func() {
		select {
		case got := <-otherSeen:
			t.Errorf("the other server received %s %s (Basic auth: %v)", got.method, got.path, got.basicAuth)
		default:
		}
	})
}

func TestExecuteReadFollowsARedirectOnTheSameHostKeepingBasicAuth(t *testing.T) {
	t.Parallel()
	fake, seen := redirectingLangfuse(t, func(self *url.URL) string {
		return self.String() + "/api/public/traces/redirected"
	})
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{"operationId": "trace_list"})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	receivedOne(t, seen)
	if second := receivedOne(t, seen); !second.basicAuth || second.path != "/api/public/traces/redirected" {
		t.Errorf("redirected request = %+v, want Basic auth on /api/public/traces/redirected", second)
	}
}
