package server_test

import (
	"cmp"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam S1 (spec #172, ticket #180): the token of an Authorization header or a
// Bearer credential copied into a trace never reaches the agent or the log, as
// a Langfuse key does not: not in a tool result, a tool error, a hint, the
// audit line. The expected texts are literals.

const (
	// seededBearerToken is the token the campaign seeded into a trace
	// (finding M6); seededBearerHeader is the seeded string.
	seededBearerToken  = "seed-fake-bearer-token-00000000"
	seededBearerHeader = "Authorization: Bearer " + seededBearerToken
	// otherBearerToken has no word in common with the text around it, so that
	// a short part of it left behind by a cut is found.
	otherBearerToken  = "Zq9X4k7W2m5N8v3B1c6D" //nolint:gosec // G101: a fake token for the fake Langfuse
	otherBearerHeader = "Authorization: Bearer " + otherBearerToken
	// bearerLeakRun is how many bytes of a token in a row count as a leak.
	bearerLeakRun = 8
)

// plantedCredential is one form of a credential, the token inside it and the
// text that must come back in its place.
type plantedCredential struct{ text, token, redacted string }

// plantedCredentials are the forms a leak could take.
func plantedCredentials() map[string]plantedCredential {
	return map[string]plantedCredential{
		"the seeded Authorization header": {text: seededBearerHeader, token: seededBearerToken,
			redacted: "Authorization: Bearer [REDACTED]"},
		"a Bearer token alone": {text: "Bearer " + otherBearerToken, token: otherBearerToken,
			redacted: "Bearer [REDACTED]"},
		"an Authorization header of another scheme": {text: "authorization: Basic " + otherBearerToken, token: otherBearerToken,
			redacted: "authorization: Basic [REDACTED]"},
		// The header map forms of #196. The map with double quotes needs
		// escaping wherever it is echoed in a message: it has its own tests.
		"a header map written as a Python dictionary": {text: "{'Authorization': 'Basic " + otherBearerToken + "'}",
			token: otherBearerToken, redacted: "{'Authorization': 'Basic [REDACTED]'}"},
		"a header map with a list value": {text: "{'Proxy-Authorization': ['Digest " + otherBearerToken + "']}",
			token: otherBearerToken, redacted: "{'Proxy-Authorization': ['Digest [REDACTED]']}"},
		"a server-side header name": {text: "HTTP_AUTHORIZATION: Negotiate " + otherBearerToken,
			token: otherBearerToken, redacted: "HTTP_AUTHORIZATION: Negotiate [REDACTED]"},
	}
}

// bearerPartOf returns a run of bearerLeakRun bytes of token found in text, so
// that a token cut short still counts as a leak.
func bearerPartOf(token, text string) (string, bool) {
	for i := 0; i+bearerLeakRun <= len(token); i++ {
		if strings.Contains(text, token[i:i+bearerLeakRun]) {
			return token[i : i+bearerLeakRun], true
		}
	}
	return "", false
}

// connectLogged starts the server against fake with a JSON log it returns.
func connectLogged(t *testing.T, fake *httptest.Server, opts ...server.Option) (*mcp.ClientSession, *syncBuffer) {
	t.Helper()
	var logs syncBuffer
	cs := connectServer(t, langfuse.New(testOptions(t, fake.URL)), slog.New(slog.NewJSONHandler(&logs, nil)),
		server.Secrets{Keys: testKeys()}, opts...)
	return cs, &logs
}

// assertNoToken fails the test when the result's text, its structuredContent
// or the log holds a run of the token.
func assertNoToken(t *testing.T, res *mcp.CallToolResult, logs *syncBuffer, token string) {
	t.Helper()
	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	for channel, text := range map[string]string{
		"result text": resultText(t, res), "structuredContent": string(structured), "log": logs.String(),
	} {
		if leak, ok := bearerPartOf(token, text); ok {
			t.Errorf("the %s carries %q of the token:\n%.600s", channel, leak, text)
		}
	}
}

// bearerPayload is a trace page whose input holds text.
func bearerPayload(t *testing.T, input string) string {
	t.Helper()
	return jsonOf(t, map[string]any{"data": []map[string]string{{"id": "t-1", "input": input}}})
}

// idleLangfuse is a Langfuse that no call reaches: a refused call sends nothing.
func idleLangfuse(t *testing.T) *httptest.Server {
	t.Helper()
	fake := httptest.NewServer(nil)
	t.Cleanup(fake.Close)
	return fake
}

// paddedLangfuse answers every request with body(pad), pad being the number in
// the name query parameter of the call (trace_list's name filter carries it);
// a call with no number gets no padding.
func paddedLangfuse(t *testing.T, status int, body func(pad int) string) *httptest.Server {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pad, _ := strconv.Atoi(r.URL.Query().Get("name")) // a missing or bad number means no padding
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body(max(pad, 0)))) // a failed write fails the call on the client side
	}))
	t.Cleanup(fake.Close)
	return fake
}

// connectUnthrottled starts the server against fake with a result size cap of
// 16 KiB and a rate limit high enough for the many calls of a cut test (the
// default 30 a minute would take minutes).
func connectUnthrottled(t *testing.T, fake *httptest.Server) *mcp.ClientSession {
	t.Helper()
	opts := testOptions(t, fake.URL)
	opts.RateLimit = 60_000
	return startServer(t, langfuse.New(opts), slog.New(slog.DiscardHandler),
		server.Secrets{Keys: testKeys()}, langfuse.UnknownProfile(), server.WithMaxResultBytes(16384))
}

// readPadded calls trace_list with the padding the paddedLangfuse answer uses.
func readPadded(t *testing.T, cs *mcp.ClientSession, pad int) *mcp.CallToolResult {
	t.Helper()
	return callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"name": strconv.Itoa(pad)}})
}

func TestTheSeededAuthorizationHeaderInATraceComesBackRedactedWithItsSchemeKept(t *testing.T) {
	t.Parallel()
	fake, _ := fakeLangfuse(t, http.StatusOK, bearerPayload(t, "sent "+seededBearerHeader+" to the API"))
	cs, logs := connectLogged(t, fake)

	res := callExecuteRead(t, cs, traceList)

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	_, inputs := rowInputs(t, decodeResult(t, resultText(t, res)))
	if len(inputs) != 1 || inputs[0] != "sent Authorization: Bearer [REDACTED] to the API" {
		t.Fatalf("inputs = %q, want the seeded header with its token redacted and its scheme kept", inputs)
	}
	assertNoToken(t, res, logs, seededBearerToken)
}

func TestACredentialInsideALangfusePayloadIsRedactedInTheResultTextAndStructuredContent(t *testing.T) {
	t.Parallel()
	for form, c := range plantedCredentials() {
		quoted := jsonOf(t, c.text)
		tests := map[string]string{
			"in a string value":           `{"data":[{"id":"t-1","input":` + quoted + `}]}`,
			"in a sentence":               `{"data":[{"id":"t-1","input":` + jsonOf(t, "headers were "+c.text+", all of them") + `}]}`,
			"in an object key":            `{"data":[{"id":"t-1","metadata":{` + quoted + `:"x"}}]}`,
			"in a nested list":            `{"data":[{"id":"t-1","metadata":{"headers":[[` + quoted + `]]}}]}`,
			"as the cursor of a page":     `{"data":[],"meta":{"cursor":` + quoted + `}}`,
			"in a payload that is a text": quoted,
		}
		for where, body := range tests {
			t.Run(form+" "+where, func(t *testing.T) {
				t.Parallel()
				fake, _ := fakeLangfuse(t, http.StatusOK, body)
				cs, logs := connectLogged(t, fake)

				res := callExecuteRead(t, cs, traceList)

				if res.IsError {
					t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
				}
				assertNoToken(t, res, logs, c.token)
				if !strings.Contains(resultText(t, res), "[REDACTED]") {
					t.Errorf("the result does not mark where the token was: %s", resultText(t, res))
				}
			})
		}
	}
}

func TestProseThatOnlyMentionsBearerOrAuthorizationComesBackUnchanged(t *testing.T) {
	t.Parallel()
	prose := []string{
		"The Authorization header carries a Bearer token that expires after an hour.",
		"the bearer of good news",
		"Authorization: required",
		"Authorization: Bearer",
	}
	fake, _ := fakeLangfuse(t, http.StatusOK, bearerPayload(t, strings.Join(prose, "\n")))
	cs := connect(t, fake)

	_, inputs := rowInputs(t, decodeResult(t, resultText(t, callExecuteRead(t, cs, traceList))))

	if len(inputs) != 1 || inputs[0] != strings.Join(prose, "\n") {
		t.Fatalf("input = %q, want the prose unchanged", inputs)
	}
}

func TestACredentialInAnObservationOfTheTraceTreeIsRedacted(t *testing.T) {
	t.Parallel()
	page := jsonOf(t, map[string]any{"data": []map[string]any{{
		"id": "o-1", "traceId": "t-1", "parentObservationId": nil, "startTime": "2026-09-25T10:00:00.000Z",
		"type": "SPAN", "name": "call", "input": "sent " + seededBearerHeader, "statusMessage": "retry with Bearer " + otherBearerToken,
	}}, "meta": map[string]any{"cursor": nil}})
	fake, _ := observationsLangfuse(t, map[string]string{"": page})
	cs, logs := connectLogged(t, fake)

	res := callTool(t, cs, "get_trace_tree", map[string]any{"traceId": "t-1", "include": []string{"io"}})

	if res.IsError {
		t.Fatalf("get_trace_tree returned a tool error: %s", resultText(t, res))
	}
	row := traceTreeRow(t, resultText(t, res))
	if row["input"] != "sent Authorization: Bearer [REDACTED]" || row["statusMessage"] != "retry with Bearer [REDACTED]" {
		t.Fatalf("row = %v, want the input and the status message redacted with their schemes kept", row)
	}
	assertNoToken(t, res, logs, seededBearerToken)
	assertNoToken(t, res, logs, otherBearerToken)
}

// traceTreeRow returns the one row of a get_trace_tree result.
func traceTreeRow(t *testing.T, text string) map[string]any {
	t.Helper()
	var env traceTreeEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil || len(env.Data.Data) != 1 {
		t.Fatalf("result is not an enveloped trace tree of one row (%v): %.300s", err, text)
	}
	return env.Data.Data[0]
}

func TestACredentialInALangfuseErrorMessageIsRedactedInTheToolError(t *testing.T) {
	t.Parallel()
	for form, c := range plantedCredentials() {
		t.Run(form, func(t *testing.T) {
			t.Parallel()
			body := jsonOf(t, map[string]string{"message": "rejected the header " + c.text + " as expired"})
			fake, _ := fakeLangfuse(t, http.StatusBadRequest, body)
			cs, logs := connectLogged(t, fake)

			res := callExecuteRead(t, cs, traceList)

			want := "Langfuse answered HTTP 400: rejected the header " + c.redacted + " as expired"
			if got := toolErrorOf(t, res).Error.Message; got != want {
				t.Fatalf("message = %q, want %q", got, want)
			}
			assertNoToken(t, res, logs, c.token)
		})
	}
}

func TestTheSeededHeaderInALangfuseErrorMessageKeepsItsScheme(t *testing.T) {
	t.Parallel()
	fake, _ := fakeLangfuse(t, http.StatusUnauthorized, jsonOf(t, map[string]string{"message": "bad " + seededBearerHeader}))
	cs := connect(t, fake)

	got := toolErrorOf(t, callExecuteRead(t, cs, traceList)).Error

	if want := "Langfuse answered HTTP 401: bad Authorization: Bearer [REDACTED]"; got.Message != want {
		t.Fatalf("message = %q, want %q", got.Message, want)
	}
}

// invisibleCharacters split a token the way a hidden character does: they are
// dropped before the agent sees the text, so redaction must look at the text
// without them.
func invisibleCharacters() map[string]string {
	return map[string]string{
		"a zero-width space":   string(rune(0x200b)),
		"a bidi override":      string(rune(0x202e)),
		"a tag character":      string(rune(0xE0041)),
		"a variation selector": string(rune(0xFE0F)),
		"a control character":  string(rune(0x07)),
	}
}

func TestATokenSplitByInvisibleCharactersIsRedactedAfterTheyAreStripped(t *testing.T) {
	t.Parallel()
	for hidden, split := range invisibleCharacters() {
		half := len(seededBearerToken) / 2
		text := "Authorization:" + split + " Bearer " + seededBearerToken[:half] + split + seededBearerToken[half:]
		quoted := jsonOf(t, text)
		tests := map[string]struct {
			status int
			body   string
		}{
			"in a payload":                {http.StatusOK, `{"data":[{"id":"t-1","input":` + quoted + `}]}`},
			"in a payload object key":     {http.StatusOK, `{"data":[{"id":"t-1","metadata":{` + quoted + `:"x"}}]}`},
			"in a Langfuse error message": {http.StatusBadRequest, `{"message":` + quoted + `}`},
		}
		for where, tc := range tests {
			t.Run(hidden+" "+where, func(t *testing.T) {
				t.Parallel()
				fake, _ := fakeLangfuse(t, tc.status, tc.body)
				cs, logs := connectLogged(t, fake)

				res := callExecuteRead(t, cs, traceList)

				assertNoToken(t, res, logs, seededBearerToken)
				if !strings.Contains(resultText(t, res), "Bearer [REDACTED]") {
					t.Errorf("the result does not mark where the token was: %s", resultText(t, res))
				}
			})
		}
	}
}

func TestATokenSplitByInvisibleCharactersInACallsOwnTextIsRedactedAfterTheyAreStripped(t *testing.T) {
	t.Parallel()
	for hidden, split := range invisibleCharacters() {
		half := len(seededBearerToken) / 2
		text := "Bearer " + seededBearerToken[:half] + split + seededBearerToken[half:]
		calls := map[string]struct {
			tool string
			args map[string]any
		}{
			"an unknown operation ID":                  {"execute_read", map[string]any{"operationId": text}},
			"an unknown parameter name":                {"execute_read", map[string]any{"operationId": "trace_list", "parameters": map[string]any{text: "x"}}},
			"an unknown argument name":                 {"execute_read", map[string]any{"operationId": "trace_list", text: "x"}},
			"an operation ID to describe_operation":    {"describe_operation", map[string]any{"operationId": text}},
			"an unknown argument of search_operations": {"search_operations", map[string]any{text: "x"}},
			"an unknown argument of get_trace_tree":    {"get_trace_tree", map[string]any{"traceId": "t-1", text: "x"}},
		}
		for where, tc := range calls {
			t.Run(hidden+" "+where, func(t *testing.T) {
				t.Parallel()
				cs, logs := connectLogged(t, idleLangfuse(t))

				res := callTool(t, cs, tc.tool, tc.args)

				if !res.IsError {
					t.Fatalf("%s accepted the call: %s", tc.tool, resultText(t, res))
				}
				assertNoToken(t, res, logs, seededBearerToken)
			})
		}
	}
}

func TestACredentialInACallsOwnTextIsRedactedInTheEchoAndTheAuditLine(t *testing.T) {
	t.Parallel()
	for form, c := range plantedCredentials() {
		calls := map[string]struct {
			tool    string
			args    map[string]any
			options []server.Option
		}{
			"an unknown operation ID of execute_read": {tool: "execute_read",
				args: map[string]any{"operationId": c.text}},
			"an unknown operation ID of execute_write": {tool: "execute_write",
				args:    map[string]any{"operationId": c.text},
				options: []server.Option{server.WithWriteMode()}},
			"an unknown operation ID of describe_operation": {tool: "describe_operation",
				args: map[string]any{"operationId": c.text}},
			"an unknown parameter name": {tool: "execute_read",
				args: map[string]any{"operationId": "trace_list", "parameters": map[string]any{c.text: "x"}}},
			"an unknown argument name of execute_read": {tool: "execute_read",
				args: map[string]any{"operationId": "trace_list", c.text: "x"}},
			"an unknown argument name of get_trace_tree": {tool: "get_trace_tree",
				args: map[string]any{"traceId": "t-1", c.text: "x"}},
		}
		for where, tc := range calls {
			t.Run(form+" "+where, func(t *testing.T) {
				t.Parallel()
				cs, logs := connectLogged(t, idleLangfuse(t), tc.options...)

				res := callTool(t, cs, tc.tool, tc.args)

				assertNoToken(t, res, logs, c.token)
				if message := toolErrorOf(t, res).Error.Message; !strings.Contains(message, c.redacted) {
					t.Errorf("the message does not show %q where the token was: %s", c.redacted, message)
				}
			})
		}
	}
}

func TestTheAuditLineNamesAnOperationIDHoldingAHeaderRedactedWithItsSchemeKept(t *testing.T) {
	t.Parallel()
	cs, logs := connectLogged(t, idleLangfuse(t))

	callExecuteRead(t, cs, map[string]any{"operationId": seededBearerHeader})

	var line struct {
		Tool        string `json:"tool"`
		OperationID string `json:"operationId"`
		Code        string `json:"code"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs.String())), &line); err != nil {
		t.Fatalf("the log is not one JSON line (%v): %s", err, logs.String())
	}
	if line.Tool != "execute_read" || line.Code != "operation_not_found" || line.OperationID != "Authorization: Bearer [REDACTED]" {
		t.Fatalf("audit line = %+v, want execute_read, operation_not_found, operationId %q", line, "Authorization: Bearer [REDACTED]")
	}
}

func TestTheHintOfEveryRefusalAndFailureHoldsNoCredentialTheCallCarried(t *testing.T) {
	t.Parallel()
	// The header map forms of #196 are among the planted credentials: the
	// JSON text of a map, written as a quoted name and value.
	for form, header := range map[string]string{
		"an Authorization header": otherBearerHeader,
		"a JSON header map":       `{"Authorization": "Basic ` + otherBearerToken + `"}`,
		"a Python header map":     `{'Authorization': 'Basic ` + otherBearerToken + `'}`,
	} {
		message := jsonOf(t, map[string]string{"message": header})
		tests := map[string]struct {
			status int
			body   string
			args   map[string]any
		}{
			"an unknown operation ID": {args: map[string]any{"operationId": header}},
			"an unknown parameter":    {args: map[string]any{"operationId": "trace_list", "parameters": map[string]any{header: "x"}}},
			"a Folder name not found": {status: http.StatusNotFound, body: message,
				args: map[string]any{"operationId": "prompts_get", "parameters": map[string]any{"promptName": "a/" + header[:10]}}},
			"a bad request":                     {status: http.StatusBadRequest, body: message, args: traceList},
			"a server failure":                  {status: http.StatusInternalServerError, body: message, args: traceList},
			"a rate limit":                      {status: http.StatusTooManyRequests, body: message, args: traceList},
			"a result over the result size cap": {status: http.StatusOK, body: bearerPayload(t, strings.Repeat(header+" ", 9000)), args: traceList},
			"a one-row result": {status: http.StatusOK, body: bearerPayload(t, header),
				args: map[string]any{"operationId": "trace_list", "parameters": map[string]any{"limit": 1}}},
		}
		for name, tc := range tests {
			t.Run(form+" "+name, func(t *testing.T) {
				t.Parallel()
				fake := idleLangfuse(t)
				if tc.body != "" {
					fake, _ = fakeLangfuse(t, cmp.Or(tc.status, http.StatusOK), tc.body)
				}
				cs, logs := connectLogged(t, fake, server.WithMaxResultBytes(16384))

				res := callExecuteRead(t, cs, tc.args)

				assertNoToken(t, res, logs, otherBearerToken)
				var env clippedEnvelope
				if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
					t.Fatalf("result is not JSON: %v", err)
				}
				if leak, ok := bearerPartOf(otherBearerToken, env.Hint); ok {
					t.Errorf("the hint carries %q of the token: %s", leak, env.Hint)
				}
			})
		}
	}
}

// Redaction runs before any length cut: a token that a cut would leave short
// of its length is replaced whole first, so no part of it survives.

func TestATokenThatALangfuseErrorMessageCutWouldHaveLeftShortIsStillRedacted(t *testing.T) {
	t.Parallel()
	fake := paddedLangfuse(t, http.StatusBadRequest, func(pad int) string {
		return jsonOf(t, map[string]string{"message": strings.Repeat("x ", pad/2) + otherBearerHeader + " tail"})
	})
	cs := connectUnthrottled(t, fake)

	// The message is cut at 500 runes: the header starts before, at and after
	// the point where only part of the token would be kept.
	for pad := 400; pad <= 480; pad += 2 {
		got := toolErrorOf(t, readPadded(t, cs, pad)).Error

		if leak, ok := bearerPartOf(otherBearerToken, got.Message); ok {
			t.Fatalf("after %d bytes of padding the message carries %q of the token: %s", pad, leak, got.Message)
		}
	}
}

func TestATokenInACallsOwnTextThatTheEchoCutWouldHaveLeftShortIsStillRedacted(t *testing.T) {
	t.Parallel()
	cs, logs := connectLogged(t, idleLangfuse(t))

	// The echo of a name is cut at 64 bytes: the header starts before, at and
	// after the point where only part of the token would be kept.
	for pad := 20; pad <= 66; pad++ {
		text := strings.Repeat("x", pad) + otherBearerHeader
		for where, call := range map[string]struct {
			tool string
			args map[string]any
		}{
			"an operation ID":                       {"execute_read", map[string]any{"operationId": text}},
			"a parameter name":                      {"execute_read", map[string]any{"operationId": "trace_list", "parameters": map[string]any{text: "x"}}},
			"an argument name":                      {"execute_read", map[string]any{"operationId": "trace_list", text: "x"}},
			"an operation ID to describe_operation": {"describe_operation", map[string]any{"operationId": text}},
		} {
			res := callTool(t, cs, call.tool, call.args)

			if leak, ok := bearerPartOf(otherBearerToken, resultText(t, res)); ok {
				t.Fatalf("%s after %d bytes: the result carries %q of the token: %s", where, pad, leak, resultText(t, res))
			}
		}
	}
	if leak, ok := bearerPartOf(otherBearerToken, logs.String()); ok {
		t.Fatalf("the log carries %q of the token:\n%.600s", leak, logs.String())
	}
}

func TestATokenCutInHalfByAClipIsStillRedacted(t *testing.T) {
	t.Parallel()
	fake := paddedLangfuse(t, http.StatusOK, func(pad int) string {
		return jsonOf(t, map[string]any{"data": []map[string]string{{
			"id": "r1", "input": strings.Repeat("a", max(pad-1, 0)) + " " + otherBearerHeader + " " + strings.Repeat("b", 30000)}}})
	})
	cs := connectUnthrottled(t, fake)
	_, inputs := rowInputs(t, decodeResult(t, resultText(t, readPadded(t, cs, 0))))
	clip := len(inputs[0]) // where the clip lands, marker included: the range of padding to probe
	if clip < 1000 {
		t.Fatalf("the first read clipped the input to %d bytes, want about the result size cap", clip)
	}

	for pad := clip - 90; pad <= clip+3; pad++ { // the header ends well before, at and after the cut
		text := resultText(t, readPadded(t, cs, pad))

		if leak, ok := bearerPartOf(otherBearerToken, text); ok {
			t.Fatalf("after %d bytes: the result carries %q of the token", pad, leak)
		}
	}
}

func TestATokenInAWriteBodyThatTheConfirmationCutWouldHaveLeftShortIsStillRedacted(t *testing.T) {
	t.Parallel()
	// The confirmation shows the body cut at 300 characters: the header starts
	// before, at and after the point where only part of the token would show.
	for pad := 230; pad <= 265; pad++ {
		label := strings.Repeat("a", pad) + " " + otherBearerHeader

		got := question(t, map[string]any{"operationId": "promptVersion_update",
			"parameters": map[string]any{"name": "greeting", "version": 2},
			"body":       map[string]any{"newLabels": []any{label}}})

		if leak, ok := bearerPartOf(otherBearerToken, got); ok {
			t.Fatalf("after %d bytes of padding the confirmation carries %q of the token:\n%s", pad, leak, got)
		}
	}
}
