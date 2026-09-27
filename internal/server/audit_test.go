package server_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam S1: the audit line an operator reads on stderr for every tool call.

// auditLines returns the JSON lines the server logged.
func auditLines(t *testing.T, logs *syncBuffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.Lines(strings.TrimSpace(logs.String())) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stderr line is not JSON: %v\n%s", err, line)
		}
		lines = append(lines, m)
	}
	return lines
}

func TestEveryToolCallLogsExactlyOneAuditLineWithoutThePayload(t *testing.T) {
	t.Parallel()
	const payload = `{"data":[{"id":"trace-1","input":"SECRET-PAYLOAD-TEXT"}]}`
	tests := map[string]struct {
		status     int
		body       string
		args       map[string]any
		wantStatus float64
		wantBytes  float64
		wantMethod string
		wantCode   string
	}{
		"a successful read": {status: http.StatusOK, body: payload, args: traceList,
			wantStatus: 200, wantBytes: float64(len(payload)), wantMethod: "GET"},
		"a Langfuse error": {status: http.StatusBadRequest, body: `{"message":"SECRET-PAYLOAD-TEXT"}`, args: traceList,
			wantStatus: 400, wantMethod: "GET", wantCode: "langfuse_bad_request"},
		"a response too large to read": {status: http.StatusOK, body: `{"data":"` + strings.Repeat("a", 5<<20) + `"}`,
			args: traceList, wantStatus: 200, wantMethod: "GET", wantCode: "response_too_large"},
		"an unknown operation": {status: http.StatusOK, body: payload,
			args: map[string]any{"operationId": "no_such_operation"}, wantCode: "operation_not_found"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, _ := fakeLangfuse(t, tc.status, tc.body)
			var logs syncBuffer
			cs := connectClient(t, langfuse.New(clientOptions(t, fake)), slog.New(slog.NewJSONHandler(&logs, nil)))

			callExecuteRead(t, cs, tc.args)

			lines := auditLines(t, &logs)
			if len(lines) != 1 {
				t.Fatalf("logged %d lines, want exactly 1:\n%s", len(lines), logs.String())
			}
			got := lines[0]
			if got["tool"] != "execute_read" || got["operationId"] != tc.args["operationId"] ||
				got["method"] != tc.wantMethod || got["status"] != tc.wantStatus || got["bytes"] != tc.wantBytes {
				t.Errorf("audit line = %v, want tool execute_read, operationId %v, method %q, status %v, bytes %v",
					got, tc.args["operationId"], tc.wantMethod, tc.wantStatus, tc.wantBytes)
			}
			if code, _ := got["code"].(string); code != tc.wantCode {
				t.Errorf("audit line code = %q, want %q", code, tc.wantCode)
			}
			if _, ok := got["latencyMs"].(float64); !ok {
				t.Errorf("audit line has no latencyMs: %v", got)
			}
			if strings.Contains(logs.String(), "SECRET-PAYLOAD-TEXT") {
				t.Errorf("the audit line carries the payload:\n%s", logs.String())
			}
		})
	}
}

func TestAPanickingToolCallStillLogsExactlyOneAuditLine(t *testing.T) {
	t.Parallel()
	var logs syncBuffer
	// A nil Langfuse client makes the handler dereference nil: a real bug path.
	cs := connectClient(t, nil, slog.New(slog.NewJSONHandler(&logs, nil)))

	callExecuteRead(t, cs, traceList)

	lines := auditLines(t, &logs)
	if len(lines) != 1 || lines[0]["code"] != "internal_error" || lines[0]["operationId"] != "trace_list" {
		t.Fatalf("logged %d lines, want exactly 1 audit line with code internal_error:\n%s", len(lines), logs.String())
	}
}

// security.md Audit: requests counts every Langfuse request the call made, so
// a read the client retried counts each attempt that left.
func TestTheAuditLineCountsEveryRetriedLangfuseRequest(t *testing.T) {
	t.Parallel()
	const page = `{"data":[{"id":"a","startTime":"2026-09-25T10:00:00.000Z"}],"meta":{}}`
	unavailable := answer{status: http.StatusServiceUnavailable, body: `{"message":"busy"}`}
	tests := map[string]struct {
		tool         string
		args         map[string]any
		answers      []answer
		wantRequests float64
	}{
		"execute_read retried once, then answered": {tool: "execute_read", args: traceList,
			answers: []answer{unavailable, {status: http.StatusOK, body: page}}, wantRequests: 2},
		"execute_read failing after every retry": {tool: "execute_read", args: traceList,
			answers: []answer{unavailable}, wantRequests: 3},
		"get_trace_tree retried once, then answered": {tool: "get_trace_tree", args: map[string]any{"traceId": "t-1"},
			answers: []answer{unavailable, {status: http.StatusOK, body: page}}, wantRequests: 2},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, calls := scriptedLangfuse(t, tc.answers...)
			var logs syncBuffer
			cs := startServer(t, langfuse.New(testOptions(t, fake.URL)), slog.New(slog.NewJSONHandler(&logs, nil)),
				server.Secrets{Keys: testKeys()}, v4Profile)

			callTool(t, cs, tc.tool, tc.args)

			lines := auditLines(t, &logs)
			if len(lines) != 1 {
				t.Fatalf("logged %d lines, want exactly 1:\n%s", len(lines), logs.String())
			}
			if got := lines[0]["requests"]; got != tc.wantRequests || float64(calls.Load()) != tc.wantRequests {
				t.Errorf("audit requests = %v, Langfuse received %d, want both %v", got, calls.Load(), tc.wantRequests)
			}
		})
	}
}
