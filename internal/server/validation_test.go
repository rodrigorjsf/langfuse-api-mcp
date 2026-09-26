package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolErrorFields is the ADR-0008 tool error shape, as the agent reads it.
type toolErrorFields struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	Hint              string `json:"hint"`
	Retryable         bool   `json:"retryable"`
	HTTPStatus        int    `json:"httpStatus"`
	RetryAfterSeconds int    `json:"retryAfterSeconds"`
	OperationID       string `json:"operationId"`
}

// toolErrorOf returns the tool error of a result, failing the test when the
// result is not a tool error.
func toolErrorOf(t *testing.T, res *mcp.CallToolResult) toolErrorFields {
	t.Helper()
	if !res.IsError {
		t.Fatalf("result is not a tool error: %s", resultText(t, res))
	}
	var body struct {
		Error toolErrorFields `json:"error"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &body); err != nil {
		t.Fatalf("tool error text is not JSON: %v", err)
	}
	return body.Error
}

// assertNoRequest fails the test when the fake Langfuse received a request.
func assertNoRequest(t *testing.T, seen <-chan received) {
	t.Helper()
	select {
	case got := <-seen:
		t.Fatalf("Langfuse received %s %s for an invalid call", got.method, got.path)
	default:
	}
}

func TestExecuteReadRejectsInvalidParametersNamingTheFieldAndTheReason(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		operationID string
		params      map[string]any
		wantField   string
		wantReason  string
	}{
		"missing required path parameter": {
			operationID: "trace_get", params: map[string]any{},
			wantField: "traceId", wantReason: "required",
		},
		"missing required query parameter": {
			operationID: "metrics_metrics", params: nil,
			wantField: "query", wantReason: "required",
		},
		"unknown parameter": {
			operationID: "trace_list", params: map[string]any{"userID": "u-42"},
			wantField: "userID", wantReason: "unknown parameter",
		},
		"string where an integer is expected": {
			operationID: "trace_list", params: map[string]any{"limit": "ten"},
			wantField: "limit", wantReason: "integer",
		},
		"fraction where an integer is expected": {
			operationID: "trace_list", params: map[string]any{"page": 1.5},
			wantField: "page", wantReason: "integer",
		},
		"string where a boolean is expected": {
			operationID: "prompts_get", params: map[string]any{"promptName": "support", "resolve": "yes"},
			wantField: "resolve", wantReason: "boolean",
		},
		"number where a string is expected": {
			operationID: "trace_list", params: map[string]any{"userId": 42},
			wantField: "userId", wantReason: "string",
		},
		"list for a parameter that is not repeated": {
			operationID: "trace_list", params: map[string]any{"userId": []any{"a", "b"}},
			wantField: "userId", wantReason: "string",
		},
		"value outside the allowed set": {
			operationID: "observations_getMany", params: map[string]any{"level": "LOUD"},
			wantField: "level", wantReason: "DEBUG, DEFAULT, WARNING, ERROR",
		},
		"list item of the wrong type": {
			operationID: "trace_list", params: map[string]any{"tags": []any{"prod", 7}},
			wantField: "tags", wantReason: "string",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
			cs := connect(t, fake)
			args := map[string]any{"operationId": tc.operationID}
			if tc.params != nil {
				args["parameters"] = tc.params
			}

			got := toolErrorOf(t, callExecuteRead(t, cs, args))

			if got.Code != "invalid_argument" || got.OperationID != tc.operationID {
				t.Errorf("error = %+v, want code invalid_argument for %s", got, tc.operationID)
			}
			if !strings.Contains(got.Message, tc.wantField) || !strings.Contains(got.Message, tc.wantReason) {
				t.Errorf("message %q does not name field %q and reason %q", got.Message, tc.wantField, tc.wantReason)
			}
			assertNoRequest(t, seen)
		})
	}
}

func TestExecuteReadRejectsPathParameterValuesThatCouldRedirectTheRequest(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"a/b",
		"..",
		".",
		"trace..1",
		"../../ingestion",
		`..\ingestion`,
		"//evil.example",
		"https://evil.example/api",
		"https:evil.example",
		"HTTP:evil.example",
		"",
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
			cs := connect(t, fake)

			got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
				"operationId": "sessions_get", "parameters": map[string]any{"sessionId": value},
			}))

			if got.Code != "invalid_argument" || !strings.Contains(got.Message, "sessionId") {
				t.Errorf("error = %+v, want invalid_argument naming sessionId", got)
			}
			assertNoRequest(t, seen)
		})
	}
}

func TestExecuteReadPercentEncodesPathParameterValues(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "sessions_get", "parameters": map[string]any{"sessionId": "user:42 chat?x=1#top%2F"},
	})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	if got, want := receivedOne(t, seen).path, "/api/public/sessions/user:42%20chat%3Fx=1%23top%252F"; got != want {
		t.Errorf("path = %s, want %s", got, want)
	}
}

func TestExecuteReadRefusesAWriteOperationSayingItRunsReadOperationsOnly(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
	cs := connect(t, fake)

	got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
		"operationId": "prompts_create", "parameters": map[string]any{},
	}))

	if got.Code != "invalid_argument" || got.OperationID != "prompts_create" ||
		!strings.Contains(got.Message, "read (GET) operations only") {
		t.Errorf("error = %+v, want invalid_argument saying execute_read runs read (GET) operations only", got)
	}
	assertNoRequest(t, seen)
}

func TestExecuteReadAnswersAnUnknownOrExcludedOperationWithOperationNotFound(t *testing.T) {
	t.Parallel()
	for name, id := range map[string]string{
		"unknown":                 "trace_lst",
		"excluded ingestion":      "ingestion_batch",
		"excluded admin mutation": "projects_createApiKey",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
			cs := connect(t, fake)

			got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{"operationId": id}))

			if got.Code != "operation_not_found" || got.OperationID != id || got.Hint == "" {
				t.Errorf("error = %+v, want operation_not_found for %s with a hint", got, id)
			}
			// An excluded operation is in the Langfuse API reference: the
			// message says it is out of this server's scope instead.
			if excluded := name != "unknown"; excluded != strings.Contains(got.Message, "not exposed by this server") {
				t.Errorf("message %q, want it to say whether %s is excluded (%v)", got.Message, id, excluded)
			}
			assertNoRequest(t, seen)
		})
	}
}

func TestExecuteReadRejectsInvalidArgumentsNamingTheField(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		args      map[string]any
		wantField string
	}{
		"missing operation ID":         {args: map[string]any{}, wantField: "operationId"},
		"operation ID of a wrong type": {args: map[string]any{"operationId": 42}, wantField: "operationId"},
		"parameters of a wrong type":   {args: map[string]any{"operationId": "trace_list", "parameters": "limit=1"}, wantField: "parameters"},
		"unknown argument":             {args: map[string]any{"operationId": "trace_list", "url": "https://evil.example"}, wantField: "url"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
			cs := connect(t, fake)

			got := toolErrorOf(t, callExecuteRead(t, cs, tc.args))

			if got.Code != "invalid_argument" || !strings.Contains(got.Message, tc.wantField) {
				t.Errorf("error = %+v, want invalid_argument naming %s", got, tc.wantField)
			}
			if strings.Contains(got.Message, "Go struct") || strings.Contains(got.Message, "executeReadInput") {
				t.Errorf("message %q exposes Go internals", got.Message)
			}
			assertNoRequest(t, seen)
		})
	}
}

func TestExecuteReadBoundsTheCallersOperationIDInTheToolError(t *testing.T) {
	t.Parallel()
	fake, _ := fakeLangfuse(t, http.StatusOK, `{}`)
	cs := connect(t, fake)
	long := strings.Repeat("x", 10_000)

	got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{"operationId": long}))

	if len(got.OperationID) > 100 || len(got.Message) > 500 {
		t.Errorf("operationId has %d bytes and message %d bytes, want both bounded (≤ 100, ≤ 500)",
			len(got.OperationID), len(got.Message))
	}
}
