package sanitize

import (
	"strings"
	"unicode"
)

// MaxMessageRunes caps a tool error message (ADR-0008).
const MaxMessageRunes = 500

// truncatedMarker ends a message that was cut to fit.
const truncatedMarker = "… [truncated]"

// Message makes untrusted text (such as an error message Langfuse wrote) safe
// to show the agent inside a tool error: control characters become spaces,
// invisible and bidirectional formatting characters (zero-width, bidi
// overrides, tag characters U+E0000–E007F) are dropped, invalid UTF-8 is
// replaced, and the result is cut to MaxMessageRunes with a marker.
func Message(s string) string {
	clean := make([]rune, 0, min(len(s), 4*MaxMessageRunes))
	for _, r := range strings.ToValidUTF8(s, "�") {
		switch {
		case unicode.IsControl(r):
			clean = append(clean, ' ')
		case hidden(r):
			// format characters: zero-width, bidi controls, tag characters
		default:
			clean = append(clean, r)
		}
	}
	if len(clean) <= MaxMessageRunes {
		return string(clean)
	}
	marker := []rune(truncatedMarker)
	return string(clean[:MaxMessageRunes-len(marker)]) + truncatedMarker
}
