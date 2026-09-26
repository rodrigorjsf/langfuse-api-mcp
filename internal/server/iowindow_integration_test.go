//go:build integration

package server_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The io/metadata window probe (issue #2, ticket #15): does the Langfuse REST
// API enforce the official MCP's rule that an observation query projecting
// input/output or metadata needs a trace id, an id filter or a window of at
// most 14 days, with at most 50 rows when date-scoped? The probe seeds
// back-dated observations, queries them through execute_read and asserts what
// Langfuse was observed to do, so a change upstream fails the suite. Outcome
// and verbatim probe lines: docs/research/langfuse.md §1.6–1.7.
//
// Seeding and cleanup call Langfuse directly: trace ingestion (OTLP) is out of
// the server's scope (ADR-0004) and deletes are writes; both are test setup.

const probeDay = 24 * time.Hour

// seededAges returns the start-time ages of the single-span traces by label:
// d12_5, d13_5, d14_5 and d20 each sit just inside one probed window (13, 14,
// 15 and 30 days); d1 is a recent row. None is older than 20 days: Langfuse
// Cloud accepted a 40-day-old span's export but did not serve it
// (docs/research/langfuse.md §1.6).
func seededAges() map[string]time.Duration {
	return map[string]time.Duration{
		"d1":    1 * probeDay,
		"d12_5": 12*probeDay + 12*time.Hour,
		"d13_5": 13*probeDay + 12*time.Hour,
		"d14_5": 14*probeDay + 12*time.Hour,
		"d20":   20 * probeDay,
	}
}

// bulkSpans is the number of spans in the 2-day-old bulk trace, all with the
// same name: more than the 51 rows the row-limit probe asks for.
const bulkSpans = 61

// ioSeed is what seedIOWindow created: one trace per age, plus the bulk trace.
type ioSeed struct {
	run         string            // unique per run: names and environment carry it
	environment string            // every seeded span's Langfuse environment
	traceIDs    map[string]string // by age label, plus "bulk"
	spanIDs     map[string]string // by age label
	now         time.Time         // the time the ages are counted from
}

// name is the observation name of a seeded span; every bulk span shares one.
func (s ioSeed) name(label string) string { return s.run + "-" + label }

// window returns the start-time window of the last `days` days before the seed.
func (s ioSeed) window(days int) map[string]any {
	end := s.now.Add(5 * time.Minute)
	return map[string]any{
		"fromStartTime": end.Add(-time.Duration(days) * probeDay).Format(time.RFC3339Nano),
		"toStartTime":   end.Format(time.RFC3339Nano),
	}
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}

// langfuseDirect sends one request straight to the live Langfuse with the
// test key pair, for the setup steps execute_read cannot do. It trusts the
// system roots only: a self-hosted test Langfuse behind a private CA would
// fail here, at seeding, before any probe runs.
func langfuseDirect(t *testing.T, method, path string, body any) (int, string) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode %s %s: %v", method, path, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// G704: the URL is the operator's LANGFUSE_TEST_BASE_URL plus a constant path.
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(os.Getenv(envTestBaseURL), "/")+path, bytes.NewReader(payload)) //nolint:gosec // see above
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.SetBasicAuth(os.Getenv(envTestPublicKey), os.Getenv(envTestSecretKey))
	req.Header.Set("Content-Type", "application/json")
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
	return resp.StatusCode, string(text)
}

// otlpSpan is one span of an OTLP/HTTP JSON export carrying Langfuse's
// input, output, metadata and environment attributes.
func otlpSpan(traceID, spanID, name, env string, start time.Time) map[string]any {
	attr := func(k, v string) map[string]any {
		return map[string]any{"key": k, "value": map[string]any{"stringValue": v}}
	}
	return map[string]any{
		"traceId": traceID, "spanId": spanID, "name": name, "kind": 1,
		"startTimeUnixNano": strconv.FormatInt(start.UnixNano(), 10),
		"endTimeUnixNano":   strconv.FormatInt(start.Add(500*time.Millisecond).UnixNano(), 10),
		"attributes": []any{
			attr("langfuse.observation.input", `{"q":"`+name+`"}`),
			attr("langfuse.observation.output", `{"a":"out-`+name+`"}`),
			attr("langfuse.observation.metadata.probe", name),
			attr("langfuse.environment", env),
		},
	}
}

// seedIOWindow exports the seeded spans through the deployment's OTLP
// endpoint, waits until all of them are queryable, and deletes their traces
// when the test ends.
func seedIOWindow(t *testing.T, cs *mcp.ClientSession) ioSeed {
	t.Helper()
	run := "it-io-" + randomHex(t, 4)
	s := ioSeed{run: run, environment: run, traceIDs: map[string]string{}, spanIDs: map[string]string{}, now: time.Now().UTC()}
	var spans []any
	for label, age := range seededAges() {
		s.traceIDs[label], s.spanIDs[label] = randomHex(t, 16), randomHex(t, 8)
		spans = append(spans, otlpSpan(s.traceIDs[label], s.spanIDs[label], s.name(label), s.environment, s.now.Add(-age)))
	}
	s.traceIDs["bulk"] = randomHex(t, 16)
	for i := range bulkSpans {
		spans = append(spans, otlpSpan(s.traceIDs["bulk"], randomHex(t, 8), s.name("bulk"), s.environment,
			s.now.Add(-2*probeDay+time.Duration(i)*time.Second)))
	}

	t.Cleanup(func() { deleteSeededTraces(t, s) })
	status, body := langfuseDirect(t, http.MethodPost, "/api/public/otel/v1/traces", map[string]any{
		"resourceSpans": []any{map[string]any{
			"resource":   map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "langfuse-mcp-integration"}}}},
			"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": "iowindow-probe"}, "spans": spans}},
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("OTLP export answered %d: %s", status, body)
	}
	waitSeeded(t, cs, s, len(spans))
	return s
}

// waitSeeded polls, a few seconds apart, until every seeded span is
// queryable: ingestion is asynchronous (about 10 s on self-hosted).
func waitSeeded(t *testing.T, cs *mcp.ClientSession, s ioSeed, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for {
		rows := probeRows(t, cs, map[string]any{"fields": "core,basic", "environment": s.environment, "limit": 100})
		if len(rows) == want {
			return
		}
		if time.Now().After(deadline) {
			seen := map[any]bool{}
			for _, r := range rows {
				seen[r["name"]] = true
			}
			var missing []string
			for label := range seededAges() {
				if !seen[s.name(label)] {
					missing = append(missing, label)
				}
			}
			t.Fatalf("after 3 minutes %d of %d seeded observations are queryable; single-span traces missing: %v",
				len(rows), want, missing)
		}
		if err := waitRetryAfter(t.Context(), 6*time.Second); err != nil {
			t.Fatalf("waiting for ingestion: %v", err)
		}
	}
}

// deleteSeededTraces removes what seedIOWindow created. A failed delete is
// reported but does not fail the probe: the rows carry a unique run name.
func deleteSeededTraces(t *testing.T, s ioSeed) {
	t.Helper()
	ids := make([]string, 0, len(s.traceIDs))
	for _, id := range s.traceIDs {
		ids = append(ids, id)
	}
	status, body := langfuseDirect(t, http.MethodDelete, "/api/public/traces", map[string]any{"traceIds": ids})
	t.Logf("cleanup: DELETE /api/public/traces (%d seeded traces) → %d %s", len(ids), status, body)
}

// probeRows calls observations_getMany with params and returns its rows,
// failing the test on a tool error.
func probeRows(t *testing.T, cs *mcp.ClientSession, params map[string]any) []map[string]any {
	t.Helper()
	var page struct {
		Data []map[string]any `json:"data"`
	}
	liveData(t, readLive(t, cs, map[string]any{"operationId": "observations_getMany", "parameters": params}, waitRetryAfter), &page)
	return page.Data
}

// mergedParams returns the union of parameter sets; later sets win.
func mergedParams(sets ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, set := range sets {
		maps.Copy(out, set)
	}
	return out
}

// A payload query requests the io or metadata field groups. Observed on
// self-hosted 4.46.0 and Langfuse Cloud (4.46.0), 2026-09-26: Langfuse caps
// neither its window nor its rows. Every probe answers 200 with all the rows
// it names.
func TestLiveLangfuseServesPayloadQueriesBeyondFourteenDaysAndFiftyRows(t *testing.T) {
	t.Parallel()
	cs := liveSession(t)
	s := seedIOWindow(t, cs)
	idFilter, err := json.Marshal([]map[string]any{{"type": "string", "column": "id", "operator": "=", "value": s.spanIDs["d20"]}})
	if err != nil {
		t.Fatal(err)
	}

	ioBy := func(label string) map[string]any { return map[string]any{"fields": "core,io", "name": s.name(label)} }
	metadataBy := func(label string) map[string]any {
		return map[string]any{"fields": "core,metadata", "name": s.name(label)}
	}
	probes := []struct {
		name     string
		params   map[string]any
		wantRows int
		field    string // present on every row: the field group was served
	}{
		{"io, 13-day window", mergedParams(ioBy("d12_5"), s.window(13)), 1, "input"},
		{"io, 14-day window", mergedParams(ioBy("d13_5"), s.window(14)), 1, "input"},
		{"io, 15-day window", mergedParams(ioBy("d14_5"), s.window(15)), 1, "input"},
		{"io, 30-day window", mergedParams(ioBy("d20"), s.window(30)), 1, "output"},
		{"io, no window", ioBy("d20"), 1, "input"},
		{"metadata, 15-day window", mergedParams(metadataBy("d14_5"), s.window(15)), 1, "metadata"},
		{"metadata, 30-day window", mergedParams(metadataBy("d20"), s.window(30)), 1, "metadata"},
		{"io, trace id, no window", map[string]any{"fields": "core,io", "traceId": s.traceIDs["d20"]}, 1, "input"},
		{"io, id filter, no window", map[string]any{"fields": "core,io", "filter": string(idFilter)}, 1, "input"},
		// The combination the official MCP caps at 50 rows: a date window, the io
		// group, and no trace id or id filter (nor any other filter).
		{"io, 13-day window, limit 50", mergedParams(map[string]any{"fields": "core,io", "limit": 50}, s.window(13)), 50, "input"},
		{"io, 13-day window, limit 51", mergedParams(map[string]any{"fields": "core,io", "limit": 51}, s.window(13)), 51, "input"},
	}
	for _, p := range probes { // sequential subtests: the Cloud run spends its rate budget one call at a time
		t.Run(p.name, func(t *testing.T) {
			probeServes(t, cs, p.params, p.wantRows, p.field)
		})
	}
}

// probeServes asserts that one probe answers with wantRows rows, each carrying
// field, and logs the answer for the research notes.
func probeServes(t *testing.T, cs *mcp.ClientSession, params map[string]any, wantRows int, field string) {
	t.Helper()
	rows := probeRows(t, cs, params)
	withField := 0
	for _, r := range rows {
		if _, ok := r[field]; ok {
			withField++
		}
	}
	first, err := json.Marshal(rows[:min(1, len(rows))])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("→ 200 rows=%d with %s=%d first=%.300s", len(rows), field, withField, first)
	if len(rows) != wantRows || withField != wantRows {
		t.Errorf("%d rows, %d with %s; want %d, all with %s", len(rows), withField, field, wantRows, field)
	}
}
