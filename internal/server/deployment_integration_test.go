//go:build integration

package server_test

import (
	"log/slog"
	"maps"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

// The per-deployment check (spec #68, ticket #73; ADR-0012 Consequences):
// against a pinned self-hosted deployment, startup detects that deployment's
// profile, and every read operation of the catalog resolved for it answers
// with something other than operation_unavailable. The weekly integration run
// starts each pinned deployment in turn (scripts/langfuse-selfhosted.sh with
// LANGFUSE_DEPLOYMENT); pull requests run 4.46.0-events_only only.

// envTestDeployment names the pinned deployment the live Langfuse is
// (a key of pinnedDeployments). scripts/langfuse-selfhosted.sh writes it next
// to the LANGFUSE_TEST_* variables; the Cloud test project has no pin.
const envTestDeployment = "LANGFUSE_TEST_DEPLOYMENT"

// liveDetectionBudget is the detection budget of the check: longer than the
// executable's 5 s, since the check proves the profile, not the budget, and a
// freshly started Langfuse can be slow to answer its first requests.
const liveDetectionBudget = 30 * time.Second

// liveRateLimit is the rate limit of the check's client: the executable's
// default for a host other than Langfuse Cloud (#47).
const liveRateLimit = 1000

func TestLiveDeploymentAnswersEveryReadOperationOfItsResolvedCatalog(t *testing.T) {
	t.Parallel()
	deployment := os.Getenv(envTestDeployment)
	if deployment == "" {
		t.Skipf("%s not set: the live Langfuse is not a pinned deployment (scripts/langfuse-selfhosted.sh writes it)", envTestDeployment)
	}
	want, ok := pinnedDeployments[deployment]
	if !ok {
		t.Fatalf("%s names no pinned deployment; the pins are %v", envTestDeployment, slices.Sorted(maps.Keys(pinnedDeployments)))
	}
	client, keys := liveClient(t, langfuse.Options{RateLimit: liveRateLimit})

	detected := client.DetectProfile(t.Context(), liveDetectionBudget)
	if mismatch := profileMismatch(deployment, want, detected); mismatch != "" {
		t.Fatal(mismatch)
	}
	all, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	// As startup does (cmd/langfuse-mcp): only a plain version crosses.
	version, _ := detected.Profile.KnownVersion()
	resolved := all.Resolve(catalog.Profile{Version: version, Families: detected.Profile.Families})
	cs := startCatalog(t, resolved, client, slog.New(slog.DiscardHandler), keys, detected.Profile)
	reads := readsOf(resolved)

	for _, failure := range unreachableReads(t, cs, deployment, reads, time.Now()) {
		t.Error(failure)
	}
	t.Logf("deployment %s: %d read operations of %d resolved operations checked", deployment, len(reads), len(resolved.Operations()))
}
