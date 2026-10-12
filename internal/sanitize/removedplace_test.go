package sanitize_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #210): sanitize.Redactor and sanitize.Payload, pure.
// A place where the cleaning removed a character (an invisible, bidirectional
// or control character, a whole terminal escape sequence) is a boundary for the
// Langfuse key rule: a key that directly follows it is redacted, whatever
// stands before the place. Expected texts are literals.

var (
	zeroWidthSpace = string(rune(0x200b))
	bidiOverride   = string(rune(0x202e))
)

const (
	bellChar   = "\x07"
	escapeChar = "\x1b"
)

// uEsc returns the six characters of a JSON escape of a code unit: a backslash,
// "u" and the four hexadecimal digits given. Test sources build the escape text
// from pieces so that no tool turns it into the character it names.
func uEsc(hex string) string { return `\` + "u" + hex }

func TestRedactReplacesAKeyThatDirectlyFollowsACharacterTheCleaningRemoved(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a zero-width space after a word":        {in: "token" + zeroWidthSpace + "sk-lf-abc123", want: "token[REDACTED]"},
		"a bell after a letter":                  {in: "x" + bellChar + "sk-lf-abc123", want: "x[REDACTED]"},
		"a NUL after a letter":                   {in: "x\x00pk-lf-abc123 end", want: "x[REDACTED] end"},
		"a bidi override after a digit":          {in: "v2" + bidiOverride + "pk-lf-abc123", want: "v2[REDACTED]"},
		"a colour sequence after a letter":       {in: "x" + escapeChar + "[31msk-lf-abc123", want: "x[REDACTED]"},
		"a hyperlink sequence after a letter":    {in: "x" + escapeChar + "]8;;https://e.example/a" + bellChar + "sk-lf-abc123", want: "x[REDACTED]"},
		"a one-character CSI after a letter":     {in: "x\u009b31msk-lf-abc123", want: "x[REDACTED]"},
		"an escape that starts no sequence":      {in: "x" + escapeChar + "sk-lf-abc123", want: "x[REDACTED]"},
		"a run of removed characters":            {in: "x" + zeroWidthSpace + bellChar + bidiOverride + "sk-lf-abc123", want: "x[REDACTED]"},
		"a word split by a hidden character":     {in: "ta" + zeroWidthSpace + "sk-lf-name", want: "ta[REDACTED]"},
		"two keys, each after a removed place":   {in: "a" + zeroWidthSpace + "sk-lf-x b" + bellChar + "pk-lf-y", want: "a[REDACTED] b[REDACTED]"},
		"a key after a place inside another key": {in: "sk-lf-abc" + zeroWidthSpace + "sk-lf-def", want: "[REDACTED]"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// A key that a removed place splits is found whole, as before, and a text where
// nothing is removed keeps the word boundary rule it had.
func TestRedactKeepsTheBoundaryRuleWhereNothingIsRemovedOrNoKeyFollowsThePlace(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a key split by a hidden character":          {in: "sk-lf-abc" + zeroWidthSpace + "def-ghi end", want: "[REDACTED] end"},
		"a key split by a colour sequence":           {in: "sk-lf-abc" + escapeChar + "[31mdef end", want: "[REDACTED] end"},
		"a key glued to a letter, nothing removed":   {in: "xsk-lf-abc123", want: "xsk-lf-abc123"},
		"a word that holds the prefix":               {in: "a task-lf-name", want: "a task-lf-name"},
		"a place before a word that holds a prefix":  {in: "x" + zeroWidthSpace + "task-lf-name", want: "x" + zeroWidthSpace + "task-lf-name"},
		"a place before a prefix with no key chars":  {in: "x" + zeroWidthSpace + "sk-lf- y", want: "x" + zeroWidthSpace + "sk-lf- y"},
		"a place in a text with no key":              {in: "x" + zeroWidthSpace + "y", want: "x" + zeroWidthSpace + "y"},
		"a key after a blank next to a removed char": {in: "x " + zeroWidthSpace + "sk-lf-abc", want: "x [REDACTED]"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

func TestRedactReplacesTheConfiguredPairAfterARemovedPlaceByItsValue(t *testing.T) {
	t.Parallel()
	r := sanitize.NewRedactor("pk-lf-0000-configured", "sk-lf-1111-configured")
	in := "a" + zeroWidthSpace + "sk-lf-1111-configured b" + bellChar + "pk-lf-0000-configured"
	assertRedacts(t, r, in, "a[REDACTED] b[REDACTED]")
}

// The same places in a payload: a string value and an object key. The result is
// compact JSON; the hidden characters are gone and the key with them.
func TestPayloadRedactsAKeyThatDirectlyFollowsACharacterTheCleaningRemoved(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a value, a zero-width space": {
			in: `{"a":"token` + zeroWidthSpace + `sk-lf-abc123"}`, want: `{"a":"token[REDACTED]"}`,
		},
		"a value, the escape of a zero-width space in the payload": {
			in: `{"a":"token` + uEsc("200b") + `sk-lf-abc123"}`, want: `{"a":"token[REDACTED]"}`,
		},
		"a value, a bell": {
			in: `{"a":"x` + uEsc("0007") + `sk-lf-abc123"}`, want: `{"a":"x[REDACTED]"}`,
		},
		"a value, a colour sequence": {
			in: `{"a":"x` + uEsc("001b") + `[31msk-lf-abc123"}`, want: `{"a":"x[REDACTED]"}`,
		},
		"an object key": {
			in: `{"x` + uEsc("200b") + `sk-lf-abc123":"v"}`, want: `{"x[REDACTED]":"v"}`,
		},
		"a key in a list": {
			in: `["a",` + `"x` + uEsc("202e") + `pk-lf-abc123"]`, want: `["a","x[REDACTED]"]`,
		},
		"a word that only holds the prefix, a place before it": {
			in: `{"a":"x` + uEsc("200b") + `task-lf-name"}`, want: `{"a":"xtask-lf-name"}`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertPayloadIs(t, tc.in, tc.want)
		})
	}
}

// 1 MiB of removed characters, each followed by a prefix with no key character
// or by a key: processed in a time that grows with the size, the answer the rule
// gives, no panic. TestRedactionTimeGrowsInProportionToTheInput holds the timing.
func TestRedactingAMegabyteOfRemovedPlacesEndsAndGivesTheRuleOfTheKey(t *testing.T) {
	t.Parallel()
	const size = 1 << 20
	tests := map[string]struct{ in, want string }{
		"removed characters then a key": {
			in:   strings.Repeat(zeroWidthSpace, size/3) + "sk-lf-abc123",
			want: "[REDACTED]",
		},
		"a word, a removed character and a prefix, repeated": {
			in:   strings.Repeat("a"+bellChar+"sk-lf- ", size/9),
			want: strings.Repeat("a"+bellChar+"sk-lf- ", size/9),
		},
		"a letter, a removed character and a key, repeated": {
			in:   strings.Repeat("a"+bellChar+"sk-lf-k ", size/9),
			want: strings.Repeat("a[REDACTED] ", size/9),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := (sanitize.Redactor{}).Redact(tc.in)
			if got != tc.want {
				t.Fatalf("Redact of %d bytes ended %q, want the rule's answer ending %q", len(tc.in), tail(got, 24), tail(tc.want, 24))
			}
			if again := (sanitize.Redactor{}).Redact(got); again != got {
				t.Fatalf("Redact of that result ended %q, want it unchanged", tail(again, 24))
			}
		})
	}
}

// FuzzRedactRemovedPlaces checks, for any text put around a removed character
// and a key, that redacting twice changes nothing and that no key stays after a
// removed place. Valid JSON stays valid JSON.
func FuzzRedactRemovedPlaces(f *testing.F) {
	f.Add("x", zeroWidthSpace, plantedSecretKey, "y")
	f.Add("tok", bellChar, plantedPublicKey, "")
	f.Add("a ", escapeChar+"[31m", "sk-lf-a", `"`)
	f.Add("{\"k\":\"v", "\x00", "pk-lf-b_c-d", `"}`)
	r := sanitize.NewRedactor("hunter2-hunter2")
	f.Fuzz(func(t *testing.T, before, removed, key, after string) {
		if !strings.HasPrefix(key, "sk-lf-") && !strings.HasPrefix(key, "pk-lf-") {
			return
		}
		in := before + removed + key + after
		once := r.Redact(in)
		if twice := r.Redact(once); twice != once {
			t.Fatalf("Redact(%q) = %q, but redacting it again gives %q", in, once, twice)
		}
		if json.Valid([]byte(in)) && !json.Valid([]byte(once)) {
			t.Fatalf("Redact(%q) = %q, which is not JSON", in, once)
		}
	})
}
