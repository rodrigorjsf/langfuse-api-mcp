package sanitize_test

import (
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #210): sanitize.Payload, pure. A JSON text may repeat
// a key; a parser that keeps the first value, a streaming parser, or a model
// reading the text sees the value that decoding into a map drops. That value is
// cleaned and redacted like any other, and it is the last value that stays.
// Expected values are literals.

func TestPayloadCleansAValueThatALaterRepeatedKeyShadowsAtEveryLevel(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ inner, want string }{
		"hidden characters written as escapes": {
			inner: `{"a":"IGNORE` + hiddenAsEscapes() + `","a":"ok"}`,
			want:  `{"a":"ok"}`,
		},
		"a Langfuse key split by an escape": {
			inner: `{"a":"sk-lf-` + keyHead + uEsc("200b") + keyTail + `","a":"ok"}`,
			want:  `{"a":"ok"}`,
		},
		"a Langfuse key after an escaped hidden character": {
			inner: `{"a":"x` + uEsc("200b") + plantedSecretKey + `","a":"ok"}`,
			want:  `{"a":"ok"}`,
		},
		"a list of credentials under an Authorization name": {
			inner: `{"Authorization":["Basic ` + mapToken + `","Digest ` + otherMapToken + `"],"Authorization":"ok"}`,
			want:  `{"Authorization":"ok"}`,
		},
		"a shadowed member of a nested object": {
			inner: `{"outer":{"a":"x` + uEsc("0007") + `y","a":"ok"},"n":1}`,
			want:  `{"n":1,"outer":{"a":"ok"}}`,
		},
		"a shadowed member inside a list": {
			inner: `[{"a":"x` + uEsc("202e") + `y","a":"ok"},2]`,
			want:  `[{"a":"ok"},2]`,
		},
		"a key repeated three times": {
			inner: `{"a":"x` + uEsc("200b") + `","a":"y` + uEsc("200b") + `","a":"ok"}`,
			want:  `{"a":"ok"}`,
		},
		"a shadowed value that is itself JSON text": {
			inner: `{"a":"{\"b\":\"x` + uEsc("200b") + `y\"}","a":"ok"}`,
			want:  `{"a":"ok"}`,
		},
	}
	for name, tc := range tests {
		for n := 1; n <= 4; n++ {
			t.Run(name+" "+strings.Repeat("q", n), func(t *testing.T) {
				t.Parallel()

				got := cleaned(t, outputPayload(nested(t, tc.inner, n)))

				if want := outputPayload(nested(t, tc.want, n)); got != want {
					t.Fatalf("Payload = %s, want %s", got, want)
				}
				assertPayloadIs(t, outputPayload(nested(t, tc.inner, n)), got)
			})
		}
	}
	// The configured key pair, split by an escape in the shadowed value.
	r := sanitize.NewRedactor("hunter2-hunter2")
	inner := `{"a":"hunter2-` + uEsc("200b") + `hunter2","a":"ok"}`
	got, err := sanitize.Payload([]byte(outputPayload(nested(t, inner, 1))), r)
	if want := outputPayload(nested(t, `{"a":"ok"}`, 1)); err != nil || string(got) != want {
		t.Fatalf("configured secret: Payload = %s (err %v), want %s", got, err, want)
	}
}

// A repeated key whose shadowed value has nothing to clean leaves the text as it
// was, byte for byte, and so does a text that mentions what would make the
// server look at it: an escape of a visible character, the word Authorization.
func TestPayloadKeepsAJSONTextWithARepeatedKeyWhoseShadowedValueIsCleanByteForByte(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"plain values":             `{"a":1,"a":2}`,
		"an escape of a letter":    `{ "a": "caf` + uEsc("00e9") + `", "a": "ok" }`,
		"a tab and a line feed":    `{"a":"x\ty\n","a":"ok"}`,
		"the word Authorization":   `{"authorization_mode":"x","authorization_mode":"y"}`,
		"a header with no scheme":  `{"Authorization":"` + mapToken + `","Authorization":"ok"}`,
		"every feature together":   "{\n  \"b\": 1.50,\n  \"a\": \"caf" + uEsc("00e9") + "\",\n  \"id\": 12345678901234567890,\n  \"a\": \"ok\"\n}",
		"a repeated key in a list": `[{"a":"x` + uEsc("00e9") + `","a":"y"},{"a":1}]`,
	}
	for name, inner := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := outputPayload(quoteJSON(t, inner))

			assertPayloadIs(t, in, in)
		})
	}
}
