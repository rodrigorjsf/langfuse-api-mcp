package sanitize_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #211): sanitize.Redactor and sanitize.Payload, pure.
// A Langfuse key as Langfuse generates it (the prefix and a UUID) is redacted
// wherever it stands, glued to a word or not; any other key keeps the rules it
// had. Expected texts are literals: the text around a key, built by
// concatenation, is never run through the pattern to get the answer.

const (
	// generatedKeyBody is a UUID, the shape Langfuse generates a key in.
	generatedKeyBody = "0a1b2c3d-1111-2222-3333-444455556666"
	// generatedKeyBodyUpper is another UUID, in upper case.
	generatedKeyBodyUpper = "0A1B2C3D-AAAA-BBBB-CCCC-DDDDEEEEFFFF"
)

func TestRedactReplacesAGeneratedKeyWhereverItStands(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"glued to a letter":                  {in: "xsk-lf-" + generatedKeyBody, want: "x[REDACTED]"},
		"glued to a digit":                   {in: "1pk-lf-" + generatedKeyBody, want: "1[REDACTED]"},
		"glued to an underscore":             {in: "KEY_sk-lf-" + generatedKeyBody, want: "KEY_[REDACTED]"},
		"glued to the name of the variable":  {in: "LANGFUSE_SECRET_KEY_sk-lf-" + generatedKeyBody, want: "LANGFUSE_SECRET_KEY_[REDACTED]"},
		"glued to a word that ends in sk":    {in: "task-lf-" + generatedKeyBody, want: "ta[REDACTED]"},
		"glued to the letter of the prefix":  {in: "ssk-lf-" + generatedKeyBody, want: "s[REDACTED]"},
		"a UUID in upper case":               {in: "xsk-lf-" + generatedKeyBodyUpper, want: "x[REDACTED]"},
		"a public key":                       {in: "Xpk-lf-" + generatedKeyBody, want: "X[REDACTED]"},
		"key characters after the UUID":      {in: "xsk-lf-" + generatedKeyBody + "-more_chars", want: "x[REDACTED]"},
		"key characters then a blank":        {in: "id=xsk-lf-" + generatedKeyBody + "Zz9 next", want: "id=x[REDACTED] next"},
		"two keys, each glued":               {in: "xsk-lf-" + generatedKeyBody + " 9pk-lf-" + generatedKeyBodyUpper, want: "x[REDACTED] 9[REDACTED]"},
		"a key at a word boundary, as ever":  {in: "key: sk-lf-" + generatedKeyBody + " end", want: "key: [REDACTED] end"},
		"a glued key before a closing quote": {in: `"xsk-lf-` + generatedKeyBody + `"`, want: `"x[REDACTED]"`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// Anything that is not a generated key keeps the rules it had: a word that only
// holds the prefix is a word, and a short or odd key glued to a letter, digit or
// underscore stays a documented limit (a maintainer decision, #211).
func TestRedactKeepsWhatIsNotAGeneratedKeyAsItWas(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a word that holds the prefix":                   {in: "task-lf-name", want: "task-lf-name"},
		"a long slug after a word that ends in sk":       {in: "task-lf-normalize-all-inputs", want: "task-lf-normalize-all-inputs"},
		"a short key glued to a letter":                  {in: "xsk-lf-abc123", want: "xsk-lf-abc123"},
		"a short key glued to an underscore":             {in: "KEY_sk-lf-abc123", want: "KEY_sk-lf-abc123"},
		"a short key glued to a digit":                   {in: "1pk-lf-abc123", want: "1pk-lf-abc123"},
		"a UUID one digit short":                         {in: "xsk-lf-0a1b2c3d-1111-2222-3333-44445555666", want: "xsk-lf-0a1b2c3d-1111-2222-3333-44445555666"},
		"a UUID one digit short, then a hyphen":          {in: "xsk-lf-0a1b2c3d-1111-2222-3333-44445555666-x", want: "xsk-lf-0a1b2c3d-1111-2222-3333-44445555666-x"},
		"a UUID with a first group of nine":              {in: "xsk-lf-0a1b2c3d4-1111-2222-3333-444455556666", want: "xsk-lf-0a1b2c3d4-1111-2222-3333-444455556666"},
		"a UUID with a letter that is not hexadecimal":   {in: "xsk-lf-0a1b2c3g-1111-2222-3333-444455556666", want: "xsk-lf-0a1b2c3g-1111-2222-3333-444455556666"},
		"32 hexadecimal digits with no hyphen":           {in: "xsk-lf-0a1b2c3d111122223333444455556666", want: "xsk-lf-0a1b2c3d111122223333444455556666"},
		"a UUID with an underscore for a hyphen":         {in: "xsk-lf-0a1b2c3d_1111_2222_3333_444455556666", want: "xsk-lf-0a1b2c3d_1111_2222_3333_444455556666"},
		"the prefix alone, glued":                        {in: "xsk-lf- y", want: "xsk-lf- y"},
		"a UUID that is not after the prefix":            {in: "xsk-lf-x" + generatedKeyBody, want: "xsk-lf-x" + generatedKeyBody},
		"a UUID with no prefix":                          {in: "id " + generatedKeyBody, want: "id " + generatedKeyBody},
		"a key at a boundary, as before":                 {in: "sk-lf-abc123", want: "[REDACTED]"},
		"a short key after an escape, as before":         {in: `x\nsk-lf-abc123`, want: `x\n[REDACTED]`},
		"a key at a boundary, with a UUID, as before":    {in: "sk-lf-" + generatedKeyBody + "-tail", want: "[REDACTED]"},
		"a key after a blank next to a glued short key":  {in: "xsk-lf-abc sk-lf-def", want: "xsk-lf-abc [REDACTED]"},
		"a word and the prefix of a key with no content": {in: "ask-lf-", want: "ask-lf-"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// The same keys in a payload: a string value, an object key, a list item, JSON
// text in a string and a text with a hidden character next to the key.
func TestPayloadRedactsAGeneratedKeyWhereverItStands(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a value":                 {in: `{"a":"xsk-lf-` + generatedKeyBody + `"}`, want: `{"a":"x[REDACTED]"}`},
		"a value, upper case":     {in: `{"a":"KEY_pk-lf-` + generatedKeyBodyUpper + ` end"}`, want: `{"a":"KEY_[REDACTED] end"}`},
		"an object key":           {in: `{"1sk-lf-` + generatedKeyBody + `":"v"}`, want: `{"1[REDACTED]":"v"}`},
		"a list item":             {in: `["a","xpk-lf-` + generatedKeyBody + `"]`, want: `["a","x[REDACTED]"]`},
		"JSON text in a string":   {in: `{"output":"{\"k\":\"xsk-lf-` + generatedKeyBody + `\"}"}`, want: `{"output":"{\"k\":\"x[REDACTED]\"}"}`},
		"a key and a short key":   {in: `{"a":"xsk-lf-` + generatedKeyBody + ` ysk-lf-abc"}`, want: `{"a":"x[REDACTED] ysk-lf-abc"}`},
		"a short key stays":       {in: `{"a":"xsk-lf-abc123 task-lf-name"}`, want: `{"a":"xsk-lf-abc123 task-lf-name"}`},
		"a one digit short stays": {in: `{"a":"xsk-lf-0a1b2c3d-1111-2222-3333-44445555666"}`, want: `{"a":"xsk-lf-0a1b2c3d-1111-2222-3333-44445555666"}`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertPayloadIs(t, tc.in, tc.want)
		})
	}
}

// A generated key that a removed character splits is found whole, as before, and
// the configured pair is still replaced by its exact value.
func TestRedactFindsAGeneratedKeyThatAHiddenCharacterSplits(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"in the UUID":            {in: "xsk-lf-0a1b2c3d-1111-" + zeroWidthSpace + "2222-3333-444455556666", want: "x[REDACTED]"},
		"in the prefix":          {in: "xsk-" + bellChar + "lf-" + generatedKeyBody, want: "x[REDACTED]"},
		"before the key":         {in: "x" + zeroWidthSpace + "sk-lf-" + generatedKeyBody, want: "x[REDACTED]"},
		"after the UUID":         {in: "xsk-lf-" + generatedKeyBody + bidiOverride + "tail rest", want: "x[REDACTED] rest"},
		"the configured pair":    {in: "ysk-lf-1111-configured " + "xsk-lf-" + generatedKeyBody, want: "y[REDACTED] x[REDACTED]"},
		"a hidden-character run": {in: strings.Repeat(zeroWidthSpace, 3) + "xsk-lf-" + generatedKeyBody, want: "x[REDACTED]"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.NewRedactor("sk-lf-1111-configured"), tc.in, tc.want)
		})
	}
}

// FuzzRedactGeneratedKey checks, for any text put before and after a generated
// key, that no part of the key is left, that redacting twice changes nothing
// and that valid JSON stays valid JSON.
func FuzzRedactGeneratedKey(f *testing.F) {
	f.Add("x", "pk", "y")
	f.Add("KEY_", "sk", " tail")
	f.Add(`{"k":"1`, "pk", `"}`)
	f.Add(`x\n`, "sk", `\n`)
	f.Add("a"+zeroWidthSpace, "sk", bellChar+"b")
	f.Fuzz(func(t *testing.T, before, kind, after string) {
		if (kind != "pk" && kind != "sk") || strings.Contains(before+after, "0a1b2c3d") {
			return
		}
		in := before + kind + "-lf-" + generatedKeyBody + after
		if !strings.Contains(sanitize.Text(in), kind+"-lf-"+generatedKeyBody) {
			return // a terminal sequence before the key took the "p" or "s" for its final byte: there is no key to find
		}
		once := sanitize.Redactor{}.Redact(in)
		if strings.Contains(sanitize.Text(once), generatedKeyBody) {
			t.Fatalf("Redact(%q) = %q, which still holds the key once cleaned", in, once)
		}
		if twice := (sanitize.Redactor{}).Redact(once); twice != once {
			t.Fatalf("Redact(%q) = %q, but redacting it again gives %q", in, once, twice)
		}
		if json.Valid([]byte(in)) && !json.Valid([]byte(once)) {
			t.Fatalf("Redact(%q) = %q, which is not JSON", in, once)
		}
	})
}
