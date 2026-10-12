package sanitize_test

import (
	"slices"
	"testing"
	"unicode/utf8"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #210): the second reading of Text that finds where it
// removed something, through the test export RemovedPlaces. The text it returns
// is Text's, and each run of removed characters leaves one place: an offset in
// bytes of the result. Expected values are literals.

func TestRemovedPlacesReturnsTheTextOfTextAndOneOffsetPerRunOfRemovedCharacters(t *testing.T) {
	t.Parallel()
	eAcute, replacement := string(rune(0xe9)), string(rune(0xfffd))
	tests := map[string]struct {
		in, want string
		places   []int
	}{
		"nothing removed":             {in: "ab", want: "ab", places: nil},
		"a character in the middle":   {in: "a" + zeroWidthSpace + "b", want: "ab", places: []int{1}},
		"a character at the start":    {in: zeroWidthSpace + "ab", want: "ab", places: []int{0}},
		"a character at the end":      {in: "ab" + zeroWidthSpace, want: "ab", places: []int{2}},
		"a run is one place":          {in: "a" + zeroWidthSpace + bellChar + bidiOverride + "b", want: "ab", places: []int{1}},
		"two runs":                    {in: "a" + zeroWidthSpace + "b" + bellChar + "c", want: "abc", places: []int{1, 2}},
		"a colour sequence":           {in: "x" + escapeChar + "[31my", want: "xy", places: []int{1}},
		"a sequence and a character":  {in: "x" + escapeChar + "[31m" + zeroWidthSpace + "y", want: "xy", places: []int{1}},
		"an escape starting nothing":  {in: "x" + escapeChar + "y", want: "xy", places: []int{1}},
		"a NUL":                       {in: "x\x00y", want: "xy", places: []int{1}},
		"the offset counts bytes":     {in: eAcute + zeroWidthSpace + "x", want: eAcute + "x", places: []int{2}},
		"tab, line feed and return":   {in: "a\tb\nc\rd", want: "a\tb\nc\rd", places: nil},
		"invalid UTF-8 is replaced":   {in: "a\xffb", want: "a" + replacement + "b", places: nil},
		"invalid UTF-8 and a removal": {in: "a\xff" + zeroWidthSpace + "b", want: "a" + replacement + "b", places: []int{4}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, places := sanitize.RemovedPlaces(tc.in)

			if got != tc.want || !slices.Equal(places, tc.places) {
				t.Fatalf("RemovedPlaces(%q) = %q, %v; want %q, %v", tc.in, got, places, tc.want, tc.places)
			}
		})
	}
}

// FuzzRemovedPlaces checks, for any text, that the second reading and Text
// agree on the text, and that the places are in ascending order inside it.
func FuzzRemovedPlaces(f *testing.F) {
	f.Add("a" + zeroWidthSpace + "b" + escapeChar + "[31mc" + bellChar)
	f.Add("\xff\xfe" + bidiOverride + "x")
	f.Add(escapeChar + "]8;;u" + bellChar + "x" + escapeChar)
	f.Fuzz(func(t *testing.T, s string) {
		got, places := sanitize.RemovedPlaces(s)
		if want := sanitize.Text(s); got != want {
			t.Fatalf("RemovedPlaces(%q) text = %q, but Text gives %q", s, got, want)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("RemovedPlaces(%q) text %q is not valid UTF-8", s, got)
		}
		for i, p := range places {
			if p < 0 || p > len(got) || (i > 0 && p <= places[i-1]) {
				t.Fatalf("RemovedPlaces(%q) places %v are not ascending offsets inside %q", s, places, got)
			}
		}
		if s == "" && len(places) != 0 {
			t.Fatalf("an empty text has places %v", places)
		}
	})
}
