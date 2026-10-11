package sanitize_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam: the sanitize functions (#193). A terminal control sequence in
// Langfuse data (a colour, a cursor move, an OSC 8 hyperlink) is removed with
// all of its characters, not with its escape character alone; the scan for the
// end of a sequence is bounded; text that only looks like the rest of a
// sequence is never changed. Expected values are literals.

// sequenceCases are the texts Text, Message and Payload must clean the same
// way: a string in, the string out.
var sequenceCases = map[string]struct{ in, want string }{
	"a colour sequence":                  {"\x1b[31mred\x1b[0m text", "red text"},
	"a colour sequence with two params":  {"\x1b[1;32mgreen\x1b[m", "green"},
	"a cursor movement":                  {"a\x1b[2;5Hb\x1b[3A\x1b[2Kc", "abc"},
	"a private mode sequence":            {"\x1b[?25lhidden\x1b[?25h", "hidden"},
	"an intermediate byte":               {"x\x1b[2 qy", "xy"},
	"a quote as an intermediate byte":    {"x\x1b[61\"py", "xy"},
	"a backslash as a final byte":        {"x\x1b[1\\y", "xy"},
	"an OSC 8 hyperlink ended by BEL":    {"\x1b]8;;https://evil.example/pay\x07click here\x1b]8;;\x07", "click here"},
	"an OSC ended by ESC and backslash":  {"\x1b]0;window title\x1b\\text", "text"},
	"an OSC 8 hyperlink with parameters": {"\x1b]8;id=1;https://evil.example/a?b=c&d=e\x1b\\link\x1b]8;;\x1b\\", "link"},
	"the single-character CSI":           {"\u009b31mred\u009b0m", "red"},
	"the single-character CSI, cursor":   {"a\u009b2;5Hb", "ab"},
	"a sequence at the very end":         {"done\x1b[0m", "done"},
	"a sequence alone":                   {"\x1b[31m", ""},
	"two sequences in a row":             {"\x1b[1m\x1b[31mred", "red"},
	"non-ASCII text around a sequence":   {"olá \x1b[31mmundo\x1b[0m ✓", "olá mundo ✓"},
	"non-ASCII text in an OSC":           {"\x1b]0;título ✓\x07ok", "ok"},
}

// remainderCases are the texts whose escape character starts no complete
// sequence: the escape character goes alone, as before, and the rest stays.
var remainderCases = map[string]struct{ in, want string }{
	"an unterminated CSI":                      {"a\x1b[31", "a[31"},
	"a CSI cut by a letter that is not a byte": {"a\x1b[3é1m", "a[3é1m"},
	"a CSI cut by a line feed":                 {"a\x1b[3\n1mb", "a[3\n1mb"},
	"a parameter after an intermediate byte":   {"a\x1b[ 1mb", "a[ 1mb"},
	"an unterminated OSC":                      {"x\x1b]8;;http://u no terminator", "x]8;;http://u no terminator"},
	"an OSC cut by a line feed":                {"x\x1b]0;t\nrest\x07", "x]0;t\nrest"},
	"an OSC cut by another escape":             {"\x1b]0;t\x1b[31mred", "]0;tred"},
	"an escape and a letter":                   {"a\x1bcb", "acb"},
	"an escape alone":                          {"a\x1b", "a"},
	"an escape before a space":                 {"a\x1b b", "a b"},
	"an escape before an invisible character":  {"\x1b\u200b[31m", "[31m"},
	"the single-character CSI cut at the end":  {"a\u009b31", "a31"},
	"the single-character CSI before a quote":  {"a\u009b61\"pb", "a61\"pb"},
	"the single-character CSI before a slash":  {"a\u009b1\\b", "a1\\b"},
}

// bareCases are the texts with no escape character: they are never changed.
var bareCases = []string{
	"[31m", "[31mred[0m", "]8;;https://example.com", "[2;5H", "plain text", "[1;32mgreen[m", "ESC[31m", "\\x1b[31m",
	`\u001b[31m`, "[?25l", "a [ b ] c",
}

func TestTextRemovesATerminalControlSequenceWhole(t *testing.T) {
	t.Parallel()
	for name, tc := range sequenceCases {
		if got := sanitize.Text(tc.in); got != tc.want {
			t.Errorf("%s: Text(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
	}
}

func TestMessageRemovesATerminalControlSequenceWhole(t *testing.T) {
	t.Parallel()
	for name, tc := range sequenceCases {
		if got := sanitize.Message(tc.in); got != tc.want {
			t.Errorf("%s: Message(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
	}
}

func TestLineRemovesATerminalControlSequenceWhole(t *testing.T) {
	t.Parallel()
	for name, tc := range sequenceCases {
		if got := sanitize.Line(tc.in); got != tc.want {
			t.Errorf("%s: Line(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
	}
}

func TestPayloadRemovesATerminalControlSequenceFromEveryValueAndKey(t *testing.T) {
	t.Parallel()
	for name, tc := range sequenceCases {
		in, _ := json.Marshal(map[string]any{tc.in: []any{tc.in, map[string]any{"k": tc.in}}})
		want, _ := json.Marshal(map[string]any{tc.want: []any{tc.want, map[string]any{"k": tc.want}}})

		got, err := sanitize.Payload(in, sanitize.Redactor{})

		if err != nil || string(got) != string(want) {
			t.Errorf("%s: Payload = %s (err %v), want %s", name, got, err, want)
		}
	}
}

func TestAnEscapeThatStartsNoCompleteSequenceIsRemovedAloneAndTheRestStays(t *testing.T) {
	t.Parallel()
	for name, tc := range remainderCases {
		if got := sanitize.Text(tc.in); got != tc.want {
			t.Errorf("%s: Text(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
	}
}

// An error message turns a control character into a space, as before: an
// escape that starts no complete sequence is one space, the rest stays.
func TestMessageTurnsAnEscapeThatStartsNoCompleteSequenceIntoASpace(t *testing.T) {
	t.Parallel()
	if got := sanitize.Message("a\x1b[31é b\x1b]8;;u"); got != "a [31é b ]8;;u" {
		t.Fatalf("Message = %q, want %q", got, "a [31é b ]8;;u")
	}
}

func TestTextThatLooksLikeTheRestOfASequenceWithNoEscapeBeforeItIsNeverChanged(t *testing.T) {
	t.Parallel()
	for _, in := range bareCases {
		if got := sanitize.Text(in); got != in {
			t.Errorf("Text(%q) = %q, want it unchanged", in, got)
		}
		if got := sanitize.Message(in); got != in {
			t.Errorf("Message(%q) = %q, want it unchanged", in, got)
		}
		payload, _ := json.Marshal(map[string]string{in: in})
		if got, err := sanitize.Payload(payload, sanitize.Redactor{}); err != nil || string(got) != string(payload) {
			t.Errorf("Payload(%s) = %s (err %v), want it unchanged", payload, got, err)
		}
	}
}

// A sequence split across two JSON strings is two texts: each is cleaned
// alone, so the first string's escape character cannot take the second one's
// characters.
func TestASequenceSplitAcrossTwoJSONStringsIsCleanedStringByString(t *testing.T) {
	t.Parallel()
	got, err := sanitize.Payload(json.RawMessage(`["\u001b[3","1mred",{"a\u001b":"[31m"}]`), sanitize.Redactor{})

	if err != nil || string(got) != `["[3","1mred",{"a":"[31m"}]` {
		t.Fatalf("Payload = %s (err %v), want [\"[3\",\"1mred\",{\"a\":\"[31m\"}]", got, err)
	}
}

// The scan for the end of a sequence reads at most MaxEscapeSequenceRunes
// characters from the escape character: a sequence of exactly that many is
// removed whole, one more and the escape character goes alone, so a payload
// cannot make the sanitizer eat the text that follows an unterminated sequence.
func TestTheScanForTheEndOfASequenceIsBoundedByTheFixedLength(t *testing.T) {
	t.Parallel()
	// The docs state this length (256 characters) in five places.
	const limit = 256
	if sanitize.MaxEscapeSequenceRunes != limit {
		t.Fatalf("MaxEscapeSequenceRunes = %d, want %d: the docs say so", sanitize.MaxEscapeSequenceRunes, limit)
	}
	osc := func(body int) string { return "\x1b]" + strings.Repeat("a", body) + "\x07" }  // 2 + body + 1 characters
	csi := func(params int) string { return "\x1b[" + strings.Repeat("1", params) + "m" } // 2 + params + 1 characters
	st := func(body int) string { return "\x1b]" + strings.Repeat("a", body) + "\x1b\\" } // 2 + body + 2 characters
	tests := map[string]struct{ in, want string }{
		"an OSC of exactly the limit":     {osc(limit-3) + "tail", "tail"},
		"an OSC one character longer":     {osc(limit-2) + "tail", "]" + strings.Repeat("a", limit-2) + "tail"},
		"a CSI of exactly the limit":      {csi(limit-3) + "tail", "tail"},
		"a CSI one character longer":      {csi(limit-2) + "tail", "[" + strings.Repeat("1", limit-2) + "mtail"},
		"an OSC ended by ST at the limit": {st(limit-4) + "tail", "tail"},
		"an OSC ended by ST one over":     {st(limit-3) + "tail", "]" + strings.Repeat("a", limit-3) + "\\tail"},
	}
	for name, tc := range tests {
		if got := sanitize.Text(tc.in); got != tc.want {
			t.Errorf("%s: Text = %.40q (%d bytes), want %.40q (%d bytes)", name, got, len(got), tc.want, len(tc.want))
		}
	}
}

func TestAnUnterminatedSequenceNeverTakesTheTextThatFollowsIt(t *testing.T) {
	t.Parallel()
	follows := "é" + strings.Repeat("keep this text. ", 500) // far longer than the limit; é ends a CSI as no byte of one
	for name, in := range map[string]string{
		"OSC":                 "\x1b]8;;http://u" + follows,
		"CSI":                 "\x1b[1;2;3" + follows,
		"CSI, one character":  "\u009b1;2;3" + follows,
		"many escapes in row": strings.Repeat("\x1b]", 300) + follows,
	} {
		got := sanitize.Text(in)

		if !strings.HasSuffix(got, follows) || strings.ContainsAny(got, "\x1b\u009b") {
			t.Errorf("%s: Text lost text that follows the unterminated sequence, or kept an escape: %.60q", name, got)
		}
	}
}

// The removal runs before redaction: a key or a token that an escape sequence
// hides behind, or splits, is found whole.
func TestARedactorFindsASecretAnEscapeSequenceSplitsOrHidesBehind(t *testing.T) {
	t.Parallel()
	r := sanitize.NewRedactor("hunter2-hunter2")
	tests := map[string]string{
		"a Langfuse key split by a colour sequence":   "key sk-lf-abc\x1b[31mdef-123456 end",
		"a Langfuse key split by an OSC 8 hyperlink":  "key pk-lf-abc\x1b]8;;https://x.example\x07def-123456 end",
		"a configured secret split by a sequence":     "value hunter2\x1b[1m-hunter2 end",
		"a configured secret split by the one-char":   "value hunter2\u009b1m-hunter2 end",
		"a bearer token behind a sequence":            "Authorization: Bearer \x1b[0mabcdefghijklmnopqrstuvwxyz end",
		"a bearer token split by a cursor movement":   "Bearer abcdefgh\x1b[2;5Hijklmnopqrstuv end",
		"a header keyword split by a sequence":        "Author\x1b[0mization: Bearer abcdefghijklmnopqrstuvwxyz",
		"an escape inside a key, in a payload string": `{"x":"sk-lf-ab\u001b[31mcdef-123456"}`,
	}
	for name, in := range tests {
		var got string
		if strings.HasPrefix(in, "{") {
			out, err := sanitize.Payload(json.RawMessage(in), r)
			if err != nil {
				t.Fatal(err)
			}
			got = string(out)
		} else {
			got = r.Redact(in)
		}
		for _, leaked := range []string{"def-123456", "hunter2", "abcdefghijklmnopqrstuvwxyz", "ijklmnopqrstuv", "cdef-123456", "\x1b", "\u009b"} {
			if strings.Contains(got, leaked) {
				t.Errorf("%s: %q still holds %q", name, got, leaked)
			}
		}
		if !strings.Contains(got, sanitize.Redacted) {
			t.Errorf("%s: %q holds no %s", name, got, sanitize.Redacted)
		}
	}
}

// A raw single-character CSI inside the JSON text of a result must never
// take a quote or a backslash with it: cleaning that text, with Text or in a
// Redact that finds a secret, keeps a valid JSON text valid.
func TestASingleCharacterCSINeverTakesTheQuoteOrBackslashOfAJSONText(t *testing.T) {
	t.Parallel()
	const planted = `"sk-lf-abcdef-123456"`
	tests := map[string]struct{ in, want string }{
		"before a closing quote":     {"{\"k\":\"x\u009b\"}", `{"k":"x"}`},
		"before a JSON escape":       {"{\"k\":\"x\u009b1\\n\"}", `{"k":"x1\n"}`},
		"before a quote and a brace": {"{\"k\":\"x\u009b1\",\"j\":\"y\"}", `{"k":"x1","j":"y"}`},
		"before a final byte":        {"{\"k\":\"x\u009b1mbc\"}", `{"k":"xbc"}`},
	}
	for name, tc := range tests {
		if got := sanitize.Text(tc.in); got != tc.want || !json.Valid([]byte(got)) {
			t.Errorf("%s: Text(%q) = %q, want the valid JSON %q", name, tc.in, got, tc.want)
		}
		withSecret := strings.TrimSuffix(tc.in, "}") + `,"s":` + planted + "}"
		want := strings.TrimSuffix(tc.want, "}") + `,"s":"` + sanitize.Redacted + `"}`
		if got := (sanitize.Redactor{}).Redact(withSecret); got != want || !json.Valid([]byte(got)) {
			t.Errorf("%s: Redact(%q) = %q, want the valid JSON %q", name, withSecret, got, want)
		}
	}
}

// FuzzText checks the invariants of Text whatever it is given: valid UTF-8,
// no control character other than tab, line feed and carriage return and no
// hidden character left (the escape character and U+009B included); cleaning
// twice changes nothing; a text with nothing to clean is returned as it was;
// and the removal is bounded: no more than the hidden characters and, for each
// escape character, MaxEscapeSequenceRunes.
func FuzzText(f *testing.F) {
	for _, tc := range sequenceCases {
		f.Add(tc.in)
	}
	for _, tc := range remainderCases {
		f.Add(tc.in)
	}
	f.Add("\x1b]" + strings.Repeat("a", 300) + "\x07tail")
	f.Add("\x1b[1;2;3" + strings.Repeat("x", 300))
	f.Add("\xff\x1b[31m\xfe\u009b")
	f.Add("{\"k\":\"x\u009b\"}")
	f.Fuzz(func(t *testing.T, s string) {
		got := sanitize.Text(s)
		if !utf8.ValidString(got) {
			t.Fatalf("Text(%q) = %q, not UTF-8", s, got)
		}
		for _, r := range got {
			if strippedByText(r) {
				t.Fatalf("Text(%q) = %q keeps the hidden character %U", s, got, r)
			}
		}
		if again := sanitize.Text(got); again != got {
			t.Fatalf("Text(%q) = %q, but cleaning it again gives %q", s, got, again)
		}
		valid, stripped, introducers := strings.ToValidUTF8(s, "\ufffd"), 0, 0
		for _, r := range valid {
			if r == '\x1b' || r == '\u009b' {
				introducers++
			}
			if strippedByText(r) {
				stripped++
			}
		}
		if stripped == 0 && got != valid {
			t.Fatalf("Text(%q) = %q, want a text with nothing to strip returned as it was", s, got)
		}
		removed := utf8.RuneCountInString(valid) - utf8.RuneCountInString(got)
		if removed > stripped+introducers*sanitize.MaxEscapeSequenceRunes {
			t.Fatalf("Text(%q) removed %d characters, more than %d stripped ones and %d escapes of %d characters allow",
				s, removed, stripped, introducers, sanitize.MaxEscapeSequenceRunes)
		}
	})
}

// strippedByText reports whether Text drops r on its own: a control character
// other than tab, line feed and carriage return, or a hidden one.
func strippedByText(r rune) bool {
	return (unicode.IsControl(r) && !strings.ContainsRune("\t\n\r", r)) || unicode.Is(unicode.Cf, r) ||
		(r >= 0xE0000 && r <= 0xE007F) ||
		unicode.In(r, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}
