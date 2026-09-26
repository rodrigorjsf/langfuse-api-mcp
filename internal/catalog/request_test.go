package catalog_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Any value a caller passes as a path parameter either is refused as an
// invalid parameter or stays one percent-encoded segment of the operation's
// path: it can never add a segment, climb out of the path or name a host.
func FuzzRequestKeepsAPathParameterInsideItsSegment(f *testing.F) {
	for _, seed := range []string{"trace-1", "a/b", "..", "%2e%2e", "https://evil.example", "//x", `a\b`, "user:42 chat?x#y", "\x00"} {
		f.Add(seed)
	}
	op, ok := mustLoad(f).Lookup("sessions_get")
	if !ok {
		f.Fatal("sessions_get missing from the catalog")
	}
	f.Fuzz(func(t *testing.T, value string) {
		req, err := op.Request(map[string]any{"sessionId": value})
		if err != nil {
			if !errors.Is(err, catalog.ErrInvalidParameter) {
				t.Fatalf("Request(%q) error %v does not wrap ErrInvalidParameter", value, err)
			}
			return
		}
		segment, ok := strings.CutPrefix(req.Path, "/api/public/sessions/")
		if !ok || strings.Contains(segment, "/") {
			t.Fatalf("Request(%q) path = %s, want one segment below /api/public/sessions/", value, req.Path)
		}
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded != value || decoded == ".." || decoded == "." {
			t.Fatalf("Request(%q) segment %q decodes to %q (%v)", value, segment, decoded, err)
		}
	})
}
