//go:build integration

package server_test

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

// Served parameters (spec #172, ticket #173) against the pinned 3.80.0
// deployment, which ignores scores_get-many's traceId and answers every score
// of the project (campaign finding H1): the server refuses the parameter
// before any request, naming the release that serves it.
func TestLiveLangfuse3800RefusesTheTraceIdOfScoresGetMany(t *testing.T) {
	t.Parallel()
	if os.Getenv(envTestDeployment) != "3.80.0" {
		t.Skipf("%s is not 3.80.0: this check needs the pinned 3.80.0 deployment", envTestDeployment)
	}
	client, keys := liveClient(t, langfuse.Options{RateLimit: liveRateLimit})
	detected := client.DetectProfile(t.Context(), liveDetectionBudget)
	version, _ := detected.Profile.KnownVersion()
	all, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	resolved := all.Resolve(catalog.Profile{Version: version, Families: detected.Profile.Families})
	cs := startCatalog(t, resolved, client, slog.New(slog.DiscardHandler), keys, detected.Profile)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "scores_get-many", "parameters": map[string]any{"traceId": "it-missing"},
	})

	got := toolErrorOf(t, res).Error
	if got.Code != "invalid_argument" {
		t.Fatalf("tool error = %+v, want invalid_argument", got)
	}
	for _, want := range []string{`"traceId"`, "scores_get-many", "Langfuse 3.80.0", "from 3.133.0"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message %q does not contain %q", got.Message, want)
		}
	}
	if names := describedParameters(t, cs, "scores_get-many"); strings.Contains(strings.Join(names, ","), "traceId") {
		t.Errorf("describe_operation lists %v on 3.80.0, want no traceId", names)
	}
}
