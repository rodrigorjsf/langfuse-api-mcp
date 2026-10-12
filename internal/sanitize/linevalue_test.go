package sanitize_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #211): sanitize.LineValue, the cleaning of a value
// shown on one line (the body summary of a confirmation), and the second
// reading of Line that finds where it dropped something, through the test export
// LinePlaces. A tab, a line feed or a carriage return that Line drops is a place
// like the characters Text removes: a Langfuse key or a header keyword directly
// after it is redacted whatever stands before it, and a credential the dropped
// character splits is found whole. Expected texts are literals.

// lineValueOf returns the JSON text of LineValue applied to the value of in,
// with a Redactor that holds the secrets given.
func lineValueOf(t *testing.T, in string, secrets ...string) string {
	t.Helper()
	v, err := sanitize.Decode(json.RawMessage(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(sanitize.LineValue(v, sanitize.NewRedactor(secrets...)))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestLineValueRedactsACredentialThatDirectlyFollowsACharacterLineDrops(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a short key after a line feed":       {in: `["word\nsk-lf-abc123"]`, want: `["word[REDACTED]"]`},
		"a short key after a tab":             {in: `["word\tpk-lf-abc123"]`, want: `["word[REDACTED]"]`},
		"a short key after a carriage return": {in: `["word\rsk-lf-abc123"]`, want: `["word[REDACTED]"]`},
		"a short key after a CRLF":            {in: `["word\r\nsk-lf-abc123"]`, want: `["word[REDACTED]"]`},
		"a generated key after a line feed":   {in: `["word\nsk-lf-` + generatedKeyBody + `"]`, want: `["word[REDACTED]"]`},
		"a key after a line feed and a tail":  {in: `["a\nsk-lf-abc123\n b"]`, want: `["a[REDACTED] b"]`},
		"Bearer after a line feed":            {in: `["word\nBearer ` + escapeToken + `"]`, want: `["wordBearer [REDACTED]"]`},
		"Authorization after a tab":           {in: `["word\tAuthorization: Basic ` + escapeToken + `"]`, want: `["wordAuthorization: Basic [REDACTED]"]`},
		"Bearer after a carriage return":      {in: `["word\rBEARER ` + escapeToken + `"]`, want: `["wordBEARER [REDACTED]"]`},
		"a word that holds the prefix":        {in: `["word\ntask-lf-name"]`, want: `["wordtask-lf-name"]`},
		"a word glued to the line feed":       {in: `["ta\nsk-lf-name"]`, want: `["ta[REDACTED]"]`},
		"an object key":                       {in: `{"word\nsk-lf-abc123":"v"}`, want: `{"word[REDACTED]":"v"}`},
		"an object value":                     {in: `{"k":"word\nsk-lf-abc123"}`, want: `{"k":"word[REDACTED]"}`},
		"a key and a header, each after one":  {in: `["a\nsk-lf-abc b\tBearer ` + escapeToken + `"]`, want: `["a[REDACTED] bBearer [REDACTED]"]`},
		"the configured key after a line feed": {
			in: `["word\nsk-lf-1111-configured"]`, want: `["word[REDACTED]"]`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := lineValueOf(t, tc.in, "sk-lf-1111-configured"); got != tc.want {
				t.Fatalf("LineValue(%s) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

// A credential the dropped character splits is found whole, as before: the line
// is cleaned first and read after.
func TestLineValueFindsACredentialThatADroppedCharacterSplits(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a key split by a line feed":          {in: `["sk-lf-abc\ndef-ghi end"]`, want: `["[REDACTED] end"]`},
		"a key split by a tab":                {in: `["pk-lf-abc\tdef end"]`, want: `["[REDACTED] end"]`},
		"a generated key split by a CRLF":     {in: `["xsk-lf-0a1b2c3d-1111-2222-\r\n3333-444455556666"]`, want: `["x[REDACTED]"]`},
		"a token split by a line feed":        {in: `["Bearer abcdefgh\nijklmnopqr"]`, want: `["Bearer [REDACTED]"]`},
		"the word split by a line feed":       {in: `["Bea\nrer ` + escapeToken + `"]`, want: `["Bearer [REDACTED]"]`},
		"a key split by a line feed and a ZW": {in: `["sk-lf-abc\n` + zeroWidthSpace + `def end"]`, want: `["[REDACTED] end"]`},
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

// Where Line drops nothing, the rules are those of the text: a key glued to a
// letter stays, and so does a keyword.
func TestLineValueKeepsTheRulesOfTheTextWhereNothingIsDropped(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a short key glued to a letter": {in: `["xsk-lf-abc123"]`, want: `["xsk-lf-abc123"]`},
		"a generated key glued":         {in: `["xsk-lf-` + generatedKeyBody + `"]`, want: `["x[REDACTED]"]`},
		"Bearer glued to a letter":      {in: `["xBearer ` + escapeToken + `"]`, want: `["xBearer ` + escapeToken + `"]`},
		"a line feed with no credential": {
			in: `["a\nb\tc"]`, want: `["abc"]`,
		},
		"a token of 15 characters after one": {in: `["a\nBearer abcdefghijklmno"]`, want: `["aBearer abcdefghijklmno"]`},
		"a text with no line break":          {in: `["plain text"]`, want: `["plain text"]`},
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

// The second reading of Line: its text is Line's, and each run of dropped
// characters, the blanks that are control characters included, leaves one place.
func TestLinePlacesReturnsTheTextOfLineAndOneOffsetPerRunOfDroppedCharacters(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in, want string
		places   []int
	}{
		"nothing dropped":                {in: "ab", want: "ab", places: nil},
		"a line feed":                    {in: "a\nb", want: "ab", places: []int{1}},
		"a tab":                          {in: "a\tb", want: "ab", places: []int{1}},
		"a carriage return":              {in: "a\rb", want: "ab", places: []int{1}},
		"a CRLF is one place":            {in: "a\r\nb", want: "ab", places: []int{1}},
		"a line feed and a hidden one":   {in: "a\n" + zeroWidthSpace + "b", want: "ab", places: []int{1}},
		"two runs":                       {in: "a\nb\tc", want: "abc", places: []int{1, 2}},
		"a line break at the start":      {in: "\nab", want: "ab", places: []int{0}},
		"a line break at the end":        {in: "ab\n", want: "ab", places: []int{2}},
		"a colour sequence and a break":  {in: "x" + escapeChar + "[31m\ny", want: "xy", places: []int{1}},
		"a character Text already drops": {in: "a" + bellChar + "b", want: "ab", places: []int{1}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, places := sanitize.LinePlaces(tc.in)

			if got != tc.want || !slices.Equal(places, tc.places) {
				t.Fatalf("LinePlaces(%q) = %q, %v; want %q, %v", tc.in, got, places, tc.want, tc.places)
			}
		})
	}
}

// FuzzLinePlaces checks, for any text, that the second reading and Line agree
// on the text, and that the places are in ascending order inside it.
func FuzzLinePlaces(f *testing.F) {
	f.Add("a\nb\t" + zeroWidthSpace + "c" + escapeChar + "[31md\r")
	f.Add("\xff\xfe" + bidiOverride + "x\n")
	f.Fuzz(func(t *testing.T, s string) {
		got, places := sanitize.LinePlaces(s)
		if want := sanitize.Line(s); got != want {
			t.Fatalf("LinePlaces(%q) text = %q, but Line gives %q", s, got, want)
		}
		for i, p := range places {
			if p < 0 || p > len(got) || (i > 0 && p <= places[i-1]) {
				t.Fatalf("LinePlaces(%q) places %v are not ascending offsets inside %q", s, places, got)
			}
		}
	})
}

// FuzzLineValue checks, for any text put around a break and a credential, that
// LineValue leaves no tab, line feed or carriage return in a string, and that
// passing its result through it again changes nothing.
func FuzzLineValue(f *testing.F) {
	f.Add("word", "\n", "sk-lf-0a1b2c3d-1111-2222-3333-444455556666", "")
	f.Add("a", "\r\n", "Bearer "+escapeToken, " Bearer "+escapeToken)
	f.Add("x", "\t", "Authorization: Basic "+escapeToken, "\nProxy-Authorization: Basic "+escapeToken)
	f.Add("tok", zeroWidthSpace+"\n", "pk-lf-abc", "hunter2-hunter2")
	r := sanitize.NewRedactor("hunter2-hunter2")
	f.Fuzz(func(t *testing.T, before, between, credential, after string) {
		in := before + between + credential + after
		once, ok := sanitize.LineValue(in, r).(string)
		if !ok {
			t.Fatalf("LineValue(%q) is not a string", in)
		}
		if strings.ContainsAny(once, "\t\n\r") {
			t.Fatalf("LineValue(%q) = %q, which holds a tab or a line break", in, once)
		}
		if twice := sanitize.LineValue(once, r); twice != once {
			t.Fatalf("LineValue(%q) = %q, but passing it through again gives %q", in, once, twice)
		}
	})
}
