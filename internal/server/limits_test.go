package server_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// Seam S1: the size limits of a read — the most the server reads from
// Langfuse, and the most it returns to the agent.

func TestAResponseAboveTheMaximumBytesReadIsRefusedAsResponseTooLarge(t *testing.T) {
	t.Parallel()
	tests := map[string]answer{
		"a body above 5 MiB": {status: http.StatusOK, body: `{"data":"` + strings.Repeat("a", 5<<20) + `"}`},
		"a 413 from Langfuse": {status: http.StatusRequestEntityTooLarge,
			body: `{"message":"Response too large"}`},
	}
	for name, a := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, calls := scriptedLangfuse(t, a)
			cs := connect(t, fake)

			got := toolErrorOf(t, callExecuteRead(t, cs, traceList)).Error

			if got.Code != "response_too_large" || got.Retryable || got.OperationID != "trace_list" ||
				!strings.Contains(got.Hint, "limit") || calls.Load() != 1 {
				t.Fatalf("tool error = %+v after %d requests, want response_too_large, not retryable, "+
					"a hint to narrow the query, after 1 request", got, calls.Load())
			}
		})
	}
}

func TestAListOperationWithoutALimitGetsTheDefaultLimit(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[],"meta":{}}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{"operationId": "trace_list"})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	if got := receivedOne(t, seen).query.Get("limit"); got != "50" {
		t.Fatalf("Langfuse received limit %q, want the default 50", got)
	}
}

func TestALimitOutsideOneToTheCapIsRefusedWithAHintWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{101, 1000, 0, -5} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
			cs := connect(t, fake)

			got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
				"operationId": "observations_getMany", "parameters": map[string]any{"limit": limit},
			})).Error

			if got.Code != "invalid_argument" || !strings.Contains(got.Message, "limit") ||
				!strings.Contains(got.Hint, "100") {
				t.Fatalf("tool error = %+v, want invalid_argument naming limit, with a hint naming the cap 100", got)
			}
			select {
			case r := <-seen:
				t.Fatalf("Langfuse received %s %s?%s for an out-of-range limit", r.method, r.path, r.rawQuery)
			default:
			}
		})
	}
}

func TestAResultAboveTheMaximumReturnedBytesIsTruncatedWithAMarkerAndAHint(t *testing.T) {
	t.Parallel()
	row := `{"id":"obs-1","input":"<p>\"quoted\" & long text é</p>"},`
	body := `{"data":[` + strings.Repeat(row, 5000) + `{"id":"last"}],"meta":{"cursor":"next"}}`
	fake, _ := fakeLangfuse(t, http.StatusOK, body)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{"operationId": "observations_getMany"})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res)[:200])
	}
	text := resultText(t, res)
	if len(text) > 100<<10 {
		t.Fatalf("result is %d bytes, want at most 102400", len(text))
	}
	var env struct {
		Label     string `json:"label"`
		Truncated bool   `json:"truncated"`
		Hint      string `json:"hint"`
		Data      string `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("truncated result is not the envelope with the payload as text: %v", err)
	}
	if !env.Truncated || !strings.Contains(env.Hint, "limit") || env.Label == "" ||
		!strings.HasPrefix(env.Data, `{"data":[{"id":"obs-1"`) || !strings.HasSuffix(env.Data, "… [truncated]") {
		t.Fatalf("envelope = {truncated: %v, hint: %q, label: %q, data: %q…%q}, want truncated, a hint to narrow "+
			"the query, the label, and the payload's start ending with the truncation marker",
			env.Truncated, env.Hint, env.Label, env.Data[:min(40, len(env.Data))], env.Data[max(0, len(env.Data)-20):])
	}
}
