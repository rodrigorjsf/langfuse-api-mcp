package server_test

import (
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
	otherBearerToken = "Zq9X4k7W2m5N8v3B1c6D" //nolint:gosec // G101: a fake token for the fake Langfuse
	// bearerLeakRun is how many bytes of a token in a row count as a leak.
	bearerLeakRun = 8
)

// plantedCredential is one form of a credential and the token inside it.
type plantedCredential struct{ text, token string }

// plantedCredentials are the forms a leak could take.
func plantedCredentials() map[string]plantedCredential {
	return map[string]plantedCredential{
		"the seeded Authorization header":        {seededBearerHeader, seededBearerToken},
		"a Bearer token alone":                   {"Bearer " + otherBearerToken, otherBearerToken},
		"an Authorization header of another one": {"authorization: Basic " + otherBearerToken, otherBearerToken},
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

			got := toolErrorOf(t, res).Error
			if got.Code != "langfuse_bad_request" || !strings.HasPrefix(got.Message, "Langfuse answered HTTP 400: rejected the header ") ||
				!strings.HasSuffix(got.Message, "[REDACTED] as expired") {
				t.Fatalf("tool error = %+v, want langfuse_bad_request with the token replaced by [REDACTED]", got)
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
				fake := httptest.NewServer(nil) // a refused call sends nothing
				t.Cleanup(fake.Close)
				cs, logs := connectLogged(t, fake)

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
				fake := httptest.NewServer(nil) // a refused call sends nothing
				t.Cleanup(fake.Close)
				cs, logs := connectLogged(t, fake, tc.options...)

				res := callTool(t, cs, tc.tool, tc.args)

				got := toolErrorOf(t, res).Error
				assertNoToken(t, res, logs, c.token)
				if leak, ok := bearerPartOf(c.token, got.Hint); ok {
					t.Errorf("the hint carries %q of the token: %s", leak, got.Hint)
				}
				if !strings.Contains(got.Message, "[REDACTED]") {
					t.Errorf("the message does not mark where the token was: %s", got.Message)
				}
			})
		}
	}
}

func TestTheAuditLineNamesAnOperationIDHoldingAHeaderRedactedWithItsSchemeKept(t *testing.T) {
	t.Parallel()
	fake := httptest.NewServer(nil)
	t.Cleanup(fake.Close)
	cs, logs := connectLogged(t, fake)

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
	const header = "Authorization: Bearer " + otherBearerToken
	tests := map[string]struct {
		status int
		body   string
		args   map[string]any
	}{
		"an unknown operation ID": {args: map[string]any{"operationId": header}},
		"an unknown parameter":    {args: map[string]any{"operationId": "trace_list", "parameters": map[string]any{header: "x"}}},
		"a Folder name not found": {status: http.StatusNotFound, body: `{"message":"` + header + `"}`,
			args: map[string]any{"operationId": "prompts_get", "parameters": map[string]any{"promptName": "a/" + header[:10]}}},
		"a bad request":     {status: http.StatusBadRequest, body: `{"message":"` + header + `"}`, args: traceList},
		"a server failure":  {status: http.StatusInternalServerError, body: `{"message":"` + header + `"}`, args: traceList},
		"a rate limit":      {status: http.StatusTooManyRequests, body: `{"message":"` + header + `"}`, args: traceList},
		"a payload to cut":  {status: http.StatusOK, body: bearerPayload(t, strings.Repeat(header+" ", 9000)), args: traceList},
		"a response to cut": {status: http.StatusOK, body: bearerPayload(t, header), args: map[string]any{"operationId": "trace_list", "parameters": map[string]any{"limit": 1}}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			status := cmpOrOK(tc.status)
			var fake *httptest.Server
			if tc.body == "" {
				fake = httptest.NewServer(nil)
				t.Cleanup(fake.Close)
			} else {
				fake, _ = fakeLangfuse(t, status, tc.body)
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
			if res.IsError {
				if hint := toolErrorOf(t, res).Error.Hint; hint == "" {
					t.Errorf("the tool error has no hint")
				}
			}
		})
	}
}

// cmpOrOK returns status, or 200 when it is zero.
func cmpOrOK(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}

// Redaction runs before any length cut: a token that a cut would leave short
// of its length is replaced whole first, so no part of it survives.

func TestATokenThatALangfuseErrorMessageCutWouldHaveLeftShortIsStillRedacted(t *testing.T) {
	t.Parallel()
	const prefix = "Langfuse answered HTTP 400: "
	header := "Authorization: Bearer " + otherBearerToken
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pad, _ := strconv.Atoi(r.URL.Query().Get("name")) // trace_list's name filter carries the padding
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(jsonOf(t, map[string]string{"message": strings.Repeat("x ", pad/2) + header + " tail"})))
	}))
	t.Cleanup(fake.Close)
	opts := testOptions(t, fake.URL)
	opts.RateLimit = 60_000 // many calls: the default 30 a minute would take minutes
	cs := connectServer(t, langfuse.New(opts), slog.New(slog.DiscardHandler), server.Secrets{Keys: testKeys()})

	// The message is cut at 500 runes: the header starts before, at and after
	// the point where only part of the token would be kept.
	for pad := 400; pad <= 480; pad += 2 {
		res := callExecuteRead(t, cs, map[string]any{"operationId": "trace_list", "parameters": map[string]any{"name": strconv.Itoa(pad)}})

		got := toolErrorOf(t, res).Error
		if leak, ok := bearerPartOf(otherBearerToken, got.Message); ok {
			t.Fatalf("after %d bytes of padding the message carries %q of the token: %s", len(prefix)+pad, leak, got.Message)
		}
	}
}

func TestATokenInACallsOwnTextThatTheEchoCutWouldHaveLeftShortIsStillRedacted(t *testing.T) {
	t.Parallel()
	fake := httptest.NewServer(nil)
	t.Cleanup(fake.Close)
	header := "Authorization: Bearer " + otherBearerToken
	cs, logs := connectLogged(t, fake)

	// The echo of a name is cut at 64 bytes: the header starts before, at and
	// after the point where only part of the token would be kept.
	for pad := 20; pad <= 66; pad++ {
		text := strings.Repeat("x", pad) + header
		for where, args := range map[string]map[string]any{
			"an operation ID":    {"operationId": text},
			"a parameter name":   {"operationId": "trace_list", "parameters": map[string]any{text: "x"}},
			"an argument name":   {"operationId": "trace_list", text: "x"},
			"a describe request": {"operationId": text},
		} {
			tool := "execute_read"
			if where == "a describe request" {
				tool = "describe_operation"
			}
			res := callTool(t, cs, tool, args)

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
	header := "Authorization: Bearer " + otherBearerToken
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pad, _ := strconv.Atoi(r.URL.Query().Get("name")) // trace_list's name filter carries the padding
		body := jsonOf(t, map[string]any{"data": []map[string]string{
			{"id": "r1", "input": strings.Repeat("a", max(pad-1, 0)) + " " + header + " " + strings.Repeat("b", 30000)}}})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body)) // a failed write fails the call
	}))
	t.Cleanup(fake.Close)
	opts := testOptions(t, fake.URL)
	opts.RateLimit = 60_000 // many calls: the default 30 a minute would take minutes
	cs := startServer(t, langfuse.New(opts), slog.New(slog.DiscardHandler),
		server.Secrets{Keys: testKeys()}, langfuse.UnknownProfile(), server.WithMaxResultBytes(16384))
	read := func(pad int) string {
		return resultText(t, callExecuteRead(t, cs, map[string]any{
			"operationId": "trace_list", "parameters": map[string]any{"name": strconv.Itoa(pad)}}))
	}
	_, inputs := rowInputs(t, decodeResult(t, read(0)))
	clip := len(inputs[0]) // where the clip lands, marker included
	if clip < 1000 {
		t.Fatalf("the first read clipped the input to %d bytes, want about the result size cap", clip)
	}

	for pad := clip - 90; pad <= clip+3; pad++ { // the header ends well before, at and after the cut
		text := read(pad)

		if leak, ok := bearerPartOf(otherBearerToken, text); ok {
			t.Fatalf("after %d bytes: the result carries %q of the token", pad, leak)
		}
	}
}
