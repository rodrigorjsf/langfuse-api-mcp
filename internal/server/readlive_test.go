package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// readLive is how the integration suite (executor_integration_test.go, build
// tag integration) calls execute_read against a live Langfuse. It lives in
// the default build so that its rate-limit behaviour is proven on every OS
// against the fake Langfuse, without a live one.

// healthRead returns the execute_read call of Langfuse's health check, fresh
// on every call: parallel tests share no mutable map.
func healthRead() map[string]any { return map[string]any{"operationId": "health_health"} }

// maxRetryAfterWait bounds the one wait readLive makes on a Retry-After: a
// Langfuse Cloud rate-limit window is one minute on the Hobby plan.
const maxRetryAfterWait = 2 * time.Minute

// readLive calls execute_read with args. When Langfuse still answers 429 after
// the client's own retry, it waits the tool error's retryAfterSeconds once and
// calls again. It never retries a 429 that named no wait, nor retries twice:
// the Cloud run spends as few calls of the test project's budget as it can.
func readLive(t *testing.T, cs *mcp.ClientSession, args map[string]any, wait func(context.Context, time.Duration) error) *mcp.CallToolResult {
	t.Helper()
	res := callExecuteRead(t, cs, args)
	if !res.IsError {
		return res
	}
	got := toolErrorOf(t, res).Error
	if got.Code != "langfuse_rate_limited" || got.RetryAfterSeconds <= 0 {
		return res
	}
	d := time.Duration(got.RetryAfterSeconds) * time.Second
	if d > maxRetryAfterWait {
		t.Fatalf("Langfuse asks to wait %v before the next call, more than the suite's %v", d, maxRetryAfterWait)
	}
	t.Logf("Langfuse rate-limited %v: waiting Retry-After %v", args["operationId"], d)
	if err := wait(t.Context(), d); err != nil {
		t.Fatalf("waiting for Retry-After: %v", err)
	}
	return callExecuteRead(t, cs, args)
}

func TestTheSuiteWaitsForRetryAfterWhenLangfuseStillRateLimitsAfterTheClientsRetry(t *testing.T) {
	t.Parallel()
	fake, calls := scriptedLangfuse(t, rateLimited("7"), rateLimited("7"), answer{status: http.StatusOK, body: `{"status":"OK"}`})
	var clientWait, suiteWait fakeWait
	cs := connectFakeTime(t, fake, &clientWait, 0)

	res := readLive(t, cs, healthRead(), suiteWait.wait)

	if res.IsError || calls.Load() != 3 {
		t.Fatalf("after %d requests the suite got %s, want the 200 answer on the third request", calls.Load(), resultText(t, res))
	}
	if waits := suiteWait.recorded(); len(waits) != 1 || waits[0] != 7*time.Second {
		t.Fatalf("the suite waited %v, want one 7s wait (Retry-After)", waits)
	}
}

func TestTheSuiteNeverRetriesA429ThatNamesNoWait(t *testing.T) {
	t.Parallel()
	fake, calls := scriptedLangfuse(t, rateLimited(""), answer{status: http.StatusOK, body: `{"status":"OK"}`})
	var clientWait, suiteWait fakeWait
	cs := connectFakeTime(t, fake, &clientWait, 0)

	got := toolErrorOf(t, readLive(t, cs, healthRead(), suiteWait.wait)).Error

	if got.Code != "langfuse_rate_limited" || calls.Load() != 1 || len(suiteWait.recorded()) != 0 {
		t.Fatalf("after %d requests and waits %v the suite got %+v, want langfuse_rate_limited after one request and no wait",
			calls.Load(), suiteWait.recorded(), got)
	}
}
