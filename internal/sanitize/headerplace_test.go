package sanitize_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #211): sanitize.Redactor and sanitize.Payload, pure.
// A place where the cleaning removed a character (#210) is a start for the
// header keywords too: "Bearer", and the name of the plain form
// "Authorization:", directly after it are found whatever stands before the
// place. The characters the cleaning removed are gone from a text that held a
// credential, and stay in one that held none. Expected texts are literals.

func TestRedactReplacesTheTokenOfAHeaderWhoseKeywordDirectlyFollowsACharacterTheCleaningRemoved(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"Bearer after a zero-width space": {
			in: "x" + zeroWidthSpace + "Bearer " + escapeToken, want: "xBearer [REDACTED]",
		},
		"Authorization after a bell": {
			in: "x" + bellChar + "Authorization: Basic " + escapeToken, want: "xAuthorization: Basic [REDACTED]",
		},
		"Bearer after a colour sequence": {
			in: "x" + escapeChar + "[31mBearer " + escapeToken, want: "xBearer [REDACTED]",
		},
		"Authorization after a hyperlink sequence": {
			in:   "x" + escapeChar + "]8;;https://e.example/a" + bellChar + "Authorization: Digest " + escapeToken,
			want: "xAuthorization: Digest [REDACTED]",
		},
		"Bearer after a one-character CSI":     {in: "x\u009b31mBearer " + escapeToken, want: "xBearer [REDACTED]"},
		"Bearer after an escape starting none": {in: "x" + escapeChar + "Bearer " + escapeToken, want: "xBearer [REDACTED]"},
		"Bearer after a NUL":                   {in: "x\x00BEARER " + escapeToken, want: "xBEARER [REDACTED]"},
		"Bearer after a bidi override":         {in: "v2" + bidiOverride + "bearer " + escapeToken, want: "v2bearer [REDACTED]"},
		"Bearer after a run of removed ones":   {in: "x" + zeroWidthSpace + bellChar + bidiOverride + "Bearer " + escapeToken, want: "xBearer [REDACTED]"},
		"a name ending in the word, accepted":  {in: "re" + zeroWidthSpace + "authorization: Basic " + escapeToken, want: "reauthorization: Basic [REDACTED]"},
		"Proxy-Authorization after a place":    {in: "x" + bellChar + "Proxy-Authorization: Digest " + escapeToken, want: "xProxy-Authorization: Digest [REDACTED]"},
		"a place and blanks in the plain form": {in: "x" + bellChar + "Authorization \t: \tBasic " + escapeToken, want: "xAuthorization \t: \tBasic [REDACTED]"},
		"a token that a place splits":          {in: "x" + zeroWidthSpace + "Bearer abcdefgh" + bellChar + "ijklmnopqr", want: "xBearer [REDACTED]"},
		"two headers, each after a place":      {in: "a" + zeroWidthSpace + "Bearer " + escapeToken + " b" + bellChar + "Bearer " + escapeToken, want: "aBearer [REDACTED] bBearer [REDACTED]"},
		"a key and a header, each after a place": {
			in:   "a" + zeroWidthSpace + "sk-lf-abc b" + bellChar + "Bearer " + escapeToken,
			want: "a[REDACTED] bBearer [REDACTED]",
		},
		"a header and a key, each after a place": {
			in:   "a" + zeroWidthSpace + "Bearer " + escapeToken + " b" + bellChar + "pk-lf-abc",
			want: "aBearer [REDACTED] b[REDACTED]",
		},
		"a header after a place and a header after an escape": {
			in:   `a\nBearer ` + escapeToken + " b" + bellChar + "Bearer " + escapeToken,
			want: `a\nBearer [REDACTED] bBearer [REDACTED]`,
		},
		// The token of the first credential runs up to the keyword of the second,
		// which the removed character glued to it: the keyword is still a start.
		"Bearer after a place that ends the token of another Bearer": {
			in:   "Bearer abcdefghijklmnopqrst" + zeroWidthSpace + "Bearer " + escapeToken,
			want: "Bearer [REDACTED] [REDACTED]",
		},
		"Authorization after a place that ends the token of another one": {
			in:   "Authorization: Basic " + escapeToken + bellChar + "Authorization: Basic ABCDEFGHIJKLMNOPQRSTUVWX",
			want: "Authorization: Basic [REDACTED] Basic [REDACTED]",
		},
		"Proxy-Authorization after a place that ends the token of Authorization": {
			in:   "Authorization: Basic " + escapeToken + bellChar + "Proxy-Authorization: Basic ABCDEFGHIJKLMNOPQRSTUVWX",
			want: "Authorization: Basic [REDACTED] Basic [REDACTED]",
		},
		"a dotted name after a place that ends the token of Bearer": {
			in:   "Bearer " + escapeToken + zeroWidthSpace + "http.request.header.authorization: Digest ABCDEFGHIJKLMNOPQRSTUVWX",
			want: "Bearer [REDACTED] Digest [REDACTED]",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// Where nothing is removed, or no credential follows the place, or the place is
// inside the keyword or the token, the rules of the header are as they were.
func TestRedactKeepsTheRulesOfAHeaderWhereNothingIsRemovedOrNoCredentialFollowsThePlace(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a letter glued to Bearer, nothing removed": {
			in: "xBearer " + escapeToken, want: "xBearer " + escapeToken,
		},
		"a letter glued to Authorization, nothing removed": {
			in: "xAuthorization: Basic " + escapeToken, want: "xAuthorization: Basic " + escapeToken,
		},
		"a place before a token of 15 characters": {
			in: "x" + zeroWidthSpace + "Bearer abcdefghijklmno", want: "x" + zeroWidthSpace + "Bearer abcdefghijklmno",
		},
		"a place before Bearer with no token": {
			in: "x" + zeroWidthSpace + "Bearer", want: "x" + zeroWidthSpace + "Bearer",
		},
		"a place before Authorization with no scheme": {
			in: "x" + bellChar + "Authorization: required", want: "x" + bellChar + "Authorization: required",
		},
		"a place before a name that only holds the word": {
			in: "x" + bellChar + "authorizationMode: Basic " + escapeToken, want: "x" + bellChar + "authorizationMode: Basic " + escapeToken,
		},
		"a place inside the keyword": {
			in: "xBea" + zeroWidthSpace + "rer " + escapeToken, want: "xBea" + zeroWidthSpace + "rer " + escapeToken,
		},
		"a place in a text with no keyword": {
			in: "x" + zeroWidthSpace + "y", want: "x" + zeroWidthSpace + "y",
		},
		"a name that only holds the word, after a token": {
			in:   "Bearer " + escapeToken + bellChar + "reauthorization: Basic ABCDEFGHIJKLMNOPQRSTUVWX",
			want: "Bearer [REDACTED] Basic ABCDEFGHIJKLMNOPQRSTUVWX",
		},
		"a name longer than the bound, after a token": {
			in:   "Bearer " + escapeToken + bellChar + strings.Repeat("a-", 40) + "authorization: Basic ABCDEFGHIJKLMNOPQRSTUVWX",
			want: "Bearer [REDACTED] Basic ABCDEFGHIJKLMNOPQRSTUVWX",
		},
		"a place before a word that ends in bearer": {
			in: "x" + zeroWidthSpace + "NotBearer " + escapeToken, want: "x" + zeroWidthSpace + "NotBearer " + escapeToken,
		},
		"a place after a blank, the pattern's own": {
			in: "x " + zeroWidthSpace + "Bearer " + escapeToken, want: "x Bearer [REDACTED]",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// The same places in a payload: a string value, an object key, a list item and
// the JSON text of a string. The result is compact JSON.
func TestPayloadRedactsAHeaderWhoseKeywordDirectlyFollowsACharacterTheCleaningRemoved(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a value, the escape of a zero-width space": {
			in: `{"a":"x` + uEsc("200b") + `Bearer ` + escapeToken + `"}`, want: `{"a":"xBearer [REDACTED]"}`,
		},
		"a value, a bell, Authorization": {
			in: `{"a":"x` + uEsc("0007") + `Authorization: Basic ` + escapeToken + `"}`, want: `{"a":"xAuthorization: Basic [REDACTED]"}`,
		},
		"a value, a colour sequence": {
			in: `{"a":"x` + uEsc("001b") + `[31mBearer ` + escapeToken + `"}`, want: `{"a":"xBearer [REDACTED]"}`,
		},
		"an object key": {
			in: `{"x` + uEsc("200b") + `Bearer ` + escapeToken + `":"v"}`, want: `{"xBearer [REDACTED]":"v"}`,
		},
		"a list item": {
			in: `["a","x` + uEsc("202e") + `Bearer ` + escapeToken + `"]`, want: `["a","xBearer [REDACTED]"]`,
		},
		"JSON text, a zero-width space written as an escape": {
			in:   `{"output":"{\"b\":\"x\\` + "u200b" + `Bearer ` + escapeToken + `\"}"}`,
			want: `{"output":"{\"b\":\"xBearer [REDACTED]\"}"}`,
		},
		"a letter glued to Bearer stays, nothing removed": {
			in: `{"a":"xBearer ` + escapeToken + `"}`, want: `{"a":"xBearer ` + escapeToken + `"}`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertPayloadIs(t, tc.in, tc.want)
		})
	}
}

// FuzzRedactHeaderAfterEscapeOrPlace checks, for any text put around a keyword
// and a token, after a JSON escape written as text or a removed character, that
// redacting twice changes nothing and that valid JSON stays valid JSON.
func FuzzRedactHeaderAfterEscapeOrPlace(f *testing.F) {
	f.Add("x", `\n`, "Bearer "+escapeToken, "y")
	f.Add("", `\\t`, "Authorization: Basic "+escapeToken, "")
	f.Add("a ", `\\\\u000a`, "Proxy-Authorization: Digest "+escapeToken, `\"`)
	f.Add("{\"k\":\"", `\r`, "bearer "+escapeToken, `\n"}`)
	f.Add("tok", zeroWidthSpace, "Bearer "+escapeToken, "")
	f.Add("v", bellChar, "Authorization: Basic "+escapeToken, " Bearer "+escapeToken)
	f.Add("a ", escapeChar+"[31m", "Bearer "+escapeToken+zeroWidthSpace+"Bearer "+escapeToken, `"}`)
	f.Add("x", zeroWidthSpace, "sk-lf-abc123"+zeroWidthSpace+"Bearer "+escapeToken, "")
	f.Add("x", bellChar, "Bearer "+escapeToken+bellChar+"Authorization: Basic "+escapeToken, " y")
	r := sanitize.NewRedactor("hunter2-hunter2")
	f.Fuzz(func(t *testing.T, before, between, header, after string) {
		in := before + between + header + after
		once := r.Redact(in)
		if twice := r.Redact(once); twice != once {
			t.Fatalf("Redact(%q) = %q, but redacting it again gives %q", in, once, twice)
		}
		if json.Valid([]byte(in)) && !json.Valid([]byte(once)) {
			t.Fatalf("Redact(%q) = %q, which is not JSON", in, once)
		}
	})
}
