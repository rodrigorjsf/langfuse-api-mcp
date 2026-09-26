package catalog_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Any value a caller passes as a path parameter either is refused as an
// invalid parameter or stays one percent-encoded segment of the operation's
// path: it can never add a segment, a query or a fragment, climb out of the
// path or name a host. That holds for an ID (sessions_get) and for a Folder
// name (prompts_get), whose "/" are sent as %2F and whose folders are never
// empty, "." or "..".
func FuzzRequestKeepsAPathParameterInsideItsSegment(f *testing.F) {
	for _, seed := range []string{
		"trace-1", "a/b", "..", "%2e%2e", "https://evil.example", "//x", `a\b`, "user:42 chat?x#y", "\x00",
		"folder/sub/name", "a/../b", "a/./b", "/a", "a/", "a//b", "v1..2", "a/%2F/b", "a/b?c#d",
	} {
		f.Add(seed)
	}
	cat := mustLoad(f)
	targets := []struct{ operationID, param, prefix string }{
		{"sessions_get", "sessionId", "/api/public/sessions/"},
		{"prompts_get", "promptName", "/api/public/v2/prompts/"},
	}
	ops := make([]catalog.Operation, len(targets))
	for i, tg := range targets {
		op, ok := cat.Lookup(tg.operationID)
		if !ok {
			f.Fatalf("%s missing from the catalog", tg.operationID)
		}
		ops[i] = op
	}
	f.Fuzz(func(t *testing.T, value string) {
		for i, tg := range targets {
			req, err := ops[i].Request(map[string]any{tg.param: value})
			if err != nil {
				if !errors.Is(err, catalog.ErrInvalidParameter) {
					t.Fatalf("%s: Request(%q) error %v does not wrap ErrInvalidParameter", tg.operationID, value, err)
				}
				continue
			}
			segment, ok := strings.CutPrefix(req.Path, tg.prefix)
			if !ok || strings.ContainsAny(segment, "/?#") || len(req.Query) != 0 {
				t.Fatalf("%s: Request(%q) = %s?%s, want one segment below %s and no query",
					tg.operationID, value, req.Path, req.Query.Encode(), tg.prefix)
			}
			decoded, err := url.PathUnescape(segment)
			if err != nil || decoded != value {
				t.Fatalf("%s: Request(%q) segment %q decodes to %q (%v)", tg.operationID, value, segment, decoded, err)
			}
			for folder := range strings.SplitSeq(decoded, "/") {
				if folder == "" || folder == "." || folder == ".." {
					t.Fatalf("%s: Request(%q) accepted an empty, \".\" or \"..\" segment", tg.operationID, value)
				}
			}
		}
	})
}

// Any value a caller passes as the JSON query of a metrics operation either is
// refused as an invalid parameter or becomes the single query parameter of the
// request: one JSON object whose config.row_limit is from 1 to 1000. It can
// never add a query parameter, change the path or carry data after the object.
func FuzzRequestKeepsAMetricsQueryInsideTheQueryParameter(f *testing.F) {
	for _, seed := range []string{
		`{"view":"observations","metrics":[{"measure":"count","aggregation":"count"}]}`,
		`{"view":"x","config":{"row_limit":1000}}`, `{"config":{"row_limit":1001}}`, `{"config":null}`,
		`{"view":"x"}&limit=5000`, `{"view":"x"}{"view":"y"}`, `{"view":"x","view":"y"}`,
		`{"config":{"row_limit":1},"config":{"row_limit":5000}}`, `{"a":1}`, `[]`, `null`, "\x00", `{"view":"/../x?y#z"}`,
	} {
		f.Add(seed)
	}
	op, ok := mustLoad(f).Lookup("metrics_metrics")
	if !ok {
		f.Fatal("metrics_metrics missing from the catalog")
	}
	f.Fuzz(func(t *testing.T, value string) {
		req, err := op.Request(map[string]any{"query": value})
		if err != nil {
			if !errors.Is(err, catalog.ErrInvalidParameter) {
				t.Fatalf("Request(%q) error %v does not wrap ErrInvalidParameter", value, err)
			}
			return
		}
		if req.Method != "GET" || req.Path != "/api/public/v2/metrics" || len(req.Query) != 1 || len(req.Query["query"]) != 1 {
			t.Fatalf("Request(%q) = %s %s?%s, want GET /api/public/v2/metrics with one query parameter",
				value, req.Method, req.Path, req.Query.Encode())
		}
		dec := json.NewDecoder(strings.NewReader(req.Query.Get("query")))
		var sent struct {
			Config struct {
				RowLimit int `json:"row_limit"`
			} `json:"config"`
		}
		if err := dec.Decode(&sent); err != nil || dec.More() ||
			sent.Config.RowLimit < 1 || sent.Config.RowLimit > 1000 {
			t.Fatalf("Request(%q) sends query %q (%v), want one JSON object with config.row_limit from 1 to 1000",
				value, req.Query.Get("query"), err)
		}
	})
}
