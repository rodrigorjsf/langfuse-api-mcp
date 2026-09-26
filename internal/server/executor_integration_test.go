//go:build integration

package server_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

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

// liveLeakOptions lets TestMain tolerate the idle keep-alive connections the
// live Langfuse client holds open after the tests, as it does in production;
// the client has no way to close them. Only when a live Langfuse is configured:
// without one, the integration build checks for leaks like the default build.
func liveLeakOptions() []goleak.Option {
	if os.Getenv(envTestBaseURL) == "" {
		return nil
	}
	return []goleak.Option{
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
	}
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
	client := langfuse.New(langfuse.Options{Host: host, PublicKey: keys.PublicKey, SecretKey: keys.SecretKey})
	return connectServer(t, client, slog.New(slog.DiscardHandler), keys)
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

func TestLiveLangfuseAnswersTheHealthCheckThroughTheExecutor(t *testing.T) {
	t.Parallel()
	cs := liveSession(t)

	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	liveData(t, readLive(t, cs, healthRead, waitRetryAfter), &health)

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
	liveData(t, readLive(t, cs, map[string]any{"operationId": "projects_get"}, waitRetryAfter), &projects)

	if len(projects.Data) != 1 || projects.Data[0].ID == "" {
		t.Fatalf("projects = %+v, want exactly the one project of the key pair", projects.Data)
	}
}

// waitRetryAfter waits d or until ctx is done: readLive's wait against a
// live Langfuse.
func waitRetryAfter(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
