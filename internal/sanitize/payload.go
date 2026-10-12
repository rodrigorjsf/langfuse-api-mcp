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

// Payload cleans a Langfuse JSON payload before the agent sees it: in every
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
// included, passed through Line and redacted by r, and the token of a header
// credential under an Authorization name replaced by Redacted, as Payload does
// (#196). A Langfuse key or a header keyword that directly follows a character
// Line dropped, a line break included, is redacted as at a boundary, and one
// that a dropped character splits is found whole (#211). Keys that read the same
// once cleaned keep one value by Payload's rule: the key that was already
// clean, else the first in byte order.
func LineValue(v any, r Redactor) any {
	w := walker{f: r.cleanLine}
	return w.walk(v)
}

// cleanLine returns s as Line cleans it, redacted by r: the places where Line
// dropped something go to the key and header rules before the secrets are
// replaced, as cleanString does for Text.
func (r Redactor) cleanLine(s string) string {
	text, places := linePlaces(s)
	return r.redact(redactAfterPlaces(text, places))
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
	}, prepared(s, ""))
}

// replacement stands for each byte of invalid UTF-8.
const replacement = "\ufffd"

// prepared returns s as valid UTF-8 (invalid bytes become replacement) without
// the terminal control sequences it holds, each replaced by marker: the text
// Text, Message and removedPlaces then remove single characters from.
func prepared(s, marker string) string {
	return withoutEscapeSequences(strings.ToValidUTF8(s, replacement), marker)
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
// sequence) stood, in ascending order. A Langfuse key or the keyword of a header
// credential that starts at a place is one, whatever stands before the place
// (see redactAfterPlaces). The result is Text(s); a text Text leaves as it is
// has no place.
func textPlaces(s string) (string, []int) {
	// Text comes back as s itself when it removes nothing, which is nearly every
	// text; only a text that lost something is read again, with the places
	// recorded.
	if Text(s) == s {
		return s, nil
	}
	return removedPlaces(s, removed)
}

// linePlaces is Line that also reports the places where it dropped something
// (#211), as textPlaces does for Text: the characters Text removes, and the tab,
// line feed and carriage return Line drops besides. The result is Line(s).
func linePlaces(s string) (string, []int) {
	if Line(s) == s {
		return s, nil
	}
	return removedPlaces(s, droppedByLine)
}

// droppedByLine reports whether Line drops r: what Text removes, and the tab,
// line feed and carriage return besides.
func droppedByLine(r rune) bool {
	return r == '\t' || r == '\n' || r == '\r' || removed(r)
}

// removedPlaces is the second reading of a text that a cleaning changes: it
// returns the text without the characters dropped reports (removed for Text,
// droppedByLine for Line) and without its terminal control sequences, and where
// each run of them stood.
func removedPlaces(s string, dropped func(rune) bool) (string, []int) {
	var places []int
	size, inRun := 0, false
	text := strings.Map(func(r rune) rune {
		if dropped(r) {
			if !inRun {
				places = append(places, size)
				inRun = true
			}
			return -1
		}
		inRun = false
		size += utf8.RuneLen(r)
		return r
	}, prepared(s, sequenceMarker))
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
