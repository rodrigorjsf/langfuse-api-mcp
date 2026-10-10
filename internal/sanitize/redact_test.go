package sanitize_test

import (
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam: sanitize.Redactor (pure). Expected texts are literals.

// seededBearer is the string the campaign seeded into a trace (finding M6).
const seededBearer = "Authorization: Bearer seed-fake-bearer-token-00000000"

func TestRedactReplacesTheTokenOfAnAuthorizationOrBearerHeaderAndKeepsTheScheme(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"the seeded campaign string": {
			in: seededBearer, want: "Authorization: Bearer [REDACTED]",
		},
		"a Bearer token alone": {
			in: "sent Bearer abcdefghijklmnop to the API", want: "sent Bearer [REDACTED] to the API",
		},
		"a JWT": {
			in:   "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
			want: "Bearer [REDACTED]",
		},
		"any other scheme after Authorization, in any case": {
			in:   "authorization: basic dXNlcjpwYXNzd29yZDEyMzQ1",
			want: "authorization: basic [REDACTED]",
		},
		"a scheme with digits and dashes": {
			in:   "Authorization: AWS4-HMAC-SHA256 0123456789abcdef0123",
			want: "Authorization: AWS4-HMAC-SHA256 [REDACTED]",
		},
		"bearer in lower and upper case": {
			in: "bearer abcdefghijklmnop and BEARER qrstuvwxyz012345", want: "bearer [REDACTED] and BEARER [REDACTED]",
		},
		"tabs between the words": {
			in: "Authorization:\tBearer\tabcdefghijklmnop", want: "Authorization:\tBearer\t[REDACTED]",
		},
		"a token that stops at a quote": {
			in: `{"h":"Authorization: Bearer abcdefghijklmnop"}`, want: `{"h":"Authorization: Bearer [REDACTED]"}`,
		},
		"a token that stops at a JSON escape": {
			in: `"Bearer abcdefghijklmnop\nnext line"`, want: `"Bearer [REDACTED]\nnext line"`,
		},
		"a token that stops at a closing single quote": {
			in: "curl -H 'Authorization: Bearer abcdefghijklmnop' https://example.test", want: "curl -H 'Authorization: Bearer [REDACTED]' https://example.test",
		},
		"two tokens in one text": {
			in:   "Bearer abcdefghijklmnop then Authorization: Token 0123456789abcdefgh",
			want: "Bearer [REDACTED] then Authorization: Token [REDACTED]",
		},
		"a token with the characters of a base64 or URL-safe credential": {
			in: "Bearer ab+cd/ef-gh_ij.kl~mn==", want: "Bearer [REDACTED]",
		},
		"a Langfuse key next to a Bearer token": {
			in: "pk-lf-abc123 Bearer abcdefghijklmnop", want: "[REDACTED] Bearer [REDACTED]",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (sanitize.Redactor{}).Redact(tc.in); got != tc.want {
				t.Fatalf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedactKeepsProseThatOnlyMentionsBearerOrAuthorization(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"the words in a sentence":                         "The Authorization header carries a Bearer token that expires after an hour.",
		"a bearer of news":                                "the bearer of good news arrived",
		"Authorization with a value that is not a scheme": "Authorization: required",
		"a header without a token":                        "Authorization: Bearer",
		"a short token, one under the minimum":            "Bearer abcdefghijklmno",
		"a short word after a scheme":                     "Authorization: Basic dXNlcjpw",
		"a long word after a word that is not the scheme": "the Bearer is authentication-and-authorization-service",
		"a word that ends in bearer":                      "NotBearer abcdefghijklmnopqrstuvwxyz",
		"a word that ends in authorization":               "preauthorization: Bearer",
		"the token on the next line":                      "Authorization: Bearer\nabcdefghijklmnopqrstuvwxyz",
		"no Authorization colon":                          "Authorization Bearer",
		"the redaction marker itself":                     "Authorization: Bearer [REDACTED]",
		"a colon with no scheme before the token":         "Authorization: abcdefghijklmnopqrstuvwxyz",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (sanitize.Redactor{}).Redact(in); got != in {
				t.Fatalf("Redact(%q) = %q, want it unchanged", in, got)
			}
		})
	}
}

// A token is redacted from its sixteenth character on: sixteen is the
// documented minimum, one less is prose.
func TestRedactTakesATokenOfSixteenCharactersAndNotOneOfFifteen(t *testing.T) {
	t.Parallel()
	const fifteen = "abcdefghijklmno"
	if got := (sanitize.Redactor{}).Redact("Bearer " + fifteen); got != "Bearer "+fifteen {
		t.Errorf("Redact of a 15-character token = %q, want it unchanged", got)
	}
	if got := (sanitize.Redactor{}).Redact("Bearer " + fifteen + "p"); got != "Bearer [REDACTED]" {
		t.Errorf("Redact of a 16-character token = %q, want %q", got, "Bearer [REDACTED]")
	}
}

// The marker is shorter than the minimum, so redacting twice (a payload is
// redacted per string, then the whole result again) changes nothing.
func TestRedactingARedactedTextChangesNothing(t *testing.T) {
	t.Parallel()
	r := sanitize.NewRedactor("hunter2-hunter2")
	once := r.Redact(seededBearer + " pk-lf-abc123 hunter2-hunter2")
	if once != "Authorization: Bearer [REDACTED] [REDACTED] [REDACTED]" {
		t.Fatalf("Redact = %q", once)
	}
	if twice := r.Redact(once); twice != once {
		t.Fatalf("Redact of a redacted text = %q, want %q", twice, once)
	}
}

// #180: an invisible character inside a secret does not hide it. The
// characters are dropped from a text that held a secret, where they would be
// dropped before the agent saw it anyway; a text with no secret keeps them for
// its own caller to judge.
func TestRedactFindsASecretSplitByInvisibleCharacters(t *testing.T) {
	t.Parallel()
	zwsp, bidi, tag := string(rune(0x200b)), string(rune(0x202e)), string(rune(0xE0041))
	r := sanitize.NewRedactor("hunter2-hunter2")
	tests := map[string]struct{ in, want string }{
		"a Bearer token":           {in: "Bearer abcdefgh" + zwsp + "ijklmnop" + bidi + "qrst", want: "Bearer [REDACTED]"},
		"an Authorization header":  {in: "Authorization:" + zwsp + " Bearer abcdefgh" + tag + "ijklmnop", want: "Authorization: Bearer [REDACTED]"},
		"the word Bearer":          {in: "Bea" + zwsp + "rer abcdefghijklmnop", want: "Bearer [REDACTED]"},
		"a Langfuse key":           {in: "key sk-lf-ab" + zwsp + "cd-ef end", want: "key [REDACTED] end"},
		"a configured secret":      {in: "x hunter2-" + zwsp + "hunter2 y", want: "x [REDACTED] y"},
		"a control character":      {in: "Bearer abcdefgh\x07ijklmnopqr", want: "Bearer [REDACTED]"},
		"text with no secret":      {in: "a" + zwsp + "b" + bidi + "c", want: "a" + zwsp + "b" + bidi + "c"},
		"a short token, still not": {in: "Bearer abc" + zwsp + "def", want: "Bearer abc" + zwsp + "def"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := r.Redact(tc.in); got != tc.want {
				t.Fatalf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A cut that falls inside a token must not leave a prefix of it: callers
// redact first and cut after, and redacting is by token, not by the text a
// cut leaves.
func TestRedactReplacesAWholeLongTokenSoNoPrefixOfItSurvivesACutAfterwards(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("0123456789abcdef", 8)
	got := (sanitize.Redactor{}).Redact("Authorization: Bearer " + token)
	for cut := 1; cut <= len(got); cut++ {
		if strings.Contains(got[:cut], token[:8]) {
			t.Fatalf("a cut at %d of %q still shows the start of the token", cut, got)
		}
	}
}
