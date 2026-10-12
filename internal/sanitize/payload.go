package sanitize

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxJSONTextLevels is the number of levels of JSON text that Payload decodes
// below a payload string (#210): a string that holds JSON text is decoded and its
// strings are cleaned, a string among those that holds JSON text is decoded in
// turn, and so on for this many decodings. The Observations v2 input and output
// are one level; a tool call's arguments inside an output are two, and a JSON
// argument inside those three. A string that is reached after this many
// decodings is cleaned and redacted as plain text, so its escapes stay. Each
// level reads its string once more, so the work stays in proportion to the size
// of the payload. A constant of the binary: no payload, argument or client
// changes it.
const MaxJSONTextLevels = 4

// Payload cleans a Langfuse JSON payload before the agent sees them: in every
// string, keys included, invisible and bidirectional formatting characters are
// dropped, and so are control characters other than tab, line feed and
// carriage return, which are visible text; then r redacts its secrets.
// Invalid UTF-8 becomes U+FFFD. A string that is JSON text (see cleanJSONText)
// is decoded and cleaned the same way, down to MaxJSONTextLevels levels. The
// result is compact JSON with object keys in sorted order and numbers kept
// exact; payload must be valid JSON.
func Payload(payload json.RawMessage, r Redactor) (json.RawMessage, error) {
	v, err := Decode(payload)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false) // the payload is never rendered as HTML
	if err := enc.Encode(clean(v, r)); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// Decode decodes one JSON value, keeping numbers exact as json.Number: an
// ID-like 12345678901234567890 stays as is.
func Decode(raw json.RawMessage) (any, error) {
	_, v, err := decodeFirst(bytes.NewReader(raw))
	return v, err
}

// decodeFirst decodes the first JSON value of r, keeping numbers exact, and
// returns the decoder so that the caller can look at what follows.
func decodeFirst(r io.Reader) (*json.Decoder, any, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return dec, v, err
}

// clean returns v with every string cleaned and redacted by r (cleanString).
func clean(v any, r Redactor) any {
	w := walker{f: func(s string) string { return r.cleanString(s, MaxJSONTextLevels) }}
	return w.walk(v)
}

// cleanString returns s cleaned as a payload string is: Text drops the hidden
// and control characters, r redacts its secrets, and a string that holds JSON
// text has the text cleaned too while levels allows another decoding. The
// places where Text removed something go to the key rule (removedPlaces); that
// rule runs before the text is decoded, because the offsets of a place mean
// nothing once the text is written again.
func (r Redactor) cleanString(s string, levels int) string {
	text := Text(s)
	if text != s { // rare: the places are found by a second reading
		var places []int
		text, places = removedPlaces(s)
		text = redactKeysAfterPlaces(text, places)
	}
	// Nearly every string fails this test on its first byte; cleanJSONText
	// looks at the last one.
	if levels > 0 && len(text) > 1 && startsJSONText(text[0]) {
		text = r.cleanJSONText(text, levels)
	}
	return r.redact(text)
}

// startsJSONText reports whether c can be the first byte of JSON text: a
// brace, a bracket, a quote, or a blank before one.
func startsJSONText(c byte) bool { return c == '{' || c == '[' || c == '"' || isJSONBlank(c) }

// cleanJSONText returns text, which is clean, with the JSON text it holds
// cleaned (#210). A string is JSON text when it is exactly one valid JSON value
// that is an object, a list or a string, with nothing but JSON blanks around
// it; any other string comes back as it is. The value is cleaned as a payload
// is (every string and key, a string that is itself JSON text one decoding
// deeper), and text is written again only when that changed the value: then it
// is the cleaned value written as Payload writes a payload, compact with sorted
// keys, numbers exact and a repeated key once, except that U+2028 and U+2029
// stay characters (see jsonTextOf). Otherwise text comes back byte for byte.
// Text cannot tell the escape of a character from the same letters written on
// purpose, so it is decoded, not searched: a backslash that is itself escaped
// (C:\\build, or \\u001b[31m in a source file) decodes to the letters and
// stays, and the same text with one backslash less is the character.
func (r Redactor) cleanJSONText(text string, levels int) string {
	if !looksLikeJSONText(text) { // prose that starts with a brace or a quote: two bytes read
		return text
	}
	text = redactKeysAfterLetterEscapes(text)
	v, ok := decodeJSONText(text)
	if !ok {
		return text
	}
	w := walker{f: func(s string) string { return r.cleanString(s, levels-1) }}
	cleaned := w.walk(v)
	if !w.changed {
		return text
	}
	return jsonTextOf(cleaned, text)
}

// looksLikeJSONText reports whether text, which may have JSON blanks around
// it, starts with a brace, a bracket or a quote and ends with the one that
// closes it: the least JSON text has. It reads one byte of nearly every string
// and two of prose that merely starts with a brace or a quote.
func looksLikeJSONText(text string) bool {
	start := 0
	for start < len(text) && isJSONBlank(text[start]) {
		start++
	}
	if start == len(text) {
		return false
	}
	var closing byte
	switch text[start] {
	case '{':
		closing = '}'
	case '[':
		closing = ']'
	case '"':
		closing = '"'
	default:
		return false
	}
	end := len(text) - 1
	for end > start && isJSONBlank(text[end]) {
		end--
	}
	return end > start && text[end] == closing
}

// isJSONBlank reports whether c is a character JSON allows around a value.
func isJSONBlank(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// decodeJSONText decodes text, which looksLikeJSONText accepted, when it is
// JSON text: exactly one valid JSON value, and nothing but JSON blanks around
// it.
func decodeJSONText(text string) (any, bool) {
	dec, v, err := decodeFirst(strings.NewReader(text))
	if err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF { // a second value, or something that is no JSON
		return nil, false
	}
	return v, true
}

// jsonTextOf writes v, a decoded JSON value, the way Payload writes a payload
// (compact, keys sorted, numbers as read, HTML left alone), as the text of a
// string. U+2028 and U+2029 stay characters instead of becoming escapes: they
// are valid inside a JSON string, and writing each as six characters would make
// the text longer than the one it replaces, once more at every level of nesting
// (#210). If v cannot be written, which a decoded value always can, fallback
// comes back.
func jsonTextOf(v any, fallback string) string {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fallback
	}
	return string(withRawLineSeparators(bytes.TrimSuffix(out.Bytes(), []byte("\n"))))
}

// withRawLineSeparators returns b, JSON written by the standard encoder, with
// the escapes of U+2028 and U+2029 replaced by the characters. A backslash
// starts an escape pair; the pair is skipped whole, so the six characters of a
// backslash that is itself escaped, followed by u2028, stay as they are.
func withRawLineSeparators(b []byte) []byte {
	if !bytes.Contains(b, []byte(`\u202`)) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' || i+1 >= len(b) {
			out = append(out, b[i])
			continue
		}
		switch {
		case bytes.HasPrefix(b[i+1:], []byte(`u2028`)):
			out = append(out, "\u2028"...)
			i += 5
		case bytes.HasPrefix(b[i+1:], []byte(`u2029`)):
			out = append(out, "\u2029"...)
			i += 5
		default:
			out = append(out, b[i], b[i+1])
			i++
		}
	}
	return out
}

// redactHeaderMember returns value, the value of the member key of a payload's
// object, with the token of a header credential replaced by Redacted when key
// is an Authorization name: a string of the form "<scheme> <token>", or each
// such string item of a list (#196); and whether it changed anything. A value of
// another type, an item that is not a string and the key itself stay as they
// are.
func redactHeaderMember(key string, value any) (any, bool) {
	if !isAuthorizationName(key) {
		return value, false
	}
	changed := false
	switch v := value.(type) {
	case string:
		out := redactHeaderValue(v)
		return out, out != v
	case []any:
		for i, item := range v {
			if s, ok := item.(string); ok {
				if out := redactHeaderValue(s); out != s {
					v[i], changed = out, true
				}
			}
		}
	}
	return value, changed
}

// Line drops every control, invisible and bidirectional formatting character
// of s, tab and line breaks included, so a value shown on one line stays on
// it. Invalid UTF-8 becomes U+FFFD.
func Line(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, Text(s))
}

// LineValue returns v, a decoded JSON value, with every string, keys
// included, passed through Line, and the token of a header credential under an
// Authorization name replaced by Redacted, as Payload does (#196). Keys that
// read the same once cleaned keep one value by Payload's rule: the key that was
// already clean, else the first in byte order.
func LineValue(v any) any {
	w := walker{f: Line}
	return w.walk(v)
}

// walker passes the strings of a decoded JSON value through f, and remembers
// whether that, or the header credential rule, changed anything.
type walker struct {
	f       func(string) string
	changed bool // a string, a key or a header member came back different, or a key was dropped
}

// walk returns v, a decoded JSON value, with every string, keys included,
// passed through f; then the value of each object member is replaced by
// redactHeaderMember(key, value), key being the cleaned key (#196).
func (w *walker) walk(v any) any {
	switch v := v.(type) {
	case string:
		out := w.f(v)
		w.changed = w.changed || out != v
		return out
	case []any:
		for i, item := range v {
			v[i] = w.walk(item)
		}
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		// Keys that clean to the same key keep one value, deterministically:
		// the key that was already clean, else the first in byte order.
		for _, k := range slices.Sorted(maps.Keys(v)) {
			key := w.f(k)
			if key != k {
				w.changed = true // renamed, or dropped below
			}
			if _, taken := out[key]; taken && key != k {
				continue
			}
			value, redacted := redactHeaderMember(key, w.walk(v[k]))
			w.changed = w.changed || redacted
			out[key] = value
		}
		return out
	}
	return v // json.Number, bool, nil
}

// Text drops the characters of s that hide or reorder text: invisible and
// bidirectional formatting characters (hidden) and control characters other
// than tab, line feed and carriage return. A terminal control sequence (a
// colour, a cursor movement, an OSC 8 hyperlink: see withoutEscapeSequences)
// goes whole, not with its escape character alone; text that only looks like
// the rest of one, with no escape character before it, stays.
func Text(s string) string {
	if plain(s) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if removed(r) {
			return -1
		}
		return r
	}, withoutEscapeSequences(strings.ToValidUTF8(s, "�"), ""))
}

// removed reports whether Text drops r: a hidden character, or a control
// character other than tab, line feed and carriage return. ASCII is decided
// here, in a body small enough to be inlined into the loop that calls it;
// every other character goes to removedBeyondASCII.
func removed(r rune) bool {
	if r < utf8.RuneSelf {
		return r < ' ' && r != '\t' && r != '\n' && r != '\r' || r == 0x7f
	}
	return removedBeyondASCII(r)
}

// removedBeyondASCII is removed for a character that is not ASCII.
func removedBeyondASCII(r rune) bool {
	return hidden(r) || unicode.IsControl(r)
}

// sequenceMarker stands in for each terminal control sequence that
// textPlaces removes whole. It is a control character, so the pass that drops
// the other control characters drops it too, and leaves a place behind.
const sequenceMarker = "\x00"

// textPlaces is Text that also reports the places where it removed something
// (#210): each offset, in bytes of the result, is where a run of removed
// characters (a hidden or control character, or a whole terminal control
// sequence) stood, in ascending order. A Langfuse key that starts at a place
// is a key, whatever stands before the place (see redactKeysAfterPlaces). The
// result is Text(s); a text Text leaves as it is has no place.
func textPlaces(s string) (string, []int) {
	// Text comes back as s itself when it removes nothing, which is nearly every
	// text; only a text that lost something is read again, with the places
	// recorded.
	if Text(s) == s {
		return s, nil
	}
	return removedPlaces(s)
}

// removedPlaces is textPlaces for a text that Text changes: the second reading,
// which records where each run of removed characters stood.
func removedPlaces(s string) (string, []int) {
	var places []int
	size, inRun := 0, false
	text := strings.Map(func(r rune) rune {
		if removed(r) {
			if !inRun {
				places = append(places, size)
				inRun = true
			}
			return -1
		}
		inRun = false
		size += utf8.RuneLen(r)
		return r
	}, withoutEscapeSequences(strings.ToValidUTF8(s, "\ufffd"), sequenceMarker))
	return text, places
}

// plain reports whether s is ASCII with no control character other than tab,
// line feed and carriage return: text that Text leaves as it is, found by a
// byte scan that costs far less than Text's scan by rune on ASCII text
// (BenchmarkRedact; Redact cleans every text it is given).
func plain(s string) bool {
	for i := range len(s) {
		if c := s[i]; c >= 0x7f || (c < ' ' && c != '\t' && c != '\n' && c != '\r') {
			return false
		}
	}
	return true
}

// hidden reports whether r is an invisible or bidirectional formatting
// character: the Unicode format category Cf (zero-width characters, bidi
// overrides and isolates, the byte order mark), the whole tag block
// U+E0000–E007F, the variation selectors (U+FE00–FE0F, U+E0100–E01EF), which
// can encode any byte invisibly, and the other default-ignorable code points
// (Hangul fillers, the combining grapheme joiner), which render as nothing
// (#156).
func hidden(r rune) bool {
	return unicode.Is(unicode.Cf, r) || (r >= 0xE0000 && r <= 0xE007F) ||
		unicode.In(r, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}
