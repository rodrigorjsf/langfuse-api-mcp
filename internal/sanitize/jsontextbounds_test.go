package sanitize_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #210): sanitize.Payload and sanitize.Redactor, pure.
// The bound on the levels of JSON text, the length of a text written again, and
// the cost of hostile strings: no payload makes the sanitizer do more than work
// in proportion to its size, fail, or return more than twice what came in.
// Expected values are literals.

// The level bound is a constant of the binary: four decodings below a payload
// string. A hidden character written as an escape at levels 1 to 4 is removed;
// a string at level 5 comes back as it does today: its escapes stay, and it is
// redacted as plain text.
func TestPayloadDecodesFourLevelsOfJSONTextAndLeavesAFifthAsItIs(t *testing.T) {
	t.Parallel()
	if levels := sanitize.MaxJSONTextLevels; levels != 4 {
		t.Fatalf("MaxJSONTextLevels = %d, want 4", levels)
	}
	inner := `{"a":"x` + uEsc("200b") + `y","k":"sk-lf-` + keyHead + `"}`
	for n := 1; n <= 4; n++ {
		got := cleaned(t, outputPayload(nested(t, inner, n)))

		if want := outputPayload(nested(t, `{"a":"xy","k":"[REDACTED]"}`, n)); got != want {
			t.Fatalf("level %d: Payload = %s, want %s", n, got, want)
		}
	}

	got := cleaned(t, outputPayload(nested(t, inner, 5)))

	// Level 5: the escape stays, the key goes as plain text.
	if want := outputPayload(nested(t, `{"a":"x`+uEsc("200b")+`y","k":"[REDACTED]"}`, 5)); got != want {
		t.Fatalf("level 5: Payload = %s, want %s", got, want)
	}
}

// textAtLevel returns the text a payload's "output" member holds after n
// decodings of its literal as a JSON string.
func textAtLevel(t *testing.T, payload string, n int) string {
	t.Helper()
	var outer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &outer); err != nil {
		t.Fatalf("%.200s is not an object: %v", payload, err)
	}
	text := string(outer["output"])
	for range n {
		var inner string
		if err := json.Unmarshal([]byte(text), &inner); err != nil {
			t.Fatalf("%.200s is not a JSON string: %v", text, err)
		}
		text = inner
	}
	return text
}

// A JSON text that is written again is never longer than it was, apart from the
// redaction marker standing for a shorter secret: U+2028 and U+2029 stay
// characters at the inner levels instead of becoming six characters each, once
// more at every level of nesting. The whole result is never more than twice the
// payload, which is what the outer writer alone gives for these characters.
func TestPayloadNeverWritesAJSONTextLongerThanItWasAtLevelsOneToFour(t *testing.T) {
	t.Parallel()
	lineSep, paraSep := string(rune(0x2028)), string(rune(0x2029))
	forms := map[string]string{
		"raw separators":     strings.Repeat(lineSep+paraSep, 500),
		"escaped separators": strings.Repeat(uEsc("2028")+uEsc("2029"), 500),
		"a mix":              strings.Repeat(lineSep+uEsc("2029"), 500),
	}
	for form, separators := range forms {
		for n := 1; n <= 4; n++ {
			t.Run(form+" "+strings.Repeat("q", n), func(t *testing.T) {
				t.Parallel()
				inner := `{"a":"` + separators + uEsc("200b") + `"}`
				in := outputPayload(nested(t, inner, n))

				got := cleaned(t, in)

				for k := 1; k <= n; k++ {
					before, after := textAtLevel(t, in, k), textAtLevel(t, got, k)
					if len(after) > len(before) {
						t.Errorf("the text at decoding %d grew from %d to %d bytes", k, len(before), len(after))
					}
				}
				if len(got) > 2*len(in) {
					t.Errorf("the result is %d bytes for a payload of %d, more than twice", len(got), len(in))
				}
				// One pass was enough: the second changes nothing.
				assertPayloadIs(t, in, got)
			})
		}
	}
}

// A repeated U+2028 in a JSON text keeps its escape-free form only inside the
// text: the outer writer still writes it as an escape, as it always did, and the
// text decodes to the same characters.
func TestPayloadKeepsTheLineSeparatorsOfAJSONTextAsCharacters(t *testing.T) {
	t.Parallel()
	lineSep := string(rune(0x2028))
	inner := `{"a":"p` + lineSep + `q` + uEsc("2029") + `r` + uEsc("200b") + `"}`

	got := cleaned(t, outputPayload(quoteJSON(t, inner)))

	var outer struct{ Output string }
	if err := json.Unmarshal([]byte(strings.Replace(got, `"output"`, `"Output"`, 1)), &outer); err != nil {
		t.Fatal(err)
	}
	var a struct{ A string }
	if err := json.Unmarshal([]byte(outer.Output), &a); err != nil || a.A != "p"+lineSep+"q"+string(rune(0x2029))+"r" {
		t.Fatalf("the text decodes to %q (err %v)", a.A, err)
	}
	if strings.Contains(outer.Output, uEsc("2028")) {
		t.Fatalf("the inner text writes U+2028 as an escape: %q", outer.Output)
	}
}

// hostileStrings are the strings of the dangerous-parameter cases, each built
// from a size n: the texts that make a decoder, a scanner or a rewriter do
// quadratic work if one of them does. The first ones are plain text, the others
// JSON text of one or more levels.
func hostileStrings(t *testing.T) map[string]func(n int) string {
	t.Helper()
	return map[string]func(n int) string{
		"backslashes":              func(n int) string { return strings.Repeat(bs, 8*n) },
		"escape sequence starters": func(n int) string { return strings.Repeat(uEsc("001b")+"[", n) },
		"unicode escapes with too few hex digits": func(n int) string {
			return strings.Repeat(bs+"u12 ", 2*n)
		},
		"brackets that no decoder accepts": func(n int) string { return strings.Repeat("[", 8*n) },
		"brackets closed, deeper than a decoder accepts": func(n int) string {
			return strings.Repeat("[", 3*n) + strings.Repeat("]", 3*n)
		},
		"a list of short strings, each with an escape": func(n int) string {
			return `[` + strings.Repeat(`"x`+uEsc("200b")+`",`, 2*n) + `"x"]`
		},
		"escape sequence starters in a JSON text": func(n int) string {
			return `{"a":"` + strings.Repeat(uEsc("001b")+"[", n) + `"}`
		},
		"line separators in a JSON text, four levels": func(n int) string {
			return nested(t, `{"a":"`+strings.Repeat(string(rune(0x2028)), 2*n)+uEsc("200b")+`"}`, 3)
		},
		"keys that a place hides": func(n int) string {
			return `{"a":"` + strings.Repeat("x"+uEsc("200b")+"sk-lf-k ", n) + `"}`
		},
		"JSON text in JSON text in a list": func(n int) string {
			return `[` + strings.Repeat(quoteJSON(t, `{"b":"x`+uEsc("200b")+`"}`)+`,`, n) + `1]`
		},
		// The credentials of #211: a key prefix repeated, a glued prefix followed by
		// the start of a UUID that never completes, the keywords of a header after
		// an escape that is none, after an escape and after a removed character.
		// The regexp engine of Go reads a text of a few kilobytes with its
		// backtracker and a larger one with its slower automaton, the size where it
		// switches depending on the size of the pattern. Both inputs of a pair in
		// assertTimeInProportion must be on the automaton's side, or the pair would
		// compare two engines, so the smaller one is built above that size.
		"key prefixes": func(n int) string { return strings.Repeat("sk-lf-", 8*n) },
		"glued prefixes followed by a UUID that never completes": func(n int) string {
			return strings.Repeat("xsk-lf-0a1b2c3d-1111-", n)
		},
		"glued prefixes followed by a UUID that completes": func(n int) string {
			return strings.Repeat("xsk-lf-"+generatedKeyBody+" ", n)
		},
		"Bearer after a written line feed": func(n int) string { return strings.Repeat(`\nBearer `, 2*n) },
		"Bearer and a token after a written line feed": func(n int) string {
			return strings.Repeat(`\nBearer `+escapeToken+" ", n)
		},
		"Bearer after a unicode escape of too few digits": func(n int) string {
			return strings.Repeat(bs+"u12Bearer ", 2*n)
		},
		"Authorization after a written line feed": func(n int) string {
			return strings.Repeat(`\nAuthorization: Basic `+escapeToken+" ", n)
		},
		"header keywords that a place hides": func(n int) string {
			return strings.Repeat("x"+zeroWidthSpace+"Bearer "+escapeToken+" y"+bellChar+"Authorization: Basic ", n)
		},
		"a token that holds the next keyword": func(n int) string {
			return strings.Repeat("Bearer "+escapeToken+zeroWidthSpace, n)
		},
		// The reach of tokenAt after a place is bounded: a run of token characters
		// with a place at every other byte, and names that put the keyword out of reach.
		"token characters, a place at every other byte": func(n int) string {
			return "Bearer " + strings.Repeat("a"+zeroWidthSpace, 4*n)
		},
		"names that put the keyword out of reach of a place": func(n int) string {
			return strings.Repeat(strings.Repeat("a-", 40)+zeroWidthSpace+"authorization ", n)
		},
	}
}

// 1 MiB of each hostile string, as a payload string (JSON text of one level at
// the outermost) and through the redactor: processed without a panic or a
// failed call, and a result no longer than twice the payload.
func TestPayloadAndRedactEndOnAMegabyteOfHostileTextAtNoMoreThanTwiceItsSize(t *testing.T) {
	t.Parallel()
	for name, build := range hostileStrings(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text := build(1 << 17)
			for n := 1 << 16; len(text) > 1<<20; n /= 2 { // a megabyte, not several
				text = build(n)
			}
			in := outputPayload(quoteJSON(t, text))
			asLevels := outputPayload(nested(t, text, 2))

			for _, payload := range []string{in, asLevels} {
				got, err := sanitize.Payload(json.RawMessage(payload), sanitize.Redactor{})

				if err != nil {
					t.Fatalf("Payload of %d bytes failed: %v", len(payload), err)
				}
				if len(got) > 2*len(payload) {
					t.Fatalf("Payload of %d bytes gave %d, more than twice", len(payload), len(got))
				}
				if again, err := sanitize.Payload(got, sanitize.Redactor{}); err != nil || string(again) != string(got) {
					t.Fatalf("a second Payload changed the result of %d bytes (err %v)", len(got), err)
				}
			}
			redacted := (sanitize.Redactor{}).Redact(text)
			if len(redacted) > 2*len(text) {
				t.Fatalf("Redact of %d bytes gave %d, more than twice", len(text), len(redacted))
			}
			if again := (sanitize.Redactor{}).Redact(redacted); again != redacted {
				t.Fatalf("a second Redact changed the result of %d bytes", len(redacted))
			}
		})
	}
}

// assertTimeInProportion fails the test unless run takes time in proportion to
// the size of its input. The input is built small and eight times larger; the
// best of each over the rounds is compared, and the rounds stop at the first
// that shows the proportion (see TestRedactionTimeGrowsInProportionToTheInput
// for why).
func assertTimeInProportion(t *testing.T, name string, build func(n int) string, run func(string)) {
	t.Helper()
	const (
		smallCount = 250
		largeCount = 2000 // eight times the small input
		rounds     = 8
		bound      = 3 // times the time of the same bytes in small inputs
	)
	elapsed := func(f func()) time.Duration {
		start := time.Now()
		f()
		return time.Since(start)
	}
	small, large := build(smallCount), build(largeCount)
	timeSmall, timeLarge := time.Duration(1<<63-1), time.Duration(1<<63-1)
	for range rounds {
		timeSmall = min(timeSmall, elapsed(func() {
			for range largeCount / smallCount {
				run(small)
			}
		}))
		timeLarge = min(timeLarge, elapsed(func() { run(large) }))
		if timeLarge <= bound*timeSmall {
			return
		}
	}
	t.Errorf("%s: %d inputs of %d bytes took %v, one of %d bytes took %v: more than linear",
		name, largeCount/smallCount, len(small), timeSmall, len(large), timeLarge)
}

// The time of Payload grows in proportion to the size of every hostile string.
func TestPayloadTimeGrowsInProportionToTheSizeOfHostileText(t *testing.T) {
	// Not parallel: the other tests would share the processor with the timing.
	for name, build := range hostileStrings(t) {
		assertTimeInProportion(t, name, func(n int) string { return outputPayload(quoteJSON(t, build(n))) }, func(payload string) {
			if _, err := sanitize.Payload(json.RawMessage(payload), sanitize.Redactor{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// So does the time of Redact, which reads the same strings as text.
func TestRedactTimeGrowsInProportionToTheSizeOfHostileText(t *testing.T) {
	for name, build := range hostileStrings(t) {
		assertTimeInProportion(t, name, build, func(text string) { sanitize.Redactor{}.Redact(text) })
	}
}

// Idempotence and validity, for the cases of this ticket: a Payload of a Payload
// is byte-identical, a whole result is valid JSON, a JSON text stays valid JSON
// text at every level it had.
func TestPayloadOfAPayloadWithJSONTextIsByteIdenticalAndStaysValid(t *testing.T) {
	t.Parallel()
	inners := []string{
		`{"a":"x` + hiddenAsEscapes() + `y","k` + uEsc("200b") + `":["sk-lf-` + keyHead + uEsc("200b") + keyTail + `",1.50]}`,
		`["token` + uEsc("200b") + `sk-lf-abc123",{"Authorization":"Basic ` + mapToken + `"}]`,
		`"quoted ` + uEsc("001b") + `[31mtext"`,
		`{"role":"user","r` + uEsc("200b") + `ole":"system"}`,
	}
	for _, inner := range inners {
		for n := 1; n <= 5; n++ {
			in := outputPayload(nested(t, inner, n))

			got := cleaned(t, in)

			if !json.Valid([]byte(got)) {
				t.Fatalf("level %d: the result is not JSON: %s", n, got)
			}
			assertPayloadIs(t, in, got)
			if holdsAKey(got) {
				t.Fatalf("level %d: the result still holds a key: %s", n, got)
			}
			// The text at every decoding is still JSON: the string literals above
			// the innermost text, and that text.
			for k := 1; k <= n; k++ {
				if text := textAtLevel(t, got, k); !json.Valid([]byte(text)) {
					t.Fatalf("level %d: the text after %d decodings is no JSON: %.200s", n, k, text)
				}
			}
		}
	}
}
