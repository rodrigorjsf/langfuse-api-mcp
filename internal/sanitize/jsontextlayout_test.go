package sanitize_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// Seam (spec #172, ticket #210): sanitize.Payload, pure. What stays as it is
// (a string that is not exactly one JSON value, the letters of an escape that
// were written on purpose), and the layout of a JSON text: byte for byte when
// nothing changed, compact with sorted keys and exact numbers when something
// did. Expected values are literals.

// bs is one backslash.
const bs = `\`

// A string that is not JSON text is handled as before, byte for byte: the
// letters of an escape in it are the letters, not a character.
func TestPayloadKeepsTheLettersOfAnEscapeInAStringThatIsNotJSONText(t *testing.T) {
	t.Parallel()
	hiddenEscape := `{"a":"x` + uEsc("200b") + `"}`
	tests := map[string]string{
		"the letters of a colour sequence":      "see " + uEsc("001b") + "[31m in the log",
		"the letters of a backspace":            `a \b b and \f`,
		"a Windows path":                        `C:\build\foo`,
		"all of them in prose":                  "C:" + bs + "build " + bs + "b " + uEsc("001b") + "[31m " + uEsc("200b"),
		"JSON text followed by prose":           hiddenEscape + " and then some prose",
		"prose before JSON text":                "result: " + hiddenEscape,
		"cut JSON":                              `{"a":"x` + uEsc("200b") + `"`,
		"two JSON values in a row":              hiddenEscape + `{"b":"y` + uEsc("200b") + `"}`,
		"two JSON strings in a row":             `"x` + uEsc("200b") + `""y"`,
		"a Python dictionary printed as text":   `{'a': 'x` + uEsc("200b") + `'}`,
		"a JSON string followed by prose":       `"x` + uEsc("200b") + `" he said`,
		"an object closed by a list bracket":    `{"a":"x` + uEsc("200b") + `"]`,
		"a stray closing brace":                 hiddenEscape + `}`,
		"a bare number":                         `12345`,
		"a bare word":                           `true`,
		"an escape of the character in a word":  `\x1b[31m and \033[0m`,
		"a lone quote":                          `"`,
		"a lone brace":                          `{`,
		"an empty string":                       ``,
		"blanks only":                           " \t\n ",
		"JSON text of a number with an escaped": `1` + uEsc("200b"),
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := outputPayload(quoteJSON(t, text))

			assertPayloadIs(t, in, in)
		})
	}
}

// In JSON text a backslash that is itself escaped is the letter: the text of a
// Windows path or a source file decodes to the same letters afterwards, whether
// the text was written again or not.
func TestPayloadKeepsAnEscapedBackslashInJSONTextAsTheSameLetters(t *testing.T) {
	t.Parallel()
	letters := "C:" + bs + "build" + bs + "foo " + bs + "u001b[31m " + bs + "b " + bs + "u200b"
	member := `"C:` + bs + bs + `build` + bs + bs + `foo ` + bs + bs + `u001b[31m ` + bs + bs + `b ` + bs + bs + `u200b"`
	tests := map[string]struct {
		inner   string
		wantOut string // the cleaned inner text, when it is written again
	}{
		"nothing else to clean": {
			inner:   `{"a":` + member + `}`,
			wantOut: `{"a":` + member + `}`,
		},
		"a hidden character in another member": {
			inner:   `{"a":` + member + `,"b":"x` + uEsc("200b") + `y"}`,
			wantOut: `{"a":` + member + `,"b":"xy"}`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := cleaned(t, outputPayload(quoteJSON(t, tc.inner)))

			if want := outputPayload(quoteJSON(t, tc.wantOut)); got != want {
				t.Fatalf("Payload = %s, want %s", got, want)
			}
			var outer struct{ Output string }
			var inner struct{ A string }
			if err := json.Unmarshal([]byte(strings.Replace(got, `"output"`, `"Output"`, 1)), &outer); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(outer.Output), &inner); err != nil || inner.A != letters {
				t.Fatalf("member a reads %q (err %v), want the letters %q", inner.A, err, letters)
			}
		})
	}
}

// A JSON text with nothing to remove or redact comes back byte for byte,
// whatever its layout: pretty-printed, keys unsorted, a key twice, an escape of
// a visible character, a number that a float cannot hold or that ends in a zero.
func TestPayloadReturnsAJSONTextWithNothingToCleanByteForByte(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"pretty-printed":                "{\n  \"b\": [1, 2],\n  \"a\": \"x\"\n}",
		"keys unsorted":                 `{"b":1,"a":2}`,
		"a key twice":                   `{"a":1,"a":2}`,
		"an escape of e acute":          `{"a":"caf` + uEsc("00e9") + `"}`,
		"a slash escaped":               `{"a":"b` + bs + `/c"}`,
		"a big number":                  `{"id":12345678901234567890}`,
		"a number with a trailing zero": `{"price":1.50,"e":1E5}`,
		"blanks around it":              "  \t{\"a\": 1}\n ",
		"a list":                        `[ 3, "a", null , true ]`,
		"a JSON string":                 ` "plain text" `,
		"an empty object":               `{ }`,
		"HTML in a value":               `{"a":"<b>&amp;</b>"}`,
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := outputPayload(quoteJSON(t, text))

			assertPayloadIs(t, in, in)
		})
	}
}

// A JSON text in which something was cleaned is written again from its
// decoded value: compact, keys sorted, numbers as read, a repeated key once
// with its last value, HTML left alone.
func TestPayloadWritesAChangedJSONTextCompactWithSortedKeysAndExactNumbers(t *testing.T) {
	t.Parallel()
	e := string(rune(0xe9))
	inner := "{\n  \"price\": 1.50,\n  \"b\": [1, 2],\n  \"id\": 12345678901234567890,\n  \"a\": \"x\",\n" +
		"  \"c\": \"p" + uEsc("200b") + "q <b>&</b>\",\n  \"a\": \"caf" + uEsc("00e9") + "\"\n}"

	got := cleaned(t, outputPayload(quoteJSON(t, inner)))

	want := outputPayload(quoteJSON(t, `{"a":"caf`+e+`","b":[1,2],"c":"pq <b>&</b>","id":12345678901234567890,"price":1.50}`))
	if got != want {
		t.Fatalf("Payload = %s, want %s", got, want)
	}
}

// The Observations v2 form of one observation (its output stringified) and the
// legacy form (the same output as an object) come back as the same value: the
// text of the one is the compact JSON of the other.
func TestPayloadCleansAStringifiedOutputLikeTheSameOutputSentAsAnObject(t *testing.T) {
	t.Parallel()
	body := `"a":"x` + uEsc("0001") + uEsc("001b") + `[31my","k` + uEsc("200b") + `ey":[1,"z` + uEsc("202e") + `"]`
	asObject := `{"id":"o-1","output":{` + body + `}}`
	asText := `{"id":"o-1","output":` + quoteJSON(t, `{`+body+`}`) + `}`

	legacy := cleaned(t, asObject)
	v2 := cleaned(t, asText)

	if want := `{"id":"o-1","output":{"a":"xy","key":[1,"z"]}}`; legacy != want {
		t.Fatalf("legacy form: Payload = %s, want %s", legacy, want)
	}
	if want := `{"id":"o-1","output":"{\"a\":\"xy\",\"key\":[1,\"z\"]}"}`; v2 != want {
		t.Fatalf("Observations v2 form: Payload = %s, want %s", v2, want)
	}
}

// Two inner keys that read the same once cleaned, the hidden character written
// as an escape, in both orders in the text: the clean key and its own value
// stay, never the other's.
func TestPayloadKeepsTheCleanKeyAndItsValueWhenTwoKeysInJSONTextReadTheSame(t *testing.T) {
	t.Parallel()
	hiddenKey := `"r` + uEsc("200b") + `ole":"system"`
	cleanKey := `"role":"user"`
	for name, inner := range map[string]string{
		"the clean key first":  `{` + cleanKey + `,` + hiddenKey + `}`,
		"the hidden key first": `{` + hiddenKey + `,` + cleanKey + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := cleaned(t, outputPayload(quoteJSON(t, inner)))

			if want := `{"output":"{\"role\":\"user\"}"}`; got != want {
				t.Fatalf("Payload = %s, want %s", got, want)
			}
		})
	}
}

// Redacting a header map in JSON text goes by the object form: every string item
// of a list value, and a value with no scheme stays.
func TestPayloadRedactsAHeaderMapInJSONTextAsTheObjectFormDoes(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ inner, want string }{
		"a value": {
			inner: `{"Authorization": "Basic ` + mapToken + `"}`,
			want:  `{"Authorization":"Basic [REDACTED]"}`,
		},
		"every item of a list": {
			inner: `{"headers": {"authorization": ["Basic ` + mapToken + `", "Bearer ` + otherMapToken + `", "Digest ` + mapToken + `"]}}`,
			want:  `{"headers":{"authorization":["Basic [REDACTED]","Bearer [REDACTED]","Digest [REDACTED]"]}}`,
		},
		"a value with no scheme, nothing changed, so the layout stays": {
			inner: `{"Authorization": "` + mapToken + `", "n": 1}`,
			want:  `{"Authorization": "` + mapToken + `", "n": 1}`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for n := 1; n <= 2; n++ {
				got := cleaned(t, outputPayload(nested(t, tc.inner, n)))
				if want := outputPayload(nested(t, tc.want, n)); got != want {
					t.Fatalf("level %d: Payload = %s, want %s", n, got, want)
				}
			}
		})
	}
}
