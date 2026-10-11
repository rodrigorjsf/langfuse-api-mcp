package server_test

import (
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam S1 (spec #172, ticket #193): a terminal escape sequence in what
// Langfuse sends is removed whole, wherever the server strips control
// characters from Langfuse data: tool results, keys and values, and error text.
// The removal runs before redaction and before the result size cap is measured.

// osc8Link is an OSC 8 hyperlink as JSON text: the URL is where the link leads,
// "click" is what a terminal shows.
const osc8Link = `\u001b]8;;https://evil.example/pay?token=1\u0007click\u001b]8;;\u0007`

func TestATerminalEscapeSequenceInAPayloadIsRemovedWholeFromKeysAndValues(t *testing.T) {
	t.Parallel()
	body := `{"na\u001b[31mme":"\u001b[1;32mgreen\u001b[m text","link":"` + osc8Link + `",` +
		`"moved":"a\u001b[2;5Hb\u009b3Ac","bare":"[31mred[0m ]8;;x","split":["\u001b[3","1m"],` +
		`"cut":"x\u001b[31"}`
	fake, _ := fakeLangfuse(t, http.StatusOK, body)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_get", "parameters": map[string]any{"traceId": "trace-1"},
	})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	want := map[string]any{
		"name":  "green text",
		"link":  "click",
		"moved": "abc",
		"bare":  "[31mred[0m ]8;;x", // no escape character before it: ordinary data
		"split": []any{"[3", "1m"},  // each string is cleaned alone
		"cut":   "x[31",             // no end within reach: the escape character goes alone
	}
	if got := res.StructuredContent.(map[string]any)["data"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("data = %#v, want %#v", got, want)
	}
	text := resultText(t, res)
	for _, left := range []string{`\u001b`, "\x1b", "\u009b", "evil.example", "token=1", `\u0007`} {
		if strings.Contains(text, left) {
			t.Errorf("the result text still holds %q: %s", left, text)
		}
	}
}

// A payload cannot make the sanitizer remove the text after a sequence that
// has no end within the fixed length: the escape character goes alone.
func TestAnUnterminatedEscapeSequenceNeverTakesTheTextThatFollowsIt(t *testing.T) {
	t.Parallel()
	follows := "é" + strings.Repeat("the text after the escape stays. ", 200)
	for name, lead := range map[string]string{
		"an OSC":                         `\u001b]8;;http://u`,
		"a CSI":                          `\u001b[1;2;3`,
		"a one-character CSI":            `\u009b1;2;3`,
		"an OSC 8 whose URL is too long": `\u001b]8;;` + strings.Repeat("a", 300) + `\u0007`,
	} {
		fake, _ := fakeLangfuse(t, http.StatusOK, `{"x":"`+lead+follows+`"}`)
		cs := connect(t, fake)

		res := callExecuteRead(t, cs, map[string]any{
			"operationId": "trace_get", "parameters": map[string]any{"traceId": "trace-1"},
		})

		got, _ := res.StructuredContent.(map[string]any)["data"].(map[string]any)["x"].(string)
		if !strings.HasSuffix(got, follows) || strings.ContainsAny(got, "\x1b\u009b\a") {
			t.Errorf("%s: x = %.80q…, want the text that follows kept whole and no escape or BEL", name, got)
		}
	}
}

// The removal runs before redaction: a Langfuse key, a bearer token or a
// configured secret that a sequence hides behind or splits is found whole.
func TestAKeyHiddenBehindOrSplitByAnEscapeSequenceIsStillRedactedInAResult(t *testing.T) {
	t.Parallel()
	splitSecret := testSecretKey[:12] + `\u001b[31m` + testSecretKey[12:]
	body := `{"a":"key sk-lf-ab\u001b[31mcdef-123456 end","b":"Authorization: Bearer ` + `\u001b]8;;u\u0007abcdefghijklmnopqrstuvwxyz","c":"` + splitSecret + `","d":{"pk-lf-ab\u001b]0;t\u0007cdef-789":"v"}}`
	fake, _ := fakeLangfuse(t, http.StatusOK, body)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_get", "parameters": map[string]any{"traceId": "trace-1"},
	})

	text := resultText(t, res)
	for _, leaked := range []string{"cdef-123456", "cdef-789", "abcdefghijklmnopqrstuvwxyz", testSecretKey[12:], `\u001b`, "\x1b"} {
		if strings.Contains(text, leaked) {
			t.Errorf("the result still holds %q: %s", leaked, text)
		}
	}
	if got := strings.Count(text, "[REDACTED]"); got < 4 {
		t.Errorf("the result holds %d redactions, want one for each of the 4 credentials: %s", got, text)
	}
}

// Sequences are removed before the size of the result is measured: a value
// that is mostly escape sequences does not count them against the cap.
func TestEscapeSequencesAreRemovedBeforeTheResultSizeCapIsMeasured(t *testing.T) {
	t.Parallel()
	value := strings.Repeat(`\u001b[0m`, 6000) + "tail" // 60000 bytes on the wire, 4 characters to read
	fake, _ := fakeLangfuse(t, http.StatusOK, `{"x":"`+value+`"}`)
	cs := startServer(t, langfuse.New(testOptions(t, fake.URL)), slog.New(slog.DiscardHandler),
		server.Secrets{Keys: testKeys()}, langfuse.UnknownProfile(), server.WithMaxResultBytes(16384))

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_get", "parameters": map[string]any{"traceId": "trace-1"},
	})

	env := res.StructuredContent.(map[string]any)
	if env["truncated"] == true || !reflect.DeepEqual(env["data"], map[string]any{"x": "tail"}) {
		t.Fatalf("result = %v, want x = tail and no truncation", env)
	}
}

// Error text taken from Langfuse loses its sequences whole too, and a key an
// error message holds behind one is still redacted.
func TestAnEscapeSequenceInALangfuseErrorMessageIsRemovedWholeAndASplitKeyIsRedacted(t *testing.T) {
	t.Parallel()
	fake, _ := scriptedLangfuse(t, answer{status: http.StatusBadRequest, body: `{"message":"bad \u001b[31mparam\u001b[0m ` +
		`\u001b]8;;http://phish.example\u0007here\u001b]8;;\u0007 sk-lf-ab\u001b[1mcdef-123456 \u009b2J end"}`})
	cs := connect(t, fake)

	got := toolErrorOf(t, callExecuteRead(t, cs, traceList)).Error

	if want := "Langfuse answered HTTP 400: bad param here [REDACTED]  end"; got.Message != want {
		t.Errorf("message = %q, want %q", got.Message, want)
	}
	if all := got.Message + got.Hint; strings.ContainsAny(all, "\x1b\u009b") || strings.Contains(all, "phish.example") ||
		strings.Contains(all, "[31m") || strings.Contains(all, "cdef-123456") {
		t.Errorf("the error keeps a sequence or a key: %+v", got)
	}
}

// A control character in a parameter value is refused, never removed, and an
// escape sequence is made of control characters: the call fails before any
// request and the refusal repeats nothing of the value.
func TestAnEscapeSequenceInAParameterValueIsRefusedWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"a colour sequence":      "ab\x1b[31mcd",
		"an OSC 8 hyperlink":     "ab\x1b]8;;evil.example\x07cd",
		"the one-character CSI":  "ab\u009b31mcd",
		"an escape character":    "ab\x1bcd",
		"an unterminated escape": "ab\x1b[31" + plantedText,
	} {
		for what, call := range map[string]map[string]any{
			"a path parameter":  {"operationId": "sessions_get", "parameters": map[string]any{"sessionId": value}},
			"a query parameter": {"operationId": "trace_list", "parameters": map[string]any{"userId": value}},
		} {
			fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
			cs := connect(t, fake)

			got := toolErrorOf(t, callExecuteRead(t, cs, call)).Error

			if got.Code != "invalid_argument" || !strings.Contains(got.Message, "control or invisible") {
				t.Errorf("%s in %s: error = %+v, want invalid_argument naming control or invisible characters", name, what, got)
			}
			if all := got.Message + got.Hint; strings.ContainsAny(all, "\x1b\u009b") || strings.Contains(all, "evil.example") ||
				strings.Contains(all, plantedText) || strings.Contains(all, "31m") {
				t.Errorf("%s in %s: the refusal repeats the value: %+v", name, what, got)
			}
			assertNoRequest(t, seen)
		}
	}
}
