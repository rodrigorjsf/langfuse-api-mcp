package langfuse_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

func TestTheClientAndItsOptionsNeverPrintTheKeyPair(t *testing.T) {
	t.Parallel()
	const public, secret = "pk-lf-print-public", "sk-lf-print-secret" //nolint:gosec // G101: fake keys planted to prove they never print
	host, err := url.Parse("https://cloud.langfuse.com")
	if err != nil {
		t.Fatal(err)
	}
	opts := langfuse.Options{Host: host, Keys: langfuse.NewKeyPair(public, secret)}
	client := langfuse.New(opts)

	var out bytes.Buffer
	fmt.Fprintf(&out, "%v %+v %#v %v %+v %#v", opts, opts, opts, client, client, *client)
	slog.New(slog.NewJSONHandler(&out, nil)).Info("x", "options", opts, "client", client)
	slog.New(slog.NewTextHandler(&out, nil)).Info("x", "options", opts, "client", client)

	for _, key := range []string{public, secret} {
		if strings.Contains(out.String(), key) {
			t.Fatalf("printed client or options leak %s:\n%s", key, out.String())
		}
	}
}
