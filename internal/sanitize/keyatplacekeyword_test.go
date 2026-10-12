package sanitize_test

import (
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #211): sanitize.Redactor, sanitize.Payload and
// sanitize.LineValue, pure. A Langfuse key found at a place where the cleaning
// removed a character takes every key character after its prefix, a hyphen or
// an underscore and the letters of a header keyword included. The keyword it
// ran into still leads to its token: the token is replaced too, as it is where
// nothing was removed. Expected texts are literals.

func TestRedactReplacesTheTokenOfAHeaderWhoseKeywordAKeyFoundAtAPlaceRanInto(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"Bearer after a hyphen": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc-Bearer " + escapeToken,
			want: "x[REDACTED] [REDACTED]",
		},
		"Authorization after a hyphen": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc-Authorization: Basic " + escapeToken,
			want: "x[REDACTED]: Basic [REDACTED]",
		},
		"authorization after an underscore": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc_authorization: Basic " + escapeToken,
			want: "x[REDACTED]: Basic [REDACTED]",
		},
		"a public key after a bell": {
			in:   "id" + bellChar + "pk-lf-1234-bearer " + escapeToken,
			want: "id[REDACTED] [REDACTED]",
		},
		"after a colour sequence": {
			in:   "x" + escapeChar + "[31msk-lf-abc-Bearer " + escapeToken,
			want: "x[REDACTED] [REDACTED]",
		},
		"the keyword more than 64 bytes into the key": {
			in:   "x" + zeroWidthSpace + "sk-lf-" + seventyLetters + "-Bearer " + escapeToken,
			want: "x[REDACTED] [REDACTED]",
		},
		"two keywords in the key, the token after the last": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc-Bearer-Authorization: Basic " + escapeToken,
			want: "x[REDACTED]: Basic [REDACTED]",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

const seventyLetters = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// What the key ran into is no header credential: the key goes and the rest stays.
func TestRedactKeepsTheTextAfterAKeyFoundAtAPlaceThatRanIntoNoHeaderCredential(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a token of 15 characters": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc-Bearer abcdefghijklmno",
			want: "x[REDACTED] abcdefghijklmno",
		},
		"Bearer after an underscore starts no word": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc_Bearer " + escapeToken,
			want: "x[REDACTED] " + escapeToken,
		},
		"a key that only holds the letters": {
			in:   "x" + zeroWidthSpace + "sk-lf-abcbearer " + escapeToken,
			want: "x[REDACTED] " + escapeToken,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// The same in a payload string and in the JSON text inside one.
func TestPayloadRedactsTheTokenOfAHeaderWhoseKeywordAKeyFoundAtAPlaceRanInto(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a value": {
			in:   `{"output":"x` + uEsc("200b") + `sk-lf-abc-Bearer ` + escapeToken + `"}`,
			want: `{"output":"x[REDACTED] [REDACTED]"}`,
		},
		"JSON text, the place written as an escape": {
			in:   `{"output":"{\"b\":\"x\\` + "u200b" + `sk-lf-abc-Bearer ` + escapeToken + `\"}"}`,
			want: `{"output":"{\"b\":\"x[REDACTED] [REDACTED]\"}"}`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertPayloadIs(t, tc.in, tc.want)
		})
	}
}

// The same on one line: a line break or a tab Line drops is a place.
func TestLineValueRedactsTheTokenOfAHeaderWhoseKeywordAKeyAfterADroppedBreakRanInto(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"Bearer, after a line feed": {
			in: `["note\nsk-lf-abc-Bearer ` + escapeToken + `"]`, want: `["note[REDACTED] [REDACTED]"]`,
		},
		"authorization after an underscore, after a tab": {
			in:   `["note\tsk-lf-abc_authorization: Basic ` + escapeToken + `"]`,
			want: `["note[REDACTED]: Basic [REDACTED]"]`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := lineValueOf(t, tc.in); got != tc.want {
				t.Fatalf("LineValue(%s) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}
