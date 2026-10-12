package sanitize_test

import (
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam (spec #172, ticket #211): sanitize.Redactor and sanitize.Payload, pure.
// A header credential whose keyword follows a JSON escape written as text, such
// as the "\n" inside a string that holds JSON text, is redacted like one that
// follows a blank: the escape, the keyword and the scheme stay character for
// character and only the token goes. Expected texts are literals.

// escapeToken is a token of 24 characters, over the minimum of 16.
const escapeToken = "abcdefghijklmnopqrstuvwx"

// escapesBeforeAKeyword is every escape the key rule of #208 takes as a start,
// without its backslashes.
var escapesBeforeAKeyword = []string{"n", "t", "r", "b", "f", "u000a"}

func TestRedactReplacesTheTokenOfAHeaderWhoseKeywordFollowsAnEscapeWrittenAsText(t *testing.T) {
	t.Parallel()
	for _, backslashes := range []int{1, 2, 4} {
		for _, letter := range escapesBeforeAKeyword {
			escape := strings.Repeat(`\`, backslashes) + letter
			for name, header := range map[string]struct{ in, want string }{
				"Bearer":                      {"Bearer " + escapeToken, "Bearer [REDACTED]"},
				"Authorization":               {"Authorization: Basic " + escapeToken, "Authorization: Basic [REDACTED]"},
				"Proxy-Authorization":         {"Proxy-Authorization: Digest " + escapeToken, "Proxy-Authorization: Digest [REDACTED]"},
				"authorization, lower case":   {"authorization: basic " + escapeToken, "authorization: basic [REDACTED]"},
				"BEARER, upper case":          {"BEARER " + escapeToken, "BEARER [REDACTED]"},
				"Authorization with blanks":   {"Authorization \t: \tBasic\t" + escapeToken, "Authorization \t: \tBasic\t[REDACTED]"},
				"Authorization, no blank":     {"Authorization:Bearer " + escapeToken, "Authorization:Bearer [REDACTED]"},
				"a JWT after Bearer":          {"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVP", "Bearer [REDACTED]"},
				"a token that ends at a JSON": {`Bearer ` + escapeToken + `\nnext`, `Bearer [REDACTED]\nnext`},
			} {
				t.Run(strings.ReplaceAll(escape, `\`, "/")+"/"+name, func(t *testing.T) {
					t.Parallel()
					assertRedacts(t, sanitize.Redactor{}, "x"+escape+header.in, "x"+escape+header.want)
				})
			}
		}
	}
}

// The text around the header and the header before the escape keep their rules:
// a blank still starts a word, and two headers in a row are both found.
func TestRedactReplacesTheTokensOfSeveralHeadersSeparatedByEscapes(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"two Bearer tokens": {
			in:   `a\nBearer ` + escapeToken + `\tBearer ` + escapeToken + `\n`,
			want: `a\nBearer [REDACTED]\tBearer [REDACTED]\n`,
		},
		"an Authorization header then a Bearer token": {
			in:   `{"log":"x\nAuthorization: Basic ` + escapeToken + `\nBearer ` + escapeToken + `"}`,
			want: `{"log":"x\nAuthorization: Basic [REDACTED]\nBearer [REDACTED]"}`,
		},
		"a header after an escape and a header after a blank": {
			in:   `a\nBearer ` + escapeToken + ` Bearer ` + escapeToken,
			want: `a\nBearer [REDACTED] Bearer [REDACTED]`,
		},
		"a header after a literal backslash and a letter, accepted over-redaction": {
			in:   `C:\nBearer ` + escapeToken,
			want: `C:\nBearer [REDACTED]`,
		},
		"a header after an escaped backslash and a letter, accepted over-redaction": {
			in:   `C:\\nBearer ` + escapeToken,
			want: `C:\\nBearer [REDACTED]`,
		},
		"a key and a header, each after an escape": {
			in:   `a\nsk-lf-abc\nBearer ` + escapeToken,
			want: `a\n[REDACTED]\nBearer [REDACTED]`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// What is not an escape of the rule, or has a letter between the escape and the
// keyword, or is a name that only holds the word, stays as it is.
func TestRedactKeepsAKeywordThatNoEscapeStartsAsItWas(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"an unknown escape":                                {in: `x\dBearer ` + escapeToken, want: `x\dBearer ` + escapeToken},
		"an unknown escape before Authorization":           {in: `x\dAuthorization: Basic ` + escapeToken, want: `x\dAuthorization: Basic ` + escapeToken},
		"an escape in upper case":                          {in: `x\NBearer ` + escapeToken, want: `x\NBearer ` + escapeToken},
		"a unicode escape of three digits, B taken":        {in: `x\u00fBearer ` + escapeToken, want: `x\u00fBearer ` + escapeToken},
		"a unicode escape cut short":                       {in: `x\u0Bearer ` + escapeToken, want: `x\u0Bearer ` + escapeToken},
		"a letter after the escape":                        {in: `x\nnBearer ` + escapeToken, want: `x\nnBearer ` + escapeToken},
		"a letter after the escape, Authorization":         {in: `x\nnAuthorization: Basic ` + escapeToken, want: `x\nnAuthorization: Basic ` + escapeToken},
		"a name that only ends in the word":                {in: `x\nreauthorization: Basic ` + escapeToken, want: `x\nreauthorization: Basic ` + escapeToken},
		"a name that holds the word":                       {in: `x\nauthorizationMode: Basic ` + escapeToken, want: `x\nauthorizationMode: Basic ` + escapeToken},
		"a word that ends in bearer":                       {in: `x\nNotBearer ` + escapeToken, want: `x\nNotBearer ` + escapeToken},
		"a token of 15 characters":                         {in: `x\nBearer abcdefghijklmno`, want: `x\nBearer abcdefghijklmno`},
		"a token of 15 characters, Authorization":          {in: `x\nAuthorization: Basic abcdefghijklmno`, want: `x\nAuthorization: Basic abcdefghijklmno`},
		"an escape and a keyword with no blank":            {in: `x\nBearer`, want: `x\nBearer`},
		"a letter glued to Bearer, nothing removed":        {in: `xBearer ` + escapeToken, want: `xBearer ` + escapeToken},
		"a letter glued to Authorization, nothing removed": {in: `xAuthorization: Basic ` + escapeToken, want: `xAuthorization: Basic ` + escapeToken},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRedacts(t, sanitize.Redactor{}, tc.in, tc.want)
		})
	}
}

// The prose cases of TestRedactKeepsProseThatOnlyMentionsBearerOrAuthorization,
// each with an escape before the keyword, stay prose.
func TestRedactKeepsProseWithAnEscapeBeforeTheKeyword(t *testing.T) {
	t.Parallel()
	for _, escape := range []string{`\n`, `\\t`, `\u000a`} {
		for name, in := range map[string]string{
			"the words in a sentence":       "The Authorization header carries a Bearer token that expires after an hour.",
			"a bearer of news":              "the bearer of good news arrived",
			"Authorization, not a scheme":   "Authorization: required",
			"a header without a token":      "Authorization: Bearer",
			"a short token":                 "Bearer abcdefghijklmno",
			"a short word after a scheme":   "Authorization: Basic dXNlcjpw",
			"the token on the next line":    "Authorization: Bearer\nabcdefghijklmnopqrstuvwxyz",
			"no Authorization colon":        "Authorization Bearer",
			"the redaction marker itself":   "Authorization: Bearer [REDACTED]",
			"a colon with no scheme":        "Authorization: abcdefghijklmnopqrstuvwxyz",
			"a word that ends in bearer":    "NotBearer abcdefghijklmnopqrstuvwxyz",
			"a long word that is no scheme": "the Bearer is authentication-and-authorization-service",
		} {
			t.Run(escape+"/"+name, func(t *testing.T) {
				t.Parallel()
				text := "x" + escape + in
				assertRedacts(t, sanitize.Redactor{}, text, text)
			})
		}
	}
}

// The same headers in a payload. A string whose text holds the escape of a line
// feed is the Observations v2 form: the text is JSON text and is decoded, so the
// header follows a real line feed; the second case holds a literal backslash and
// n, which only the pattern reads.
func TestPayloadRedactsAHeaderWhoseKeywordFollowsAnEscapeWrittenAsText(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a literal backslash and n": {
			in:   `{"a":"x\\nBearer ` + escapeToken + `"}`,
			want: `{"a":"x\\nBearer [REDACTED]"}`,
		},
		"a literal backslash and n, Authorization": {
			in:   `{"a":"x\\nAuthorization: Basic ` + escapeToken + `"}`,
			want: `{"a":"x\\nAuthorization: Basic [REDACTED]"}`,
		},
		"JSON text, a line feed": {
			in:   `{"output":"{\"body\":\"x\\nBearer ` + escapeToken + `\"}"}`,
			want: `{"output":"{\"body\":\"x\\nBearer [REDACTED]\"}"}`,
		},
		"an object key": {
			in:   `{"x\\nBearer ` + escapeToken + `":"v"}`,
			want: `{"x\\nBearer [REDACTED]":"v"}`,
		},
		"a list item": {
			in:   `["a","x\\tAuthorization: Basic ` + escapeToken + `"]`,
			want: `["a","x\\tAuthorization: Basic [REDACTED]"]`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertPayloadIs(t, tc.in, tc.want)
		})
	}
}
