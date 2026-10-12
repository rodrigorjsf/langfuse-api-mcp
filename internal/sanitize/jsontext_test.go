package sanitize_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #210): sanitize.Payload, pure. A payload string that
// is JSON text (the Observations v2 input and output) is decoded, cleaned and
// redacted as a payload is, at every level it decodes to, so no control or
// invisible character written as a JSON escape reaches the agent. Expected
// values are literals written by hand; the texts that go in are built with the
// helpers below, from pieces, so no tool turns an escape into the character it
// names.

// quoteJSON returns the JSON string literal of s, HTML left alone.
func quoteJSON(t *testing.T, s string) string {
	t.Helper()
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// nested returns text put in a JSON string literal n times: the value of a
// member that holds it needs n decodings to give text back.
func nested(t *testing.T, text string, n int) string {
	t.Helper()
	for range n {
		text = quoteJSON(t, text)
	}
	return text
}

// outputPayload wraps the JSON value literal into an object member "output", the
// place Observations v2 puts a stringified input or output.
func outputPayload(literal string) string { return `{"output":` + literal + `}` }

// cleaned returns what Payload gives for in, failing the test on an error.
func cleaned(t *testing.T, in string) string {
	t.Helper()
	got, err := sanitize.Payload(json.RawMessage(in), sanitize.Redactor{})
	if err != nil {
		t.Fatalf("Payload(%s) failed: %v", in, err)
	}
	return string(got)
}

// hiddenAsEscapes is every character the cleaning drops, written as the JSON
// escape a stringified input would hold, between x and y: the control
// characters ^A, ^G, backspace, form feed, delete and ESC (with the text of a
// colour sequence after it, which goes whole), the zero-width space, the bidi
// override, the byte order mark, a variation selector and a tag character as a
// surrogate pair.
func hiddenAsEscapes() string {
	return uEsc("0001") + uEsc("0007") + `\b\f` + uEsc("007f") + uEsc("001b") + `[31m` +
		uEsc("200b") + uEsc("202e") + uEsc("feff") + uEsc("fe0f") + uEsc("db40") + uEsc("dc41")
}

func TestPayloadRemovesEveryCharacterAStringHoldingJSONTextWritesAsAnEscape(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"all of them":                     hiddenAsEscapes(),
		"a control character, ^A":         uEsc("0001"),
		"a control character, ^G":         uEsc("0007"),
		"a backspace":                     `\b`,
		"a form feed":                     `\f`,
		"the delete character":            uEsc("007f"),
		"an escape character":             uEsc("001b"),
		"a zero-width space":              uEsc("200b"),
		"a bidi override":                 uEsc("202e"),
		"a byte order mark":               uEsc("feff"),
		"a variation selector":            uEsc("fe0f"),
		"a tag character, surrogate pair": uEsc("db40") + uEsc("dc41"),
		"a NUL":                           uEsc("0000"),
	}
	for name, escapes := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inner := `{"a":"x` + escapes + `y"}`

			got := cleaned(t, outputPayload(quoteJSON(t, inner)))

			if want := `{"output":"{\"a\":\"xy\"}"}`; got != want {
				t.Fatalf("Payload = %s, want %s", got, want)
			}
		})
	}
}

func TestPayloadKeepsTheTabLineFeedAndCarriageReturnAStringHoldingJSONTextWritesAsEscapes(t *testing.T) {
	t.Parallel()
	inner := `{"a":"x` + hiddenAsEscapes() + `\t\n\r` + uEsc("0009") + uEsc("000a") + uEsc("000d") + `y"}`

	got := cleaned(t, outputPayload(quoteJSON(t, inner)))

	if want := `{"output":"{\"a\":\"x\\t\\n\\r\\t\\n\\ry\"}"}`; got != want {
		t.Fatalf("Payload = %s, want %s", got, want)
	}
}

func TestPayloadRemovesTheCharactersOfAKeyOfAStringHoldingJSONText(t *testing.T) {
	t.Parallel()
	inner := `{"k` + uEsc("200b") + `ey":"v","o` + uEsc("001b") + `[31mther":[1,{"n` + `\b` + `ested":true}]}`

	got := cleaned(t, outputPayload(quoteJSON(t, inner)))

	if want := `{"output":"{\"key\":\"v\",\"other\":[1,{\"nested\":true}]}"}`; got != want {
		t.Fatalf("Payload = %s, want %s", got, want)
	}
}

// A payload string whose text is a JSON string (a quoted value) is JSON text
// too, and what it decodes to is cleaned.
func TestPayloadCleansAStringWhoseTextIsAJSONString(t *testing.T) {
	t.Parallel()
	inner := `"hi` + uEsc("001b") + `[31mthere` + uEsc("200b") + `!"`

	got := cleaned(t, outputPayload(quoteJSON(t, inner)))

	if want := `{"output":"\"hithere!\""}`; got != want {
		t.Fatalf("Payload = %s, want %s", got, want)
	}
}

// The same text stringified two, three and four times: each time one more level
// of backslashes. The worked example is the first level; deeper levels are read
// back, level by level, to the cleaned value.
func TestPayloadCleansAStringHoldingJSONTextStringifiedTwoThreeAndFourTimes(t *testing.T) {
	t.Parallel()
	inner := `{"a":"x` + hiddenAsEscapes() + `y","b` + uEsc("200b") + `":["z` + uEsc("0007") + `"]}`
	want := map[string]any{"a": "xy", "b": []any{"z"}}
	for n := 1; n <= 4; n++ {
		t.Run(strings.Repeat("quoted ", n), func(t *testing.T) {
			t.Parallel()
			got := cleaned(t, outputPayload(nested(t, inner, n)))

			var outer map[string]json.RawMessage
			if err := json.Unmarshal([]byte(got), &outer); err != nil {
				t.Fatalf("result is not JSON: %v", err)
			}
			if diff := compareJSON(unstringified(t, string(outer["output"]), n), want); diff != "" {
				t.Fatalf("level %d decodes to a different value: %s\nresult: %s", n, diff, got)
			}
		})
	}
}

// Levels one to four, written out: the escapes of the inner text have one, two,
// four and eight backslashes. The expected texts are literals, written by an
// encoder other than the one under test (Python's json.dumps, applied to the
// cleaned inner text once for each level).
func TestPayloadCleansAStringHoldingJSONTextAtLevelsOneToFourAndWritesItAgain(t *testing.T) {
	t.Parallel()
	inner := `{"a":"x` + hiddenAsEscapes() + `\t` + `y"}`
	want := []string{
		`{"output":"{\"a\":\"x\\ty\"}"}`,
		`{"output":"\"{\\\"a\\\":\\\"x\\\\ty\\\"}\""}`,
		`{"output":"\"\\\"{\\\\\\\"a\\\\\\\":\\\\\\\"x\\\\\\\\ty\\\\\\\"}\\\"\""}`,
		`{"output":"\"\\\"\\\\\\\"{\\\\\\\\\\\\\\\"a\\\\\\\\\\\\\\\":\\\\\\\\\\\\\\\"x\\\\\\\\\\\\\\\\ty\\\\\\\\\\\\\\\"}\\\\\\\"\\\"\""}`,
	}
	for n, wanted := range want {
		got := cleaned(t, outputPayload(nested(t, inner, n+1)))

		if got != wanted {
			t.Errorf("level %d: Payload = %s, want %s", n+1, got, wanted)
		}
	}
}
