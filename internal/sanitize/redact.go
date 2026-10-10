package sanitize

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Redacted replaces a secret wherever the server would otherwise show it.
const Redacted = "[REDACTED]"

// keyPattern matches any Langfuse API key, the configured pair or not:
// pk-lf-… and sk-lf-…, not a word that merely ends in "pk" or "sk".
var keyPattern = regexp.MustCompile(`\b[ps]k-lf-[A-Za-z0-9_-]+`)

// minTokenLength is the fewest characters a token must have for Redact to take
// it for a credential (#180). It must stay above the length of Redacted, so
// that redacting a redacted text changes nothing.
const minTokenLength = 16

// valueChar is one character of a credential token after a scheme: printable
// ASCII other than space, the quotes (", ', `) and the backslash. A token ends
// at white space, at a quote that closes the text around it and at the
// backslash of a JSON escape, so redacting the JSON text of a result keeps it
// valid JSON.
const valueChar = "[!#-&(-\\[\\]-_a-~]"

// headerPattern matches a credential sent in a header: the text from the start
// of "Authorization: <scheme> " or "Bearer " up to a token of minTokenLength
// characters or more. The first group is what stays, the scheme word with it;
// the token that follows is what Redact replaces. The keywords ignore case;
// the token does not, because folding a character class would admit
// look-alike letters such as the Kelvin sign. It sees one string at a time, so
// the value of a JSON header map under an Authorization key is taken only when
// it starts with Bearer (#196).
var headerPattern = regexp.MustCompile(
	`(\b(?i:authorization)[ \t]*:[ \t]*[A-Za-z][A-Za-z0-9_.+-]*[ \t]+|\b(?i:bearer)[ \t]+)` +
		valueChar + `{` + strconv.Itoa(minTokenLength) + `,}`)

// Redactor replaces secrets in text shown to the agent or written to a log.
// The zero Redactor still redacts every Langfuse API key.
type Redactor struct {
	secrets []string
}

// NewRedactor returns a Redactor for the given secret values, such as the key
// pair and the Authorization header built from it; empty values are ignored.
func NewRedactor(secrets ...string) Redactor {
	kept := slices.DeleteFunc(slices.Clone(secrets), func(s string) bool { return s == "" })
	// Longest first, so that a secret containing another is replaced whole.
	slices.SortFunc(kept, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return Redactor{secrets: kept}
}

// Redact returns s with every secret of r, every Langfuse API key and the
// token of every Authorization or Bearer header replaced by Redacted. A secret
// split by an invisible or control character is found without it, and the
// characters Text drops are dropped from the result then; a text with no
// secret comes back as it was.
func (r Redactor) Redact(s string) string {
	clean := Text(s)
	redacted := r.redact(clean)
	if clean == s || redacted != clean {
		return redacted
	}
	return s
}

// redact is Redact for a text that is already clean.
func (r Redactor) redact(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, Redacted)
	}
	if strings.Contains(s, "k-lf-") { // the common case skips the regular expression
		s = keyPattern.ReplaceAllString(s, Redacted)
	}
	if !mentionsHeader(s) {
		return s
	}
	return headerPattern.ReplaceAllString(s, "${1}"+Redacted)
}

// mentionsHeader reports whether s holds "bearer" or "authorization" in any
// case: the cheap test that lets the common text skip headerPattern, which
// costs about a hundred times more per byte than this scan (#180).
func mentionsHeader(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		// An ASCII letter in lower case, and the second letter of the word,
		// before the comparison of the whole word.
		switch {
		case s[i]|0x20 == 'b' && s[i+1]|0x20 == 'e' && startsWithFold(s[i:], "bearer"):
			return true
		case s[i]|0x20 == 'a' && s[i+1]|0x20 == 'u' && startsWithFold(s[i:], "authorization"):
			return true
		}
	}
	return false
}

// startsWithFold reports whether s starts with word, ignoring case.
func startsWithFold(s, word string) bool {
	return len(s) >= len(word) && strings.EqualFold(s[:len(word)], word)
}
