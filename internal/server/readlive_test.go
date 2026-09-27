package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// readLive is how the integration suite (build tag integration) calls
// execute_read against a live Langfuse, and liveTarget.send how its setup
// calls Langfuse directly. Both live in the default build so that their
// rate-limit behaviour is proven on every OS against the fake Langfuse,
// without a live one.

// healthRead returns the execute_read call of Langfuse's health check, fresh
// on every call: parallel tests share no mutable map.
func healthRead() map[string]any { return map[string]any{"operationId": "health_health"} }

// sleepCtx waits d or until ctx is done: readLive's Retry-After wait and the
// seeding poll's interval against a live Langfuse.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

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

// liveTarget is a Langfuse the suite calls directly, with a key pair, for the
// setup steps execute_read cannot do: seeding over OTLP and deleting traces.
type liveTarget struct {
	baseURL, publicKey, secretKey string
}

// otlpTracesPath is Langfuse's OTLP/HTTP traces endpoint, the suite's seeding
// path; send asks it for ingestion version 4 (the v4 event pipeline).
const otlpTracesPath = "/api/public/otel/v1/traces"

// send sends one request to the target and returns its status and the start
// of its body. Like readLive, it waits a 429's Retry-After once and calls
// again, and never retries a 429 that named no wait. It trusts the system
// roots only: a self-hosted test Langfuse behind a private CA would fail here,
// at seeding, before any probe runs.
func (lt liveTarget) send(ctx context.Context, t *testing.T, method, path string, body any, wait func(context.Context, time.Duration) error) (int, string) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode %s %s: %v", method, path, err)
	}
	status, text, retryAfter := lt.do(ctx, t, method, path, payload)
	seconds, err := strconv.Atoi(retryAfter)
	if status != http.StatusTooManyRequests || err != nil || seconds <= 0 {
		return status, text
	}
	d := time.Duration(seconds) * time.Second
	if d > maxRetryAfterWait {
		t.Fatalf("Langfuse asks to wait %v before %s %s, more than the suite's %v", d, method, path, maxRetryAfterWait)
	}
	t.Logf("Langfuse rate-limited %s %s: waiting Retry-After %v", method, path, d)
	if err := wait(ctx, d); err != nil {
		t.Fatalf("waiting for Retry-After: %v", err)
	}
	status, text, _ = lt.do(ctx, t, method, path, payload)
	return status, text
}

// do sends one request and returns its status, the start of its body and its
// Retry-After header.
func (lt liveTarget) do(ctx context.Context, t *testing.T, method, path string, payload []byte) (status int, body, retryAfter string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// G704: the URL is the operator's test base URL plus a constant path.
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(lt.baseURL, "/")+path, bytes.NewReader(payload)) //nolint:gosec // see above
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.SetBasicAuth(lt.publicKey, lt.secretKey)
	req.Header.Set("Content-Type", "application/json")
	if path == otlpTracesPath {
		// Without it a 4.x `dual` deployment leaves OTLP spans out of
		// /v2/observations for minutes (#85); events_only and Cloud accept it.
		req.Header.Set("X-Langfuse-Ingestion-Version", "4")
	}
	// Its own transport, closed afterwards: no idle connection outlives the
	// call to trip TestMain's leak check.
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	defer transport.CloseIdleConnections()
	resp, err := (&http.Client{Transport: transport}).Do(req) //nolint:gosec // G704: the test's own Langfuse, see above
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()               // a read-only body: nothing to report on close
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096)) // best effort: the body only explains a failure
	return resp.StatusCode, string(text), resp.Header.Get("Retry-After")
}

func TestTheSuitesDirectSetupCallWaitsForRetryAfterOnA429(t *testing.T) {
	t.Parallel()
	fake, calls := scriptedLangfuse(t, rateLimited("7"), answer{status: http.StatusOK, body: `{}`})
	var wait fakeWait

	status, _ := liveTarget{baseURL: fake.URL}.send(t.Context(), t, http.MethodPost, "/api/public/otel/v1/traces", map[string]any{}, wait.wait)

	if status != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("after %d requests the setup call got %d, want 200 on the second request", calls.Load(), status)
	}
	if waits := wait.recorded(); len(waits) != 1 || waits[0] != 7*time.Second {
		t.Fatalf("the setup call waited %v, want one 7s wait (Retry-After)", waits)
	}
}

func TestTheSuitesDirectSetupCallNeverRetriesA429ThatNamesNoWait(t *testing.T) {
	t.Parallel()
	fake, calls := scriptedLangfuse(t, rateLimited(""), answer{status: http.StatusOK, body: `{}`})
	var wait fakeWait

	status, _ := liveTarget{baseURL: fake.URL}.send(t.Context(), t, http.MethodDelete, "/api/public/traces", map[string]any{}, wait.wait)

	if status != http.StatusTooManyRequests || calls.Load() != 1 || len(wait.recorded()) != 0 {
		t.Fatalf("after %d requests and waits %v the setup call got %d, want 429 after one request and no wait",
			calls.Load(), wait.recorded(), status)
	}
}

func TestTheSuitesOTLPSeedingAsksForIngestionVersion4(t *testing.T) {
	t.Parallel()
	got := make(chan string, 1)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("X-Langfuse-Ingestion-Version")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fake.Close)

	status, _ := liveTarget{baseURL: fake.URL}.send(t.Context(), t, http.MethodPost, "/api/public/otel/v1/traces", map[string]any{}, sleepCtx)

	if version := <-got; status != http.StatusOK || version != "4" {
		t.Fatalf("the OTLP seeding call got %d and sent x-langfuse-ingestion-version %q, want 200 and \"4\"", status, version)
	}
}
