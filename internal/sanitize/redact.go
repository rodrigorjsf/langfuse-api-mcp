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

// wordEndedEscape is a JSON escape that ends in a word character, written as
// text: one or more backslashes, then b, f, n, r or t, or u and four
// hexadecimal digits (#208). The escapes of a quote, a backslash and a slash
// end in a non-word character and need none of this. In a string that holds
// JSON text (the Observations v2 input and output) a line feed is the two
// characters backslash and n, so a key after it has a word character before it
// and no word boundary.
const wordEndedEscape = `\\+(?:[bfnrt]|u[0-9A-Fa-f]{4})`

// keyPattern matches any Langfuse API key, the configured pair or not:
// pk-lf-… and sk-lf-…, at a word boundary (not a word that merely ends in "pk"
// or "sk") or directly after a wordEndedEscape. The first group is the escape,
// which stays: the text around the key keeps its meaning as JSON text. The
// rule reads the text only. It does not decode it and does not count
// backslashes, so a literal backslash and a letter before a key (C:\nsk-lf-…)
// are taken for an escape, an over-redaction accepted for the sake of a rule
// no payload can steer. The two alternatives never match the same key: a
// letter or digit before the key is a word character, which has no boundary.
var keyPattern = regexp.MustCompile(`(?:\b|(` + wordEndedEscape + `))[ps]k-lf-[A-Za-z0-9_-]+`)

// letterEscapeKeyPattern matches a Langfuse key directly after the JSON escape
// of an ASCII letter, digit or underscore, written as text: backslashes, then
// u00 and the two hexadecimal digits of 0-9, A-Z, _ or a-z (#210). Decoded, that
// escape is a word character glued to the key, which keyPattern does not take;
// the text, as keyPattern reads it, has the escape before the key and takes it.
// The group is the escape, which stays.
var letterEscapeKeyPattern = regexp.MustCompile(`(\\+u00(?i:3[0-9]|[46][1-9a-f]|5[0-9af]|7[0-9a]))[ps]k-lf-[A-Za-z0-9_-]+`)

// redactKeysAfterLetterEscapes returns s, the text of a JSON text before it is
// decoded, with every Langfuse key that directly follows the escape of an ASCII
// letter, digit or underscore replaced by Redacted, the escape kept. Decoding and
// writing the text again would turn that escape into the character and glue the
// key to it, where #208's rule on the text no longer reads an escape.
func redactKeysAfterLetterEscapes(s string) string {
	if !strings.Contains(s, "k-lf-") {
		return s
	}
	return letterEscapeKeyPattern.ReplaceAllString(s, "${1}"+Redacted)
}

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

// scheme is the word before the token of a credential: a letter, then letters,
// digits and _ . + -.
const scheme = "[A-Za-z][A-Za-z0-9_.+-]*"

// quote is a quote of a quoted name or value, written as itself or as a JSON
// escape (one or more backslashes before it): the JSON text of a header map
// that was itself put in a JSON string has its quotes escaped, once or more.
// The single quote is that of a Python dictionary printed as text.
const quote = `\\*["']`

// space is a run of blanks in the quoted form: spaces, tabs and line breaks, so
// a pretty-printed header map matches, and so does a name with a line break
// around it, which isAuthorizationName also drops. The plain form of #180
// keeps spaces and tabs only.
const space = `[ \t\r\n]*`

// quotedName encodes the name rule (isAuthorizationName) as it reads between
// quotes: "authorization", or any text that ends in it after a character that
// is not an ASCII letter or digit, blanks allowed around it. The text cannot
// hold a quote or a backslash, which end it. A test walks the same names
// through both.
const quotedName = space + `(?:[^"'\\]*[^A-Za-z0-9"'\\])?(?i:authorization)` + space

// headerPattern matches a credential sent in a header, in three forms, up to a
// token of minTokenLength characters or more. The first group is what stays,
// the scheme word with it; the token that follows is what Redact replaces:
//   - the plain form (#180), "Authorization: <scheme> ", the word at the start
//     of the text or after a character that is not an ASCII letter or digit;
//   - "Bearer ", the word at the start of a word;
//   - the quoted form (#196), a quoted name that counts, a colon and a quoted
//     value "<scheme> <token>", the value alone or the first item of a list:
//     the JSON text of a header map, escaped or not, or its Python form.
//
// The keywords ignore case; the token does not, because folding a character
// class would admit look-alike letters such as the Kelvin sign. The pattern
// sees one string at a time: the value of a JSON header map that is an object
// of a payload is the business of redactHeaderValue.
var headerPattern = regexp.MustCompile(
	`((?:^|[^A-Za-z0-9])(?i:authorization)[ \t]*:[ \t]*` + scheme + `[ \t]+` +
		`|\b(?i:bearer)[ \t]+` +
		`|` + quote + quotedName + quote + space + `:` + space + `(?:\[` + space + `)?` + quote + space + scheme + `[ \t]+)` +
		valueChar + `{` + strconv.Itoa(minTokenLength) + `,}`)

// headerValuePattern matches the value of a member of a header map: blanks, a
// scheme, blanks and a token of minTokenLength characters or more (#196). The
// first group is what stays.
var headerValuePattern = regexp.MustCompile(
	`^(` + space + scheme + `[ \t]+)` + valueChar + `{` + strconv.Itoa(minTokenLength) + `,}`)

// authorizationWord is the word the name rule looks for, in lower case.
const authorizationWord = "authorization"

// isAuthorizationName is the name rule (#196): whether name, with invisible
// and control characters stripped, surrounding blanks dropped and case
// ignored, is "authorization" or ends in it after a character that is not an
// ASCII letter or digit: Authorization, Proxy-Authorization,
// http.request.header.authorization, HTTP_AUTHORIZATION. A name that only
// holds the word (authorizationMode, reauthorization) does not count. The
// rule is compiled in: no payload or argument changes it. EqualFold is safe
// here: the word has no k or s, the only letters with a non-ASCII look-alike
// under Unicode folding.
func isAuthorizationName(name string) bool {
	name = strings.Trim(Text(name), " \t\r\n")
	start := len(name) - len(authorizationWord)
	if start < 0 || !strings.EqualFold(name[start:], authorizationWord) {
		return false
	}
	return start == 0 || !isASCIIAlnum(name[start-1])
}

// isASCIIAlnum reports whether c is an ASCII letter or digit.
func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// redactHeaderValue returns s, the value of a header map under an
// Authorization name, with the token of "<scheme> <token>" replaced by
// Redacted; a value of another shape comes back as it was.
func redactHeaderValue(s string) string {
	return headerValuePattern.ReplaceAllString(s, "${1}"+Redacted)
}

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

// Redact returns s with every secret of r, every Langfuse API key (at a word
// boundary or directly after a JSON escape written as text, keyPattern) and the
// token of every Authorization or Bearer header replaced by Redacted: a header
// written "Authorization: <scheme> <token>", "Bearer <token>", or as the
// quoted name and value of a header map (headerPattern). A secret
// split by an invisible or control character is found without it, and the
// characters Text drops are dropped from the result then; a text with no
// secret comes back as it was.
func (r Redactor) Redact(s string) string {
	clean, places := textPlaces(s)
	redacted := r.redact(redactKeysAfterPlaces(clean, places))
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
		s = keyPattern.ReplaceAllString(s, "${1}"+Redacted)
	}
	if !mentionsHeader(s) {
		return s
	}
	return headerPattern.ReplaceAllString(s, "${1}"+Redacted)
}

// keyPrefixLength is the length of "pk-lf-" and "sk-lf-".
const keyPrefixLength = len("sk-lf-")

// redactKeysAfterPlaces returns s, a clean text, with every Langfuse key that
// starts at one of places replaced by Redacted (#210). A place is where the
// cleaning removed a hidden or control character or a whole terminal control
// sequence: the key rule reads the clean text, where that place is gone and the
// word before it is glued to the key (token, a zero-width space and sk-lf-…
// read tokensk-lf-…, which has no word boundary), so a place is a boundary of
// its own, whatever stands before it. A key that keyPattern already finds, and
// one that a place splits, are the business of keyPattern and are left to it;
// so is a text with no place.
func redactKeysAfterPlaces(s string, places []int) string {
	if len(places) == 0 || !strings.Contains(s, "k-lf-") {
		return s
	}
	covered := keyPattern.FindAllStringIndex(s, -1)
	var out strings.Builder
	written := 0 // s[:written] is in out, a replaced key included
	for _, p := range places {
		if p < written || p+keyPrefixLength >= len(s) || !isKeyPrefix(s[p:p+keyPrefixLength]) {
			continue
		}
		// The first match that ends after p; p is inside it if it starts at or before p.
		i, _ := slices.BinarySearchFunc(covered, p+1, func(m []int, target int) int { return cmp.Compare(m[1], target) })
		if i < len(covered) && covered[i][0] <= p {
			continue // inside a key keyPattern replaces whole
		}
		n := keyCharsLength(s[p+keyPrefixLength:])
		if n == 0 {
			continue
		}
		out.WriteString(s[written:p])
		out.WriteString(Redacted)
		written = p + keyPrefixLength + n
	}
	if written == 0 {
		return s
	}
	out.WriteString(s[written:])
	return out.String()
}

// isKeyPrefix reports whether s is "pk-lf-" or "sk-lf-".
func isKeyPrefix(s string) bool { return s == "pk-lf-" || s == "sk-lf-" }

// keyCharsLength returns the length of the run of key characters
// (A-Za-z0-9_-) that s starts with.
func keyCharsLength(s string) int {
	n := 0
	for n < len(s) && (isASCIIAlnum(s[n]) || s[n] == '_' || s[n] == '-') {
		n++
	}
	return n
}

// mentionsHeader reports whether s holds "bearer" or "authorization" in any
// case: the cheap test that lets the common text skip headerPattern, which
// costs far more per byte than this scan (BenchmarkRedact).
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
