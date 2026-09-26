package langfuse_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

const (
	keyPairPublic = "pk-lf-keypair-public"
	keyPairSecret = "sk-lf-keypair-secret" //nolint:gosec // G101: a fake key planted to prove it never prints
)

// assertNoKey fails the test when out holds either key of the pair.
func assertNoKey(t *testing.T, out string) {
	t.Helper()
	for _, key := range []string{keyPairPublic, keyPairSecret} {
		if strings.Contains(out, key) {
			t.Fatalf("output leaks %s:\n%s", key, out)
		}
	}
}

func TestAKeyPairNeverPrintsEitherKeyThroughFmt(t *testing.T) {
	t.Parallel()
	keys := langfuse.NewKeyPair(keyPairPublic, keyPairSecret)

	out := fmt.Sprintf("%v %+v %#v %s %q %x %X %d", keys, keys, keys, keys, keys, keys, keys, keys)

	assertNoKey(t, out)
}

func TestAKeyPairNeverPrintsEitherKeyThroughJSON(t *testing.T) {
	t.Parallel()
	keys := langfuse.NewKeyPair(keyPairPublic, keyPairSecret)

	out, err := json.Marshal(struct {
		Keys langfuse.KeyPair `json:"keys"`
	}{keys})
	if err != nil {
		t.Fatal(err)
	}

	if want := `{"keys":"[REDACTED]"}`; string(out) != want {
		t.Fatalf("json.Marshal = %s, want %s", out, want)
	}
}

func TestAKeyPairNeverLogsEitherKeyThroughSlog(t *testing.T) {
	t.Parallel()
	keys := langfuse.NewKeyPair(keyPairPublic, keyPairSecret)

	var out bytes.Buffer
	slog.New(slog.NewJSONHandler(&out, nil)).Info("x", "keys", keys)
	slog.New(slog.NewTextHandler(&out, nil)).Info("x", "keys", keys)

	assertNoKey(t, out.String())
}

func TestAStructHoldingAKeyPairUnexportedNeverPrintsEitherKey(t *testing.T) {
	t.Parallel()
	// fmt cannot call the methods of an unexported field, so the key pair
	// must not hold the keys as plain fields either.
	holder := struct {
		name string
		keys langfuse.KeyPair
	}{"copy", langfuse.NewKeyPair(keyPairPublic, keyPairSecret)}

	var out bytes.Buffer
	fmt.Fprintf(&out, "%v %+v %#v", holder, holder, holder)
	slog.New(slog.NewTextHandler(&out, nil)).Info("x", "holder", holder)

	assertNoKey(t, out.String())
}

func TestAKeyPairHandsTheRedactorTheKeysAndTheirAuthorizationValues(t *testing.T) {
	t.Parallel()
	keys := langfuse.NewKeyPair("pk-lf-a", "sk-lf-b")

	got := keys.RedactionValues()

	// base64("pk-lf-a:sk-lf-b") worked out by hand: cGstbGYtYTpzay1sZi1i.
	want := []string{"pk-lf-a", "sk-lf-b", "cGstbGYtYTpzay1sZi1i", "Basic cGstbGYtYTpzay1sZi1i"}
	if !slices.Equal(got, want) {
		t.Fatalf("RedactionValues() = %q, want %q", got, want)
	}
}
