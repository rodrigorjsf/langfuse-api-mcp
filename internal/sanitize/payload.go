package sanitize

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"unicode"
)

// Payload cleans a Langfuse JSON payload before the agent sees it: in every
// string, keys included, invisible and bidirectional formatting characters are
// dropped, and so are control characters other than tab, line feed and
// carriage return, which are visible text; then r redacts its secrets.
// Invalid UTF-8 becomes U+FFFD. The
// result is compact JSON with object keys in sorted order and numbers kept
// exact; payload must be valid JSON.
func Payload(payload json.RawMessage, r Redactor) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber() // keep numbers exact: an ID-like 12345678901234567890 stays as is
	var v any
	if err := dec.Decode(&v); err != nil {
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

// clean returns v with every string cleaned by Text and redacted by r.
func clean(v any, r Redactor) any {
	switch v := v.(type) {
	case string:
		return r.Redact(Text(v))
	case []any:
		for i, item := range v {
			v[i] = clean(item, r)
		}
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		// Keys that clean to the same key keep one value, deterministically:
		// the key that was already clean, else the first in byte order.
		for _, k := range slices.Sorted(maps.Keys(v)) {
			key := r.Redact(Text(k))
			if _, taken := out[key]; taken && key != k {
				continue
			}
			out[key] = clean(v[k], r)
		}
		return out
	}
	return v // json.Number, bool, nil
}

// Text drops the characters of s that hide or reorder text: invisible and
// bidirectional formatting characters (hidden) and control characters other
// than tab, line feed and carriage return.
func Text(s string) string {
	return strings.Map(func(r rune) rune {
		if hidden(r) || (unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r') {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
}

// hidden reports whether r is an invisible or bidirectional formatting
// character: the Unicode format category Cf (zero-width characters, bidi
// overrides and isolates, the byte order mark) and the whole tag block
// U+E0000–E007F.
func hidden(r rune) bool {
	return unicode.Is(unicode.Cf, r) || (r >= 0xE0000 && r <= 0xE007F)
}
