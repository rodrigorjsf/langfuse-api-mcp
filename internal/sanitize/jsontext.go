package sanitize

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
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

// cleanString returns s cleaned as a payload string is: Text drops the hidden
// and control characters, r redacts its secrets, and a string that holds JSON
// text has the text cleaned too while levels allows another decoding. The
// places where Text removed something go to the key and header rules
// (redactAfterPlaces); that pass runs before the text is decoded, because the
// offsets of a place mean nothing once the text is written again.
func (r Redactor) cleanString(s string, levels int) string {
	text, places := textPlaces(s)
	if places != nil { // nearly every string has none
		text = redactAfterPlaces(text, places, droppedBlanks{})
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
// it; any other string comes back as it is, apart from the keys that
// redactKeysAfterLetterEscapes redacts in a text that looks like JSON text.
// The value is cleaned as a payload
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
	f := func(s string) string { return r.cleanString(s, levels-1) }
	w := walker{f: f}
	cleaned := w.walk(v)
	if !w.changed && mayShadowSomething(text) {
		// A repeated key keeps its last value in v and drops the others, which
		// the text still holds; they are cleaned too, and if one changes the
		// text is written again from v, which no longer holds them.
		for _, member := range shadowedMembers(text) {
			shadowed := walker{f: f}
			shadowed.walk(member)
			w.changed = w.changed || shadowed.changed
		}
	}
	if !w.changed {
		return text
	}
	return jsonTextOf(cleaned, text)
}

// mayShadowSomething reports whether a value that a repeated key shadows could
// hold what the cleaning changes: the escape of a hidden or control character
// (a backslash then u, b or f) or a header keyword, authorization or bearer
// (mentionsHeader).
// Without either, reading the text again for repeated keys cannot find
// anything the walk did not, which is nearly every JSON text.
func mayShadowSomething(text string) bool {
	return strings.Contains(text, `\u`) || strings.Contains(text, `\b`) || strings.Contains(text, `\f`) || mentionsHeader(text)
}

// shadowedMembers returns the members of the JSON text text that decoding it
// into a map drops, each as an object of that one member: where an object
// repeats a key, the earlier values (decoding keeps the last). text must be
// valid JSON, as decodeJSONText found it, so the reader cannot fail and its
// depth is bounded by the decoder's.
func shadowedMembers(text string) []any {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var shadowed []any
	readValue(dec, &shadowed)
	return shadowed
}

// readValue reads one value of dec, token by token, and returns it with the
// members that a repeated key shadows added to shadowed.
func readValue(dec *json.Decoder, shadowed *[]any) any {
	token, err := dec.Token()
	if err != nil {
		return nil // not reached for valid JSON
	}
	delim, isDelim := token.(json.Delim)
	switch {
	case !isDelim:
		return token // a string, a number, a boolean or null
	case delim == '[':
		items := []any{}
		for dec.More() {
			items = append(items, readValue(dec, shadowed))
		}
		_, _ = dec.Token() // the closing bracket; the text is valid
		return items
	}
	members := map[string]any{}
	for dec.More() {
		keyToken, _ := dec.Token() // a key: the text is valid
		key, _ := keyToken.(string)
		value := readValue(dec, shadowed)
		if earlier, repeated := members[key]; repeated {
			*shadowed = append(*shadowed, map[string]any{key: earlier})
		}
		members[key] = value
	}
	_, _ = dec.Token() // the closing brace; the text is valid
	return members
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
