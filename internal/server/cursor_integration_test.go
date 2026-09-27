//go:build integration

package server_test

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The cursor page-size probe (#93, spec #68 story 37): get_trace_tree reads
// Observations v2 at limit 1000 and, for a longer trace, hands the agent the
// next cursor to continue through execute_read, whose limit is capped at 100.
// That is safe only if the cursor does not depend on the page size it was
// issued for. The probe seeds one small trace and proves the same property
// with small numbers: a cursor issued at limit 2 and continued at limit 1
// reads exactly the rows a constant limit reads, in the same order.
// Finding and cursor format: docs/research/langfuse.md §1 (pagination).
//
// Seeding and cleanup call Langfuse directly (see iowindow_integration_test.go):
// trace ingestion over OTLP is out of the server's scope and deletes are writes.

// cursorSpans is the number of spans of the seeded trace. Two of them share
// one start time, so the continuation also crosses a sort-key tie.
const cursorSpans = 5

func TestLiveObservationsCursorContinuesAtADifferentLimitWithoutGapsOrRepeats(t *testing.T) {
	t.Parallel()
	cs := liveSession(t)
	traceID := seedCursorTrace(t, cs)

	constant := pageTrace(t, cs, traceID, 2, 2)
	changed := pageTrace(t, cs, traceID, 2, 1)

	t.Logf("limit 2 throughout: %v; limit 2 then 1: %v", constant, changed)
	if len(constant) != cursorSpans {
		t.Fatalf("paging at limit 2 read %d observations %v, want the %d seeded", len(constant), constant, cursorSpans)
	}
	if !slices.Equal(changed, constant) {
		t.Fatalf("a cursor issued at limit 2 and continued at limit 1 read %v, want %v: the cursor depends on the page size",
			changed, constant)
	}
}

// pageTrace reads every observation of traceID through execute_read: the
// first page at firstLimit, every next page, from the previous page's
// meta.cursor, at restLimit. It returns the observation IDs in the order read
// and fails on a repeated ID.
func pageTrace(t *testing.T, cs *mcp.ClientSession, traceID string, firstLimit, restLimit int) []string {
	t.Helper()
	var ids []string
	params := map[string]any{"traceId": traceID, "fields": "core", "limit": firstLimit}
	for range cursorSpans + 1 { // one more page than rows: the last page proves the end
		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Meta struct {
				Cursor *string `json:"cursor"`
			} `json:"meta"`
		}
		liveData(t, readLive(t, cs, map[string]any{"operationId": "observations_getMany", "parameters": params}, sleepCtx), &page)
		for _, row := range page.Data {
			if slices.Contains(ids, row.ID) {
				t.Fatalf("observation %s read twice (limit %d then %d; read so far %v)", row.ID, firstLimit, restLimit, ids)
			}
			ids = append(ids, row.ID)
		}
		if page.Meta.Cursor == nil || *page.Meta.Cursor == "" {
			return ids
		}
		params = map[string]any{"traceId": traceID, "fields": "core", "limit": restLimit, "cursor": *page.Meta.Cursor}
	}
	t.Fatalf("still a cursor after %d pages (limit %d then %d; read %v)", cursorSpans+1, firstLimit, restLimit, ids)
	return nil
}

// seedCursorTrace exports one trace of cursorSpans spans through the
// deployment's OTLP endpoint, waits until all are queryable by trace ID, and
// deletes the trace when the test ends.
func seedCursorTrace(t *testing.T, cs *mcp.ClientSession) string {
	t.Helper()
	run := "it-cursor-" + randomHex(t, 4)
	traceID := randomHex(t, 16)
	start := time.Now().UTC().Add(-time.Hour)
	var spans []any
	for i := range cursorSpans {
		at := start.Add(time.Duration(min(i, cursorSpans-2)) * time.Second) // the last two share a start time
		spans = append(spans, map[string]any{
			"traceId": traceID, "spanId": randomHex(t, 8), "name": run + "-" + strconv.Itoa(i), "kind": 1,
			"startTimeUnixNano": strconv.FormatInt(at.UnixNano(), 10),
			"endTimeUnixNano":   strconv.FormatInt(at.Add(500*time.Millisecond).UnixNano(), 10),
			"attributes":        []any{map[string]any{"key": "langfuse.environment", "value": map[string]any{"stringValue": run}}},
		})
	}

	t.Cleanup(func() {
		// t.Context() is already cancelled when cleanups run; a failed delete
		// is reported only: the trace ID is unique to this run.
		status, body := langfuseDirect(context.WithoutCancel(t.Context()), t, http.MethodDelete, "/api/public/traces",
			map[string]any{"traceIds": []string{traceID}})
		t.Logf("cleanup: DELETE /api/public/traces (seeded trace) → %d %s", status, body)
	})
	status, body := langfuseDirect(t.Context(), t, http.MethodPost, "/api/public/otel/v1/traces", map[string]any{
		"resourceSpans": []any{map[string]any{
			"resource":   map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "langfuse-mcp-integration"}}}},
			"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": "cursor-probe"}, "spans": spans}},
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("OTLP export answered %d: %s", status, body)
	}

	// Ingestion is asynchronous (about 10 s on self-hosted).
	deadline := time.Now().Add(3 * time.Minute)
	for {
		rows := probeRows(t, cs, map[string]any{"traceId": traceID, "fields": "core", "limit": 100})
		if len(rows) == cursorSpans {
			return traceID
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 3 minutes %d of %d seeded observations are queryable", len(rows), cursorSpans)
		}
		if err := sleepCtx(t.Context(), 6*time.Second); err != nil {
			t.Fatalf("waiting for ingestion: %v", err)
		}
	}
}
