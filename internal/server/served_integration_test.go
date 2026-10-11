//go:build integration

package server_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

// Served parameters and the filter scan (spec #172, tickets #173 and #183)
// against the pinned 3.80.0 deployment, which ignores scores_get-many's traceId
// and answers every score of the project (campaign finding H1). The operation
// description does not list traceId there, and a call that carries it is
// answered by a filter scan: the server reads the scores without the filter
// and returns only the asked trace's.

// scanScoresReadBackTimeout bounds how long the test waits for Langfuse to
// serve the scores it accepted: ingestion is asynchronous.
const scanScoresReadBackTimeout = 90 * time.Second

// liveScanSession starts the server over the catalog the pinned 3.80.0
// deployment resolves to, and skips any other deployment.
func liveScanSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
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
	return startCatalog(t, resolved, client, slog.New(slog.DiscardHandler), keys, detected.Profile)
}

// liveScanRow is a row of a filter scan as far as the live tests read it.
type liveScanRow struct {
	ID      string `json:"id"`
	TraceID string `json:"traceId"`
	Level   string `json:"level"`
}

// liveScanPage is a filter scan result as far as the live tests read it.
type liveScanPage struct {
	Data []liveScanRow `json:"data"`
	Meta struct {
		FilterScan struct {
			AppliedBy   string `json:"appliedBy"`
			Parameter   string `json:"parameter"`
			RowField    string `json:"rowField"`
			Complete    bool   `json:"complete"`
			RowsMatched int    `json:"rowsMatched"`
		} `json:"filterScan"`
	} `json:"meta"`
}

// ingestLive sends a batch of events straight to the live Langfuse.
func ingestLive(t *testing.T, events ...map[string]any) {
	t.Helper()
	batch := make([]any, len(events))
	for i, e := range events {
		batch[i] = e
	}
	status, body := langfuseDirect(t.Context(), t, http.MethodPost, "/api/public/ingestion", map[string]any{"batch": batch})
	if status != http.StatusOK && status != http.StatusMultiStatus {
		t.Fatalf("ingestion answered %d %s, want 200 or 207", status, body)
	}
}

// liveScanUntil runs the scan until it returns at least want rows, which waits
// out the asynchronous ingestion, and returns the last page.
func liveScanUntil(t *testing.T, cs *mcp.ClientSession, args map[string]any, want int) liveScanPage {
	t.Helper()
	deadline := time.Now().Add(scanScoresReadBackTimeout)
	for {
		var page liveScanPage
		liveData(t, readLive(t, cs, args, sleepCtx), &page)
		if len(page.Data) >= want {
			return page
		}
		if time.Now().After(deadline) {
			t.Fatalf("the rows were not served within %v of their creation: %d of %d", scanScoresReadBackTimeout, len(page.Data), want)
		}
		if err := sleepCtx(t.Context(), 2*time.Second); err != nil {
			t.Fatalf("waiting for the rows: %v", err)
		}
	}
}

// liveEvent is one event of an ingestion batch, stamped now.
func liveEvent(id, typ string, body map[string]any) map[string]any {
	return map[string]any{"id": "ev-" + id, "type": typ, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "body": body}
}

func TestLiveLangfuse3800AFilterScanOfScoresByTraceReturnsOnlyThatTracesScores(t *testing.T) {
	t.Parallel()
	cs := liveScanSession(t)
	if names := describedParameters(t, cs, "scores_get-many"); slices.Contains(names, "traceId") {
		t.Errorf("describe_operation lists %v on 3.80.0, want no traceId", names)
	}

	// Two traces: the asked one has two scores, the other has one.
	run := randomHex(t, 6)
	traceA, traceB := "it-scan-a-"+run, "it-scan-b-"+run
	scoreIDs := []string{"it-scan-a1-" + run, "it-scan-a2-" + run, "it-scan-b1-" + run}
	t.Cleanup(func() {
		for _, id := range scoreIDs {
			status, body := langfuseDirect(context.WithoutCancel(t.Context()), t, http.MethodDelete, "/api/public/scores/"+id, nil)
			t.Logf("cleanup: DELETE /api/public/scores/%s → %d %s", id, status, body)
		}
	})
	score := func(id, traceID string) map[string]any {
		return liveEvent(id, "score-create", map[string]any{
			"id": id, "traceId": traceID, "name": "it-scan", "value": 1, "dataType": "NUMERIC",
		})
	}
	ingestLive(t,
		liveEvent(traceA, "trace-create", map[string]any{"id": traceA, "name": "it-scan"}),
		liveEvent(traceB, "trace-create", map[string]any{"id": traceB, "name": "it-scan"}),
		score(scoreIDs[0], traceA), score(scoreIDs[1], traceA), score(scoreIDs[2], traceB))

	page := liveScanUntil(t, cs, map[string]any{
		"operationId": "scores_get-many", "parameters": map[string]any{"traceId": traceA},
	}, 2)

	var ids []string
	for _, row := range page.Data {
		if row.TraceID != traceA {
			t.Errorf("row %s belongs to trace %q, want only the scores of %s", row.ID, row.TraceID, traceA)
		}
		ids = append(ids, row.ID)
	}
	slices.Sort(ids)
	if want := []string{scoreIDs[0], scoreIDs[1]}; !slices.Equal(ids, want) {
		t.Errorf("scores = %v, want exactly %v", ids, want)
	}
	scan := page.Meta.FilterScan
	if scan.AppliedBy != "server" || scan.Parameter != "traceId" || scan.RowField != "traceId" || !scan.Complete || scan.RowsMatched != 2 {
		t.Errorf("filter scan summary = %+v, want applied by the server on traceId, complete, 2 rows matched", scan)
	}
}

// The second pair of the closed list: the observations of one level, on a
// deployment that ignores level (it serves it from 3.82.0). Langfuse 3.80.0 has
// no way to delete an observation, so the throwaway stack keeps these two.
func TestLiveLangfuse3800AFilterScanOfLegacyObservationsByLevelReturnsOnlyThatLevel(t *testing.T) {
	t.Parallel()
	cs := liveScanSession(t)
	run := randomHex(t, 6)
	trace, failing, fine := "it-scan-lvl-"+run, "it-scan-err-"+run, "it-scan-ok-"+run
	observation := func(id, level string) map[string]any {
		return liveEvent(id, "span-create", map[string]any{
			"id": id, "traceId": trace, "name": "it-scan", "level": level,
			"startTime": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	ingestLive(t, liveEvent(trace, "trace-create", map[string]any{"id": trace, "name": "it-scan"}),
		observation(failing, "ERROR"), observation(fine, "DEFAULT"))

	page := liveScanUntil(t, cs, map[string]any{
		"operationId": "legacy_observationsV1_getMany",
		"parameters":  map[string]any{"level": "ERROR", "traceId": trace},
	}, 1)

	if len(page.Data) != 1 || page.Data[0].ID != failing || page.Data[0].Level != "ERROR" {
		t.Errorf("rows = %+v, want only the ERROR observation %s", page.Data, failing)
	}
	scan := page.Meta.FilterScan
	if scan.AppliedBy != "server" || scan.Parameter != "level" || scan.RowField != "level" || !scan.Complete || scan.RowsMatched != 1 {
		t.Errorf("filter scan summary = %+v, want applied by the server on level, complete, 1 row matched", scan)
	}
}
