package sanitize_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam: sanitize.Redactor (pure). Expected texts are literals.

// seededBearer is the string the campaign seeded into a trace (finding M6).
const seededBearer = "Authorization: Bearer seed-fake-bearer-token-00000000"

func TestRedactReplacesTheTokenOfAnAuthorizationOrBearerHeaderAndKeepsTheScheme(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"the seeded campaign string": {
			in: seededBearer, want: "Authorization: Bearer [REDACTED]",
		},
		"a Bearer token alone": {
			in: "sent Bearer abcdefghijklmnop to the API", want: "sent Bearer [REDACTED] to the API",
		},
		"a JWT": {
			in:   "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
			want: "Bearer [REDACTED]",
		},
		"any other scheme after Authorization, in any case": {
			in:   "authorization: basic dXNlcjpwYXNzd29yZDEyMzQ1",
			want: "authorization: basic [REDACTED]",
		},
		"a scheme with digits and dashes": {
			in:   "Authorization: AWS4-HMAC-SHA256 0123456789abcdef0123",
			want: "Authorization: AWS4-HMAC-SHA256 [REDACTED]",
		},
		"bearer in lower and upper case": {
			in: "bearer abcdefghijklmnop and BEARER qrstuvwxyz012345", want: "bearer [REDACTED] and BEARER [REDACTED]",
		},
		"tabs between the words": {
			in: "Authorization:\tBearer\tabcdefghijklmnop", want: "Authorization:\tBearer\t[REDACTED]",
		},
		"no blank after the colon": {
			in: "Authorization:Bearer abcdefghijklmnop", want: "Authorization:Bearer [REDACTED]",
		},
		"a blank before the colon": {
			in: "Authorization : Basic abcdefghijklmnop", want: "Authorization : Basic [REDACTED]",
		},
		"a token that stops at a quote": {
			in: `{"h":"Authorization: Bearer abcdefghijklmnop"}`, want: `{"h":"Authorization: Bearer [REDACTED]"}`,
		},
		"a token that stops at a JSON escape": {
			in: `"Bearer abcdefghijklmnop\nnext line"`, want: `"Bearer [REDACTED]\nnext line"`,
		},
		"a token that stops at a closing single quote": {
			in: "curl -H 'Authorization: Bearer abcdefghijklmnop' https://example.test", want: "curl -H 'Authorization: Bearer [REDACTED]' https://example.test",
		},
		"two tokens in one text": {
			in:   "Bearer abcdefghijklmnop then Authorization: Token 0123456789abcdefgh",
			want: "Bearer [REDACTED] then Authorization: Token [REDACTED]",
		},
		"a token with the characters of a base64 or URL-safe credential": {
			in: "Bearer ab+cd/ef-gh_ij.kl~mn==", want: "Bearer [REDACTED]",
		},
		"a Langfuse key next to a Bearer token": {
			in: "pk-lf-abc123 Bearer abcdefghijklmnop", want: "[REDACTED] Bearer [REDACTED]",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (sanitize.Redactor{}).Redact(tc.in); got != tc.want {
				t.Fatalf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedactKeepsProseThatOnlyMentionsBearerOrAuthorization(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"the words in a sentence":                         "The Authorization header carries a Bearer token that expires after an hour.",
		"a bearer of news":                                "the bearer of good news arrived",
		"Authorization with a value that is not a scheme": "Authorization: required",
		"a header without a token":                        "Authorization: Bearer",
		"a short token, one under the minimum":            "Bearer abcdefghijklmno",
		"a short word after a scheme":                     "Authorization: Basic dXNlcjpw",
		"a long word after a word that is not the scheme": "the Bearer is authentication-and-authorization-service",
		"a word that ends in bearer":                      "NotBearer abcdefghijklmnopqrstuvwxyz",
		"a word that ends in authorization":               "preauthorization: Bearer",
		"the token on the next line":                      "Authorization: Bearer\nabcdefghijklmnopqrstuvwxyz",
		"no Authorization colon":                          "Authorization Bearer",
		"the redaction marker itself":                     "Authorization: Bearer [REDACTED]",
		"a colon with no scheme before the token":         "Authorization: abcdefghijklmnopqrstuvwxyz",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (sanitize.Redactor{}).Redact(in); got != in {
				t.Fatalf("Redact(%q) = %q, want it unchanged", in, got)
			}
		})
	}
}

// The shape is all Redact checks (#180): a long word or a URL straight after
// the keywords is taken for a token. The docs say so; these cases pin it.
func TestRedactTakesALongWordOrURLAfterTheKeywordsForAToken(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"a long word after Bearer":                       {"Use Bearer authentication-required here", "Use Bearer [REDACTED] here"},
		"a URL after Bearer":                             {"the bearer https://example.com/users/12345 link", "the bearer [REDACTED] link"},
		"a URL after a word after Authorization":         {"Authorization: see https://docs.example.com/auth for details", "Authorization: see [REDACTED] for details"},
		"the Authorization header of a proxy, after a -": {"Proxy-Authorization: Basic abcdefghijklmnop", "Proxy-Authorization: Basic [REDACTED]"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (sanitize.Redactor{}).Redact(tc.in); got != tc.want {
				t.Fatalf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A token ends at the first character outside the token set (#180): only the
// part before it is redacted, and only when that part has 16 characters.
func TestRedactEndsATokenAtABackslashAQuoteOrANonASCIICharacter(t *testing.T) {
	t.Parallel()
	umlaut := string(rune(0xf6))
	tests := map[string]struct{ in, want string }{
		"a backslash after 16 characters": {`Bearer abcdefghijklmnop\qrstuvwxyz`, `Bearer [REDACTED]\qrstuvwxyz`},
		"a backslash before 16":           {`Bearer abcdefgh\ijklmnopqrstuvwxyz0123456789`, `Bearer abcdefgh\ijklmnopqrstuvwxyz0123456789`},
		"a non-ASCII letter after 16":     {"Bearer abcdefghijklmnop" + umlaut + "qrst", "Bearer [REDACTED]" + umlaut + "qrst"},
		"a non-ASCII letter before 16":    {"Bearer abcdefghijklmn" + umlaut + "pqrstuvwxyz", "Bearer abcdefghijklmn" + umlaut + "pqrstuvwxyz"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := (sanitize.Redactor{}).Redact(tc.in); got != tc.want {
				t.Fatalf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A token is redacted from its sixteenth character on: sixteen is the
// documented minimum, one less is prose.
func TestRedactTakesATokenOfSixteenCharacters(t *testing.T) {
	t.Parallel()
	if got := (sanitize.Redactor{}).Redact("Bearer abcdefghijklmnop"); got != "Bearer [REDACTED]" {
		t.Errorf("Redact of a 16-character token = %q, want %q", got, "Bearer [REDACTED]")
	}
}

func TestRedactLeavesATokenOfFifteenCharacters(t *testing.T) {
	t.Parallel()
	if got := (sanitize.Redactor{}).Redact("Bearer abcdefghijklmno"); got != "Bearer abcdefghijklmno" {
		t.Errorf("Redact of a 15-character token = %q, want it unchanged", got)
	}
}

// The marker is shorter than the minimum, so redacting twice (a payload is
// redacted per string, then the whole result again) changes nothing.
func TestRedactingARedactedTextChangesNothing(t *testing.T) {
	t.Parallel()
	r := sanitize.NewRedactor("hunter2-hunter2")
	once := r.Redact(seededBearer + " pk-lf-abc123 hunter2-hunter2")
	if once != "Authorization: Bearer [REDACTED] [REDACTED] [REDACTED]" {
		t.Fatalf("Redact = %q", once)
	}
	if twice := r.Redact(once); twice != once {
		t.Fatalf("Redact of a redacted text = %q, want %q", twice, once)
	}
}

// #180: an invisible character inside a secret does not hide it. The
// characters are dropped from a text that held a secret, where they would be
// dropped before the agent saw it anyway; a text with no secret keeps them for
// its own caller to judge.
func TestRedactFindsASecretSplitByInvisibleCharacters(t *testing.T) {
	t.Parallel()
	zwsp, bidi, tag := string(rune(0x200b)), string(rune(0x202e)), string(rune(0xE0041))
	r := sanitize.NewRedactor("hunter2-hunter2")
	tests := map[string]struct{ in, want string }{
		"a Bearer token":           {in: "Bearer abcdefgh" + zwsp + "ijklmnop" + bidi + "qrst", want: "Bearer [REDACTED]"},
		"an Authorization header":  {in: "Authorization:" + zwsp + " Bearer abcdefgh" + tag + "ijklmnop", want: "Authorization: Bearer [REDACTED]"},
		"the word Bearer":          {in: "Bea" + zwsp + "rer abcdefghijklmnop", want: "Bearer [REDACTED]"},
		"a Langfuse key":           {in: "key sk-lf-ab" + zwsp + "cd-ef end", want: "key [REDACTED] end"},
		"a configured secret":      {in: "x hunter2-" + zwsp + "hunter2 y", want: "x [REDACTED] y"},
		"a control character":      {in: "Bearer abcdefgh\x07ijklmnopqr", want: "Bearer [REDACTED]"},
		"text with no secret":      {in: "a" + zwsp + "b" + bidi + "c", want: "a" + zwsp + "b" + bidi + "c"},
		"a short token, still not": {in: "Bearer abc" + zwsp + "def", want: "Bearer abc" + zwsp + "def"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := r.Redact(tc.in); got != tc.want {
				t.Fatalf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A cut that falls inside a token must not leave a prefix of it: callers
// redact first and cut after, and redacting is by token, not by the text a
// cut leaves.
func TestRedactReplacesAWholeLongTokenSoNoPrefixOfItSurvivesACutAfterwards(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("0123456789abcdef", 8)
	got := (sanitize.Redactor{}).Redact("Authorization: Bearer " + token)
	for cut := 1; cut <= len(got); cut++ {
		if strings.Contains(got[:cut], token[:8]) {
			t.Fatalf("a cut at %d of %q still shows the start of the token", cut, got)
		}
	}
}

// FuzzRedact checks the invariants of the last pass over a result: whatever
// text it is given, redacting twice changes nothing, and the JSON text of a
// result stays valid JSON, so a token that ends at a quote or an escape never
// takes the structure with it.
func FuzzRedact(f *testing.F) {
	f.Add(`{"input":"Authorization: Bearer abcdefghijklmnopqrstuvwx","n":1}`)
	f.Add(`["Bearer abcdefghijklmnop\nnext", "authorization:\tBasic 0123456789abcdefgh"]`)
	f.Add(`{"k":"Bea\u200brer abcdefghijklmnop pk-lf-abc"}`)
	f.Add("Authorization: Bearer\nabcdefghijklmnopqrstuvwxyz")
	f.Add("{\"k\":\"x\u009b\",\"s\":\"sk-lf-abc\u009b1m\\n\"}") // a raw single-character CSI beside a quote
	f.Add("sk-lf-abc\x1b[31mdef Bearer \x1b]8;;u\x07abcdefghijklmnopqrstuvwxyz")
	f.Add(`{"headers": {"Authorization": "Basic dXNlcjpwYXNzd29yZDEyMzQ1Njc4"}}`)
	f.Add(`{\"Authorization\": [\"Basic dXNlcjpwYXNzd29yZDEyMzQ1Njc4\"], \"n\": 1}`)
	f.Add(`{'Proxy-Authorization': 'Digest dXNlcjpwYXNzd29yZDEyMzQ1Njc4'}`)
	f.Add("HTTP_AUTHORIZATION: Basic dXNlcjpwYXNzd29yZDEyMzQ1Njc4 \"x authorization\": \"Token abcdefghijklmnop\"")
	f.Add(`{"Authorization": "Basic [REDACTED]abcdefghijklmnop", "x": ["Authorization": 'Bearer abcdefghijklmnopq']}`)
	// A Langfuse key directly after each JSON escape written as text, with one,
	// two and four backslashes, alone and inside JSON text (#208).
	f.Add(`{"output":"{\"a\":\"x\\nsk-lf-abc123\\ny\"}"}`)
	f.Add(`"x\tsk-lf-abc\rpk-lf-def\bsk-lf-ghi\fpk-lf-jkl"`)
	f.Add(`"\\u000ask-lf-abc \\\\u00E9pk-lf-def \\\\nsk-lf-ghi"`)
	f.Add(`x\\nsk-lf-a\\\\tpk-lf-b\u000ask-lf-c`)
	f.Add("\\n\\u200b\u200bsk-lf-abc\\nsk-lf-\\n")
	// A Langfuse key directly after a place where the cleaning removed something:
	// a hidden character, a control character, a whole terminal sequence (#210).
	f.Add("token\u200bsk-lf-abc123 x\apk-lf-def x\x1b[31msk-lf-ghi")
	f.Add("ta\u200bsk-lf-name sk-lf-abc\u202esk-lf-def \x00pk-lf-x")
	f.Add("{\"k\":\"v\u200bsk-lf-abc\\n\x1b]8;;u\apk-lf-d\"}")
	r := sanitize.NewRedactor("hunter2-hunter2")
	f.Fuzz(func(t *testing.T, s string) {
		once := r.Redact(s)
		if twice := r.Redact(once); twice != once {
			t.Fatalf("Redact(%q) = %q, but redacting it again gives %q", s, once, twice)
		}
		if json.Valid([]byte(s)) && !json.Valid([]byte(once)) {
			t.Fatalf("Redact(%q) = %q, which is not JSON", s, once)
		}
	})
}

// BenchmarkRedact is the evidence for the fast paths of Redact (#180): the
// scan for the two keywords and the plain-text test of Text. On the machine
// the numbers were taken on (i7-10700K, go1.27.1), 64 KiB of ASCII JSON took
// 0.14 ms; before the two fast paths it took 3.7 ms (the pattern run on every
// text, the cleaning scan by rune on every text). 64 KiB with non-ASCII
// letters takes 1.0 ms, the cost of cleaning it by rune. Text that names a
// header, so that the pattern runs over all of it, took 3.0 ms before the
// quoted form and the name rule of #196 and takes 8.6 ms now (a header map:
// 2.7 ms before, 8.2 ms now); the text with no header name is unchanged.
// The key after an escape (#208) leaves the text with no "k-lf-" as it was
// (ASCII 0.16 ms before and after, the other three within the noise of a run):
// it skips the key pattern. Text made of nothing but keys, each pair with one
// after an escape, took 1.8 ms before and takes 3.4 ms now, the cost of the
// second way in to the key and of the group that keeps the escape. Since #210
// the cleaning decides an ASCII character in a short test before the Unicode
// tables (removed), so the text with non-ASCII letters takes 0.48 ms instead of
// 1.16 ms (the other three: ASCII 0.14 ms, keys 3.5 ms, unchanged).
func BenchmarkRedact(b *testing.B) {
	for name, input := range map[string]string{
		"ASCII":     `{"name":"checkout-flow","input":"the user asked about authorisation of a bearish, ok","n":12345},`,
		"non-ASCII": `{"name":"checkout-flow","input":"o usuário perguntou sobre autorização de um portador, ok","n":12345},`,
		// Text that names the header and holds no credential: the keyword scan
		// passes, so the pattern runs over every byte of it (#196).
		"mentions the header": `{"name":"checkout-flow","input":"the Authorization header is required, send Basic or Bearer auth","n":12345},`,
		"a header map":        `{"name":"checkout-flow","headers":{"Authorization":"Basic dXNlcjpwYXNzd29yZDEyMzQ1Njc4"},"n":12345},`,
		// Text that holds Langfuse keys, so that the key pattern runs over all
		// of it, with and without a JSON escape before the key (#208).
		"keys, some after an escape": `{"name":"checkout-flow","input":"{\"k\":\"x\\nsk-lf-abc123 and pk-lf-def456\"}","n":12345},`,
	} {
		text := strings.Repeat(input, 64<<10/len(input)+1)
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(text)))
			r := sanitize.NewRedactor("pk-lf-aaaa", "sk-lf-bbbb")
			for b.Loop() {
				r.Redact(text)
			}
		})
	}
}

// BenchmarkPayload measures the object form of #196, which looks at the key of
// every member of every object (isAuthorizationName). 64 KiB of rows, as a
// list of objects with and without a header map. On the machine of
// BenchmarkRedact, rows with no header map took 2.75 ms before and take
// 2.80 ms now; rows with a header map took 2.5 ms before and take 3.1 ms now.
// With the JSON text of #210, taken on the same machine in alternating runs of
// the package before and after: rows with no header map 2.89 ms before and
// 2.91 ms after, rows with a header map 3.17 ms before and 3.18 ms after (a
// string that does not start with a brace, a bracket or a quote costs one more
// byte read). The two rows whose input and output are JSON text are the cost of
// reading it: with nothing to clean 1.59 ms before (the text was left as it
// was) and 4.25 ms after, with a hidden character in each 1.50 ms before and
// 5.36 ms after, which is about what a payload of the same bytes costs to
// decode, walk and write.
func BenchmarkPayload(b *testing.B) {
	for name, row := range map[string]string{
		"rows with no header map": `{"name":"checkout-flow","input":"the user asked about a refund","n":12345,"tags":["a","b"]}`,
		"rows with a header map":  `{"name":"checkout-flow","headers":{"Authorization":"Basic dXNlcjpwYXNzd29yZDEyMzQ1Njc4"},"n":12345}`,
		// A page of Observations v2 rows: input and output are JSON text (#210).
		// With nothing to clean in them the texts come back as they were; with
		// a hidden character written as an escape in each, they are written
		// again.
		"rows with JSON text to keep": `{"name":"checkout-flow","input":"{\"messages\":[{\"role\":\"user\",\"content\":\"the user asked about a refund\"}]}",` +
			`"output":"{\"answer\":\"a refund was issued\",\"ids\":[1,2,3]}","n":12345}`,
		"rows with JSON text to clean": `{"name":"checkout-flow","input":"{\"messages\":[{\"role\":\"user\",\"content\":\"the user asked\\u200b about a refund\"}]}",` +
			`"output":"{\"answer\":\"a refund was issued\\u001b[31m\",\"ids\":[1,2,3]}","n":12345}`,
	} {
		payload := json.RawMessage("[" + strings.TrimSuffix(strings.Repeat(row+",", 64<<10/len(row)+1), ",") + "]")
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			r := sanitize.NewRedactor("pk-lf-aaaa", "sk-lf-bbbb")
			for b.Loop() {
				if _, err := sanitize.Payload(payload, r); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
