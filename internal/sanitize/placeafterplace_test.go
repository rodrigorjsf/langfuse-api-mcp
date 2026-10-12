package sanitize_test

import (
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #211): sanitize.Redactor, sanitize.Payload and
// sanitize.LineValue, pure. A credential found at a place where the cleaning
// removed a character runs up to the next blank, so it takes with it a header
// keyword that a later removed character glued to it. That later place is still
// a start: the token after the keyword is replaced too, and no token is shown.
// Expected texts are literals.

const secondToken = "ABCDEFGHIJKLMNOPQRSTUVWX"

func TestRedactReplacesTheTokenOfAHeaderWhoseKeywordIsGluedToACredentialFoundAtAnEarlierPlace(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a short key at a place, Bearer at the next": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc123" + zeroWidthSpace + "Bearer " + escapeToken,
			want: "x[REDACTED] [REDACTED]",
		},
		"a short key at a place, Authorization at the next": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc123" + bellChar + "Authorization: Basic " + escapeToken,
			want: "x[REDACTED]: Basic [REDACTED]",
		},
		"a short key at a place, Proxy-Authorization at the next": {
			in:   "x" + zeroWidthSpace + "pk-lf-abc123" + bellChar + "Proxy-Authorization: Digest " + escapeToken,
			want: "x[REDACTED]: Digest [REDACTED]",
		},
		"a short key at a place, split by another, Bearer at a third": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc" + bellChar + "def" + zeroWidthSpace + "Bearer " + escapeToken,
			want: "x[REDACTED] [REDACTED]",
		},
		"two short keys, each at a place, Bearer at a third": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc" + zeroWidthSpace + "pk-lf-def" + zeroWidthSpace + "Bearer " + escapeToken,
			want: "x[REDACTED] [REDACTED]",
		},
		"a prefix at a place, Bearer at the next": {
			in:   "x" + zeroWidthSpace + "sk-lf-" + zeroWidthSpace + "Bearer " + escapeToken,
			want: "x[REDACTED] [REDACTED]",
		},
		"Bearer at a place, Bearer at the next": {
			in:   "x" + zeroWidthSpace + "Bearer " + escapeToken + zeroWidthSpace + "Bearer " + secondToken,
			want: "xBearer [REDACTED] [REDACTED]",
		},
		"Bearer at a place after a blank, Bearer at the next": {
			in:   "x " + zeroWidthSpace + "Bearer " + escapeToken + zeroWidthSpace + "Bearer " + secondToken,
			want: "x Bearer [REDACTED] [REDACTED]",
		},
		"Bearer at a place, Authorization at the next": {
			in:   "x" + bellChar + "Bearer " + escapeToken + bellChar + "Authorization: Basic " + secondToken,
			want: "xBearer [REDACTED] Basic [REDACTED]",
		},
		"a short key, Bearer and Bearer, each at a place": {
			in: "x" + zeroWidthSpace + "sk-lf-abc123" + zeroWidthSpace + "Bearer " + escapeToken +
				zeroWidthSpace + "Bearer " + secondToken,
			want: "x[REDACTED] [REDACTED] [REDACTED]",
		},
		"a short key at a place, a colour sequence and Bearer": {
			in:   "x" + bellChar + "sk-lf-abc123" + escapeChar + "[31mBearer " + escapeToken,
			want: "x[REDACTED] [REDACTED]",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// What follows the key at the later place is no header credential: the key is
// found whole and the text after it stays.
func TestRedactKeepsTheTextAfterAKeyFoundAtAPlaceWhenNoHeaderCredentialFollowsTheNextPlace(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a token of 15 characters": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc123" + zeroWidthSpace + "Bearer abcdefghijklmno",
			want: "x[REDACTED] abcdefghijklmno",
		},
		"Bearer with no token": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc123" + zeroWidthSpace + "Bearer of bad news",
			want: "x[REDACTED] of bad news",
		},
		"a name that only holds the word": {
			in:   "x" + zeroWidthSpace + "sk-lf-abc123" + bellChar + "authorizationMode: Basic " + escapeToken,
			want: "x[REDACTED]: Basic " + escapeToken,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// The same in a payload string, which is what execute_read returns, and in the
// JSON text inside one.
func TestPayloadRedactsTheTokenOfAHeaderWhoseKeywordIsGluedToAKeyFoundAtAnEarlierPlace(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a value": {
			in:   `{"output":"x` + uEsc("200b") + `sk-lf-abc123` + uEsc("200b") + `Bearer ` + escapeToken + `"}`,
			want: `{"output":"x[REDACTED] [REDACTED]"}`,
		},
		"a value, Authorization after a bell": {
			in:   `{"output":"x` + uEsc("200b") + `sk-lf-abc123` + uEsc("0007") + `Authorization: Basic ` + escapeToken + `"}`,
			want: `{"output":"x[REDACTED]: Basic [REDACTED]"}`,
		},
		"an object key": {
			in:   `{"x` + uEsc("200b") + `sk-lf-abc123` + uEsc("200b") + `Bearer ` + escapeToken + `":"v"}`,
			want: `{"x[REDACTED] [REDACTED]":"v"}`,
		},
		"two Bearer credentials, each after a place": {
			in:   `["x` + uEsc("200b") + `Bearer ` + escapeToken + uEsc("200b") + `Bearer ` + secondToken + `"]`,
			want: `["xBearer [REDACTED] [REDACTED]"]`,
		},
		"JSON text, the places written as escapes": {
			in:   `{"output":"{\"b\":\"x\\` + "u200b" + `sk-lf-abc123\\` + "u200b" + `Bearer ` + escapeToken + `\"}"}`,
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

// The same on one line: each line break Line drops is a place.
func TestLineValueRedactsTheTokenOfAHeaderOnTheLineAfterACredentialFoundAfterADroppedLineBreak(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a word, a short key and Bearer on three lines": {
			in: `["word\nsk-lf-abc123\nBearer ` + escapeToken + `"]`, want: `["word[REDACTED] [REDACTED]"]`,
		},
		"a word, a short key and Authorization, tabs between": {
			in:   `["word\tsk-lf-abc123\tAuthorization: Basic ` + escapeToken + `"]`,
			want: `["word[REDACTED]: Basic [REDACTED]"]`,
		},
		"a word and two Bearer lines, CRLF between": {
			in:   `["word\r\nBearer ` + escapeToken + `\r\nBearer ` + secondToken + `"]`,
			want: `["wordBearer [REDACTED] [REDACTED]"]`,
		},
		"an object key": {
			in: `{"word\nsk-lf-abc123\nBearer ` + escapeToken + `":"v"}`, want: `{"word[REDACTED] [REDACTED]":"v"}`,
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

// 1 MiB of a short key at a place and a header keyword at the next: processed
// in a time that grows with the size, every token replaced, no panic.
func TestRedactingAMegabyteOfKeysAndHeadersEachAtAPlaceEndsAndShowsNoToken(t *testing.T) {
	t.Parallel()
	const size = 1 << 20
	unit := "a" + bellChar + "sk-lf-k" + bellChar + "Bearer " + escapeToken + " "
	in := strings.Repeat(unit, size/len(unit))
	want := strings.Repeat("a[REDACTED] [REDACTED] ", size/len(unit))

	got := (sanitize.Redactor{}).Redact(in)

	if got != want {
		t.Fatalf("Redact of %d bytes ended %q, want the rule's answer ending %q", len(in), tail(got, 48), tail(want, 48))
	}
	if again := (sanitize.Redactor{}).Redact(got); again != got {
		t.Fatalf("Redact of that result ended %q, want it unchanged", tail(again, 48))
	}
}
