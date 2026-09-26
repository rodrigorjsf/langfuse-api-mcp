//go:build integration

package server_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam S3 (spec #8): the integration suite drives the generic executor through
// execute_read, the executor's public interface, with the real catalog and the
// real Langfuse HTTP client, against a live Langfuse: a self-hosted instance
// from the official compose setup (scripts/langfuse-selfhosted.sh) or the
// dedicated Langfuse Cloud test project (scripts/setup-ci-langfuse-cloud.sh).
// It compiles only with the build tag `integration`.

// The live Langfuse the suite runs against. Never a project with real data:
// the suite may create and delete objects in it (.claude/rules/testing.md).
const (
	envTestBaseURL   = "LANGFUSE_TEST_BASE_URL"
	envTestPublicKey = "LANGFUSE_TEST_PUBLIC_KEY"
	envTestSecretKey = "LANGFUSE_TEST_SECRET_KEY" //nolint:gosec // G101: the variable's name, not a key
)

// The live client keeps its idle keep-alive connections to Langfuse open after
// the tests, as it does in production; they are not leaks.
func init() {
	leakOptions = append(leakOptions,
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
	)
}

// liveSession connects an in-memory MCP client to the real server, built with
// the real catalog, executor and Langfuse client, pointed at the live Langfuse
// named by the LANGFUSE_TEST_* variables. It skips the test when any is unset.
func liveSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	var missing []string
	for _, name := range []string{envTestBaseURL, envTestPublicKey, envTestSecretKey} {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Skipf("no live Langfuse: %s not set; start one with scripts/langfuse-selfhosted.sh up, "+
			"or load the Cloud test project's .env.integration (README, For contributors)", strings.Join(missing, ", "))
	}
	host, err := url.Parse(os.Getenv(envTestBaseURL))
	if err != nil {
		t.Fatalf("%s is not a URL: %v", envTestBaseURL, err)
	}
	keys := server.Secrets{PublicKey: os.Getenv(envTestPublicKey), SecretKey: os.Getenv(envTestSecretKey)}
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	client := langfuse.New(langfuse.Options{Host: host, PublicKey: keys.PublicKey, SecretKey: keys.SecretKey})
	srv := server.New(cat, client, slog.New(slog.DiscardHandler), keys)

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() }) // closing an already-closed session is harmless
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "integration-suite", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// liveData decodes the Langfuse payload inside the untrusted-data envelope of
// a successful execute_read result into v.
func liveData(t *testing.T, res *mcp.CallToolResult, v any) {
	t.Helper()
	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	var env sanitize.Envelope
	if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
		t.Fatalf("result is not the untrusted-data envelope: %v", err)
	}
	if err := json.Unmarshal(env.Data, v); err != nil {
		t.Fatalf("decode the Langfuse payload: %v", err)
	}
}

// sleep waits d or until ctx is done: readLive's wait against a live Langfuse.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// healthRead is the execute_read call of Langfuse's health check.
var healthRead = map[string]any{"operationId": "health_health"}

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

	res := readLive(t, cs, healthRead, suiteWait.wait)

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

	got := toolErrorOf(t, readLive(t, cs, healthRead, suiteWait.wait)).Error

	if got.Code != "langfuse_rate_limited" || calls.Load() != 1 || len(suiteWait.recorded()) != 0 {
		t.Fatalf("after %d requests and waits %v the suite got %+v, want langfuse_rate_limited after one request and no wait",
			calls.Load(), suiteWait.recorded(), got)
	}
}

func TestLiveLangfuseAnswersTheHealthCheckThroughTheExecutor(t *testing.T) {
	t.Parallel()
	cs := liveSession(t)

	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	liveData(t, readLive(t, cs, healthRead, sleep), &health)

	if health.Status != "OK" || health.Version == "" {
		t.Fatalf("health = %+v, want status OK and a version", health)
	}
	t.Logf("Langfuse %s at %s", health.Version, os.Getenv(envTestBaseURL))
}

func TestLiveLangfuseAnswersAnAuthenticatedReadWithTheKeysProject(t *testing.T) {
	t.Parallel()
	cs := liveSession(t)

	var projects struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	liveData(t, readLive(t, cs, map[string]any{"operationId": "projects_get"}, sleep), &projects)

	if len(projects.Data) != 1 || projects.Data[0].ID == "" {
		t.Fatalf("projects = %+v, want exactly the one project of the key pair", projects.Data)
	}
}
