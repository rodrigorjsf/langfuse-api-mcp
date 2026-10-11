package sanitize

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxEscapeSequenceRunes is the longest terminal control sequence, in
// characters, that Text and Message remove whole: the introducer, the body and
// the terminator together (#193). The scan for the end of a sequence reads no
// further than this from the escape character, so a payload cannot make the
// sanitizer remove more than this many characters of text after one escape
// character; an escape character that starts no complete sequence within it is
// removed alone, as every control character is. It holds an OSC 8 hyperlink
// with a URL of about 240 characters.
const MaxEscapeSequenceRunes = 256

const (
	escape = '\x1b' // starts every sequence except the one-character CSI
	bell   = '\a'   // ends an OSC
	// The two bytes of U+009B, the one-character form of the CSI introducer.
	csi8First, csi8Second = 0xc2, 0x9b
)

// withoutEscapeSequences returns s, which must be valid UTF-8, without the
// terminal control sequences it holds (#193), so that the characters of a
// colour or a cursor movement do not stay as text after the escape character
// is dropped:
//   - CSI: ESC "[" or U+009B, then parameter bytes (0x30–0x3F), intermediate
//     bytes (0x20–0x2F, after the parameter bytes) and one final byte
//     (0x40–0x7E), except that U+009B takes neither a quote nor a backslash
//     (see csiLen);
//   - OSC: ESC "]", then any characters other than controls, up to BEL or ESC
//     "\" (an OSC 8 hyperlink is one, URL included).
//
// A sequence is complete only within MaxEscapeSequenceRunes characters. An
// escape character that starts no complete sequence stays in the result for
// the control-character rule of the caller to remove alone, and so does the
// text after it. Text with no escape character comes back as it is.
func withoutEscapeSequences(s string) string {
	i := nextIntroducer(s)
	if i < 0 {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	for ; i >= 0; i = nextIntroducer(s) {
		out.WriteString(s[:i])
		s = s[i:]
		n := escapeSequenceLen(s)
		if n == 0 {
			n = introducerLen(s)
			out.WriteString(s[:n])
		}
		s = s[n:]
	}
	out.WriteString(s)
	return out.String()
}

// nextIntroducer returns the index of the first escape character or U+009B in
// s, which must be valid UTF-8, or -1.
func nextIntroducer(s string) int {
	for i := range len(s) {
		if s[i] == escape || (s[i] == csi8First && i+1 < len(s) && s[i+1] == csi8Second) {
			return i
		}
	}
	return -1
}

// introducerLen is the length in bytes of the introducer s starts with.
func introducerLen(s string) int {
	if s[0] == escape {
		return 1
	}
	return 2
}

// escapeSequenceLen returns the length in bytes of the complete sequence s
// starts with, or 0 when s starts with an escape character that starts none.
func escapeSequenceLen(s string) int {
	switch {
	case s[0] == csi8First: // U+009B: two bytes, one character
		return csiLen(s, 2, 1)
	case len(s) > 1 && s[1] == '[': // ESC and "[": two bytes, two characters
		return csiLen(s, 2, 2)
	case len(s) > 1 && s[1] == ']':
		return oscLen(s)
	}
	return 0
}

// csiLen returns the length in bytes of the CSI that s starts with, whose
// introducer is bodyAt bytes and introRunes characters long, or 0 when no final
// byte ends it within MaxEscapeSequenceRunes. Every byte of a CSI is ASCII, so
// a character that is not ends the scan.
//
// The one-character form (U+009B) also ends the scan at a quote or a backslash,
// which ECMA-48 would take as an intermediate and a final byte: unlike the
// escape character, U+009B can stand raw in the JSON text of a result, and it
// must never take the quote that closes a string or the backslash of an escape
// with it, so that redacting that text keeps it valid JSON.
func csiLen(s string, bodyAt, introRunes int) int {
	jsonSafe := s[0] == csi8First
	parameters, runes := true, introRunes
	for i := bodyAt; i < len(s); i++ {
		if runes++; runes > MaxEscapeSequenceRunes {
			return 0
		}
		c := s[i]
		if jsonSafe && (c == '"' || c == '\\') {
			return 0
		}
		switch {
		case c >= 0x30 && c <= 0x3f && parameters: // parameter byte
		case c >= 0x20 && c <= 0x2f: // intermediate byte, after the parameter bytes
			parameters = false
		case c >= 0x40 && c <= 0x7e: // final byte
			return i + 1
		default:
			return 0
		}
	}
	return 0
}

// oscLen returns the length in bytes of the OSC s starts with, or 0 when BEL
// or ESC "\" does not end it within MaxEscapeSequenceRunes. A control
// character other than the terminator, a line break included, ends the scan:
// the OSC is not complete.
func oscLen(s string) int {
	used := 2
	for i := 2; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if used++; used > MaxEscapeSequenceRunes {
			return 0
		}
		switch {
		case r == bell:
			return i + size
		case r == escape:
			if i+1 < len(s) && s[i+1] == '\\' && used < MaxEscapeSequenceRunes {
				return i + 2
			}
			return 0
		case unicode.IsControl(r):
			return 0
		}
		i += size
	}
	return 0
}
