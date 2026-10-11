//go:build integration

package server_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The legacy trace tree (spec #172, ticket #187) against the pinned 3.80.0
// deployment, which serves the legacy family and not the v4 read family:
// get_trace_tree reads the observations of a seeded trace through the legacy
// observations list (GET /api/public/observations, filtered by traceId), orders
// them as a trace tree and removes input, output and metadata unless asked.
// Seeding calls Langfuse directly; 3.80.0 has no way to delete an observation,
// so the throwaway stack keeps them.

// legacyTreeReadBackTimeout bounds how long a test waits for Langfuse to serve
// the few observations it accepted; legacyTreePagesReadBackTimeout does for the
// 130 of the paging test, which its queue serves in minutes: ingestion is
// asynchronous.
const (
	legacyTreeReadBackTimeout      = 90 * time.Second
	legacyTreePagesReadBackTimeout = 6 * time.Minute
)

// seedLegacyTree ingests one trace of four observations,
//
//	root      0 s, 2 s long
//	├─ child  1 s
//	│  └─ leaf 2 s
//	└─ later  3 s
//
// each with an input, an output and metadata, and returns the trace ID.
func seedLegacyTree(t *testing.T) string {
	t.Helper()
	run := randomHex(t, 6)
	trace := "it-tree-" + run
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	span := func(name, parent string, startOffset, lengthSeconds int) map[string]any {
		start := base.Add(time.Duration(startOffset) * time.Second)
		body := map[string]any{
			"id": trace + "-" + name, "traceId": trace, "name": name,
			"startTime": start.Format(time.RFC3339Nano), "endTime": start.Add(time.Duration(lengthSeconds) * time.Second).Format(time.RFC3339Nano),
			"input": "input of " + name, "output": "output of " + name, "metadata": map[string]any{"k": "metadata of " + name},
		}
		if parent != "" {
			body["parentObservationId"] = trace + "-" + parent
		}
		return liveEvent(trace+"-"+name, "span-create", body)
	}
	ingestLive(t,
		liveEvent(trace, "trace-create", map[string]any{"id": trace, "name": "it-tree"}),
		span("later", "root", 3, 1), span("leaf", "child", 2, 1), span("child", "root", 1, 1), span("root", "", 0, 2))
	return trace
}

// liveTreeUntil calls get_trace_tree until it has read want observations
// (meta.observations, which the result size cap does not change), which waits
// out the asynchronous ingestion, and returns the last answer.
func liveTreeUntil(t *testing.T, cs *mcp.ClientSession, args map[string]any, want int, timeout time.Duration) traceTreeEnvelope {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		res := callTool(t, cs, "get_trace_tree", args)
		if res.IsError {
			t.Fatalf("get_trace_tree returned a tool error: %s", resultText(t, res))
		}
		var env traceTreeEnvelope
		if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
			t.Fatalf("result is not the enveloped trace tree: %v", err)
		}
		if env.Data.Meta.Observations >= want {
			return env
		}
		if time.Now().After(deadline) {
			t.Fatalf("the observations were not served within %v of their creation: %d of %d", timeout, env.Data.Meta.Observations, want)
		}
		if err := sleepCtx(t.Context(), 2*time.Second); err != nil {
			t.Fatalf("waiting for the observations: %v", err)
		}
	}
}

func TestLiveLangfuse3800GetTraceTreeBuildsTheTraceTreeOfASeededTraceFromTheLegacyList(t *testing.T) {
	t.Parallel()
	cs := liveScanSession(t)
	trace := seedLegacyTree(t)

	env := liveTreeUntil(t, cs, map[string]any{"traceId": trace}, 4, legacyTreeReadBackTimeout)

	var got []string
	for _, row := range env.Data.Data {
		depth, _ := row["depth"].(float64)
		got = append(got, fmt.Sprintf("%s@%d", row["name"], int(depth)))
	}
	if want := []string{"root@0", "child@1", "leaf@2", "later@1"}; !slices.Equal(got, want) {
		t.Fatalf("trace tree = %v, want %v", got, want)
	}
	if env.OperationID != "legacy_observationsV1_getMany" || env.Truncated || env.Data.Meta.Observations != 4 || env.Data.Meta.Truncated {
		t.Errorf("result = %+v, want 4 observations read from legacy_observationsV1_getMany, not truncated", env)
	}
	root := env.Data.Data[0]
	for _, key := range []string{"input", "output", "metadata"} {
		if _, ok := root[key]; ok {
			t.Errorf("the root carries %s without include: %v", key, root)
		}
	}
	// The row is what that Langfuse returns, not translated: the legacy list
	// gives the latency of the 2-second root in milliseconds
	// (docs/research/langfuse.md §1.12).
	if root["latency"] != 2000.0 {
		t.Errorf("root latency = %v, want 2000 (milliseconds, as the legacy list returns it)", root["latency"])
	}
}

func TestLiveLangfuse3800GetTraceTreeKeepsInputOutputAndMetadataWhenIncluded(t *testing.T) {
	t.Parallel()
	cs := liveScanSession(t)
	trace := seedLegacyTree(t)
	args := map[string]any{"traceId": trace, "include": []string{"io", "metadata"}}

	env := liveTreeUntil(t, cs, args, 4, legacyTreeReadBackTimeout)

	root := env.Data.Data[0]
	meta, _ := root["metadata"].(map[string]any)
	if root["input"] != "input of root" || root["output"] != "output of root" || meta["k"] != "metadata of root" {
		t.Errorf("root = %v, want its input, output and metadata", root)
	}
}

// A trace of more than one page of the legacy list (it pages at 100): the tree
// reads page 2 and counts every observation. The tree leaves out each key
// Langfuse sent as null (#200), which makes a row of these spans (no model, usage,
// cost or end time) smaller than the recorded one, but not small enough for 130
// of them to fit the default result size cap: measured on 3.80.0 on 2026-10-11,
// 125 of the 130 rows come back (the recorded row of the seam test gives 123),
// the result is marked truncated and its hint says so. If the hint's wording
// changes, this number and the one of tracetree_legacy_nullkeys_test.go move
// with it: measure again, do not loosen.
const liveRowsAtTheDefaultCap = 125

func TestLiveLangfuse3800GetTraceTreeReadsASecondPageOfTheLegacyList(t *testing.T) {
	t.Parallel()
	cs := liveScanSession(t)
	const observations = 130
	trace := "it-tree-pages-" + randomHex(t, 6)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	events := []map[string]any{liveEvent(trace, "trace-create", map[string]any{"id": trace, "name": "it-tree"})}
	for i := range observations {
		body := map[string]any{
			"id": fmt.Sprintf("%s-%03d", trace, i), "traceId": trace, "name": fmt.Sprintf("step-%03d", i),
			"startTime": base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
		}
		events = append(events, liveEvent(fmt.Sprintf("%s-%03d", trace, i), "span-create", body))
	}
	ingestLive(t, events...)

	env := liveTreeUntil(t, cs, map[string]any{"traceId": trace}, observations, legacyTreePagesReadBackTimeout)

	wantHint := fmt.Sprintf("only the first %d of %d rows fit the result size cap", liveRowsAtTheDefaultCap, observations)
	if env.Data.Meta.Observations != observations || env.Data.Meta.Truncated || len(env.Data.Data) != liveRowsAtTheDefaultCap ||
		!env.Truncated || !strings.Contains(env.Hint, wantHint) {
		t.Fatalf("tree holds %d rows (meta %+v, truncated %v, hint %q), want %d of the %d observations read by two pages, "+
			"the result marked truncated with %q", len(env.Data.Data), env.Data.Meta, env.Truncated, env.Hint, liveRowsAtTheDefaultCap, observations, wantHint)
	}
	for _, key := range []string{"statusMessage", "version", "model", "input", "output"} {
		if _, ok := env.Data.Data[0][key]; ok {
			t.Errorf("the first row keeps %s, which Langfuse sent as null: %v", key, env.Data.Data[0])
		}
	}
	if first := env.Data.Data[0]["name"]; first != "step-000" {
		t.Errorf("the first row is %v, want step-000: the start-time order across the two pages", first)
	}
}

func TestLiveLangfuse3800GetTraceTreeOfATraceThatDoesNotExistIsAnEmptyResultWithAHint(t *testing.T) {
	t.Parallel()
	cs := liveScanSession(t)

	env := callTraceTree(t, cs, map[string]any{"traceId": "it-tree-missing-" + randomHex(t, 6)})

	if len(env.Data.Data) != 0 || env.Data.Meta.Observations != 0 || env.Hint == "" {
		t.Errorf("result = %+v, want no observation and a hint", env)
	}
}
