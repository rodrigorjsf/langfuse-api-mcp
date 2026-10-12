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

// generatedKey is what follows the prefix of a key as Langfuse generates it
// (#211): a UUID, 8, 4, 4, 4 and 12 hexadecimal digits with hyphens, in either
// case. No word or slug has that shape, so a generated key needs no word
// boundary before its prefix.
const generatedKey = `[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`

// keyPattern matches any Langfuse API key, the configured pair or not:
// pk-lf-… and sk-lf-…, at a word boundary (not a word that merely ends in "pk"
// or "sk") or directly after a wordEndedEscape, and, wherever it stands, a
// generated key: the prefix and a UUID (#211), with every key character that
// follows it. The first group is the escape, which stays: the text around the
// key keeps its meaning as JSON text. The rule reads the text only. It does not
// decode it and does not count backslashes, so a literal backslash and a letter
// before a key (C:\nsk-lf-…) are taken for an escape, an over-redaction
// accepted for the sake of a rule no payload can steer. A key that is no UUID
// glued to a letter, digit or underscore is not found: "task-lf-name" is a word.
// Where a boundary or an escape stands, the first alternative takes the key
// whole, the generated one included; the second one only takes what the first
// cannot reach, a generated key glued to a word character.
var keyPattern = regexp.MustCompile(
	`(?:\b|(` + wordEndedEscape + `))[ps]k-lf-[A-Za-z0-9_-]+|[ps]k-lf-` + generatedKey + `[A-Za-z0-9_-]*`)

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
//     of the text, after a character that is not an ASCII letter or digit, or
//     directly after a wordEndedEscape (#211);
//   - "Bearer ", the word at the start of a word or directly after a
//     wordEndedEscape (#211);
//   - the quoted form (#196), a quoted name that counts, a colon and a quoted
//     value "<scheme> <token>", the value alone or the first item of a list:
//     the JSON text of a header map, escaped or not, or its Python form.
//
// The keywords ignore case; the token does not, because folding a character
// class would admit look-alike letters such as the Kelvin sign. The pattern
// sees one string at a time: the value of a JSON header map that is an object
// of a payload is the business of redactHeaderValue.
var headerPattern = regexp.MustCompile(
	`((?:^|[^A-Za-z0-9]|` + wordEndedEscape + `)` + plainHeader +
		`|(?:\b|` + wordEndedEscape + `)` + bareHeader +
		`|` + quote + quotedName + quote + space + `:` + space + `(?:\[` + space + `)?` + quote + space + scheme + `[ \t]+)` +
		headerToken())

// plainHeader is the plain form of a header credential after its start,
// "Authorization: <scheme> ", and bareHeader the form "Bearer ".
const (
	plainHeader = `(?i:authorization)[ \t]*:[ \t]*` + scheme + `[ \t]+`
	bareHeader  = `(?i:bearer)[ \t]+`
)

// headerToken returns the token of a header credential as a pattern:
// minTokenLength characters of valueChar or more.
func headerToken() string { return valueChar + `{` + strconv.Itoa(minTokenLength) + `,}` }

// headerAtPlacePattern matches, at the start of a text, a header credential of
// the plain form or of the form "Bearer ", up to its token (#211). The first
// group is what stays. redactAfterPlaces reads it on the text from a place on.
var headerAtPlacePattern = regexp.MustCompile(`^(` + plainHeader + `|` + bareHeader + `)` + headerToken())

// headerValuePattern matches the value of a member of a header map: blanks, a
// scheme, blanks and a token of minTokenLength characters or more (#196). The
// first group is what stays.
var headerValuePattern = regexp.MustCompile(`^(` + space + scheme + `[ \t]+)` + headerToken())

// authorizationWord is the word the name rule looks for, in lower case.
const authorizationWord = "authorization"

// bearerWord is the keyword of the bearer form, in lower case.
const bearerWord = "bearer"

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
	redacted := r.redact(redactAfterPlaces(clean, places, droppedBlanks{}))
	if clean == s || redacted != clean {
		return redacted
	}
	return s
}

// redact is Redact for a text that is already clean. The headers are read
// before the keys: a key takes every key character after its prefix, a hyphen
// and the letters of a keyword included (sk-lf-abc-Bearer <token>), so once the
// key is replaced the keyword that led to the token is gone and the token would
// stay.
func (r Redactor) redact(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, Redacted)
	}
	if mentionsHeader(s) { // the common case skips the regular expression
		s = headerPattern.ReplaceAllString(s, "${1}"+Redacted)
	}
	if !strings.Contains(s, "k-lf-") { // and this one
		return s
	}
	return keyPattern.ReplaceAllString(s, "${1}"+Redacted)
}

// keyPrefixLength is the length of "pk-lf-" and "sk-lf-".
const keyPrefixLength = len("sk-lf-")

// redactAfterPlaces returns s, a clean text, with every Langfuse key and the
// token of every header credential that starts at one of places replaced by
// Redacted (#210, #211). A place is where the cleaning removed a hidden or
// control character or a whole terminal control sequence: the patterns read the
// clean text, where that place is gone and the word before it is glued to what
// follows (token, a zero-width space and sk-lf-… read tokensk-lf-…, x, a bell
// and Bearer … read xBearer …), which has no word boundary, so a place is a
// boundary of its own, whatever stands before it. A key goes whole; a header
// keeps its keyword and scheme, as headerPattern leaves them. What keyPattern
// finds on its own, and what a place splits, is the business of the pattern and
// is left to it; so is a text with no place. A header is not left to
// headerPattern when a place is inside a match of it: the token of an earlier
// credential runs up to the next blank, so it swallows the keyword the removed
// character glued to it, and the credential that keyword starts would stay. The
// same holds for a place inside a span replaced here, a key or a token found at
// an earlier place: nothing new starts a key there, but a header keyword does,
// and its token, which stands after the span (a token ends at a blank, a key
// also at a colon or any other character that is none of a key), is replaced.
//
// blanks are the places where a blank of the header rule was dropped (a tab,
// which Line drops and Text keeps): the token that stood after its scheme
// through that blank is glued to the scheme, where no pattern takes it, so the
// place is read as the blank it was (schemeEndsAt) and the run of token
// characters that starts there is replaced, whole, whatever else was dropped
// inside it.
func redactAfterPlaces(s string, places []int, blanks droppedBlanks) string {
	if len(places) == 0 {
		return s
	}
	keys, headers := strings.Contains(s, "k-lf-"), mentionsHeader(s)
	if !keys && !headers && !blanks.headerValue {
		return s
	}
	var keyMatches [][]int // what keyPattern finds on its own
	if keys {
		keyMatches = keyPattern.FindAllStringIndex(s, -1)
	}
	var out strings.Builder
	written := 0 // s[:written] is in out, a replaced key or token included
	runEnd := 0  // where the run of token characters around the last blank read ends
	for i, p := range places {
		var start, end int // the span to replace
		found := false     // whether a key or the token of a header starts at p
		if keys && p >= written && !insideAMatch(keyMatches, p) {
			start = p
			end, found = keyAt(s, p)
		}
		if !found && headers {
			start, end, found = tokenAt(s, p)
		}
		if !found && blanks.dropped(p) {
			if p >= runEnd { // else p is inside the run read last, which ends where it did
				runEnd = p + valueCharsLength(s[p:])
			}
			// What an earlier span left of the run, a key that ended inside it, goes too.
			if runEnd > written && runEnd-p >= minTokenLength && schemeEndsAt(s, places[:i], p, headers, blanks.headerValue) {
				start, end, found = max(p, written), runEnd, true
			}
		}
		if !found || start < written {
			continue
		}
		out.WriteString(s[written:start])
		out.WriteString(Redacted)
		written = end
	}
	if written == 0 {
		return s
	}
	out.WriteString(s[written:])
	return out.String()
}

// droppedBlanks says where a cleaning dropped a blank of the header rule.
type droppedBlanks struct {
	// at holds the places, offsets in the clean text, where a tab was dropped;
	// ascending, each one of the places given with it.
	at []int
	// headerValue says the text is the value of a member under an Authorization
	// name, or an item of its list, where a scheme alone stands before the token.
	headerValue bool
}

// dropped reports whether a blank was dropped at p.
func (b droppedBlanks) dropped(p int) bool {
	_, found := slices.BinarySearch(b.at, p)
	return found
}

// maxBlankReach is how many bytes before a dropped blank schemeEndsAt reads to
// find the header a token there belongs to: more than any header name and
// scheme in use, and a bound on the work per place.
const maxBlankReach = 128

// schemeEndsAt reports whether a blank at p of s, a clean text with a run of at
// least minTokenLength token characters from p on, would make that run the token
// of a header credential: whether headerPattern, or with headerValue the pattern
// of a value under an Authorization name, takes the text with a space put back
// at p up to that space. The rule is the patterns' own; only the last
// maxBlankReach bytes before p are read, and the value form only when it starts
// within them. One of earlier, the places before p, is a start for the keyword
// as it is everywhere (tokenAt). headers says whether s mentions a header
// keyword at all.
func schemeEndsAt(s string, earlier []int, p int, headers, headerValue bool) bool {
	if p == 0 || !isSchemeChar(s[p-1]) {
		return false // no scheme ends here: cheaper than the patterns below
	}
	lo := max(0, p-maxBlankReach)
	local := s[lo:p] + " " + s[p:p+minTokenLength]
	token := p - lo + 1 // where the token starts in local
	if headerValue && lo == 0 {
		if m := headerValuePattern.FindStringSubmatchIndex(local); m != nil && m[3] == token {
			return true
		}
	}
	if !headers || !mentionsHeader(local) {
		return false
	}
	for _, m := range headerPattern.FindAllStringSubmatchIndex(local, -1) {
		if m[3] == token {
			return true
		}
	}
	for i := len(earlier) - 1; i >= 0 && earlier[i] >= lo; i-- {
		// The place in local: the keyword starts there, or within reach in the
		// run it starts, which the token of an earlier credential would swallow.
		if start, _, ok := tokenAt(local, earlier[i]-lo); ok && start == token {
			return true
		}
	}
	return false
}

// isSchemeChar reports whether b is a character of scheme.
func isSchemeChar(b byte) bool {
	return isASCIIAlnum(b) || b == '_' || b == '.' || b == '+' || b == '-'
}

// valueCharsLength returns the length of the run of token characters
// (valueChar) that s starts with.
func valueCharsLength(s string) int {
	n := 0
	for n < len(s) && isValueChar(s[n]) {
		n++
	}
	return n
}

// insideAMatch reports whether p is inside one of matches, the sorted spans a
// pattern found in a text: a place there is the pattern's business.
func insideAMatch(matches [][]int, p int) bool {
	// The first match that ends after p; p is inside it if it starts at or before p.
	i, _ := slices.BinarySearchFunc(matches, p+1, func(m []int, target int) int { return cmp.Compare(m[1], target) })
	return i < len(matches) && matches[i][0] <= p
}

// keyAt returns where the Langfuse key that starts at p of s ends, and whether
// one starts there.
func keyAt(s string, p int) (end int, ok bool) {
	if p+keyPrefixLength >= len(s) || !isKeyPrefix(s[p:p+keyPrefixLength]) {
		return 0, false
	}
	n := keyCharsLength(s[p+keyPrefixLength:])
	if n == 0 {
		return 0, false
	}
	return p + keyPrefixLength + n, true
}

// maxKeywordReach is how many bytes after a place tokenAt looks for the keyword
// of a header credential, so that a header name in front of "authorization"
// (Proxy-Authorization, X-Upstream-Authorization, http.request.header.…) is
// found and the work per place stays bounded: a name that puts the word farther
// away than this is the business of headerPattern.
const maxKeywordReach = 64

// tokenAt returns the span of the token of the header credential whose keyword
// starts at p of s, or in the unbroken run of token characters that starts at p
// and has the keyword within maxKeywordReach bytes, after a character that
// headerPattern accepts before it (a place is a start itself, whatever stands
// before it); ok is false when there is none.
func tokenAt(s string, p int) (start, end int, ok bool) {
	rest := s[p:]
	for i := 0; i < len(rest) && i <= maxKeywordReach; i++ {
		if i > 0 && !isValueChar(rest[i-1]) {
			break // the run the keyword would have to be part of is over
		}
		// Cheaper than the tests below: most bytes start neither word.
		if b := rest[i] | 0x20; b != 'a' && b != 'b' {
			continue
		}
		if !keywordStartsAt(rest, i) {
			continue
		}
		if m := headerAtPlacePattern.FindStringSubmatchIndex(rest[i:]); m != nil {
			return p + i + m[3], p + i + m[1], true // after the group that stays, to the end of the token
		}
	}
	return 0, 0, false
}

// keywordStartsAt reports whether s[i:] starts with "authorization" or "bearer"
// where headerPattern would start the keyword of a credential: at i == 0, the
// place; else "authorization" after a character that is not an ASCII letter or
// digit, and "bearer" at the start of a word.
func keywordStartsAt(s string, i int) bool {
	switch {
	case startsWithFold(s[i:], authorizationWord):
		return i == 0 || !isASCIIAlnum(s[i-1])
	case startsWithFold(s[i:], bearerWord):
		return i == 0 || !isASCIIAlnum(s[i-1]) && s[i-1] != '_'
	}
	return false
}

// isValueChar reports whether b is a character of valueChar, the class a token
// is made of: printable ASCII other than space, the quotes and the backslash.
func isValueChar(b byte) bool {
	return b > ' ' && b < 0x7f && b != '"' && b != '\'' && b != '`' && b != '\\'
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
		case s[i]|0x20 == 'b' && s[i+1]|0x20 == 'e' && startsWithFold(s[i:], bearerWord):
			return true
		case s[i]|0x20 == 'a' && s[i+1]|0x20 == 'u' && startsWithFold(s[i:], authorizationWord):
			return true
		}
	}
	return false
}

// startsWithFold reports whether s starts with word, ignoring case.
func startsWithFold(s, word string) bool {
	return len(s) >= len(word) && strings.EqualFold(s[:len(word)], word)
}
