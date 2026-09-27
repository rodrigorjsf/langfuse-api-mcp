//go:build integration

package server_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"strconv"
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
// insideNd sits just inside the probed window of N days (12.5, 13.5, 14.5 and
// 20 days old); recent is one day old. None is older than 20 days: Langfuse
// Cloud accepted a 40-day-old span's export but did not serve it
// (docs/research/langfuse.md §1.6).
func seededAges() map[string]time.Duration {
	return map[string]time.Duration{
		"recent":    1 * probeDay,
		"inside13d": 12*probeDay + 12*time.Hour,
		"inside14d": 13*probeDay + 12*time.Hour,
		"inside15d": 14*probeDay + 12*time.Hour,
		"inside30d": 20 * probeDay,
	}
}

// bulkSpans is the number of spans in the 2-day-old bulk trace, all with the
// same name: more than the 51 rows the row-limit probe asks for.
const bulkSpans = 61

// ioSeed is what seedIOWindow created: one trace per age, plus the bulk trace.
type ioSeed struct {
	run         string                // unique per run: names and environment carry it
	environment string                // every seeded span's Langfuse environment
	spans       map[string]seededSpan // the single-span traces, by age label
	bulkTraceID string                // the trace holding the bulkSpans spans
	now         time.Time             // the time the ages are counted from
}

// seededSpan is one single-span trace: its trace id and its span id.
type seededSpan struct{ traceID, spanID string }

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
// test key pair, for the setup steps execute_read cannot do (liveTarget.send).
func langfuseDirect(ctx context.Context, t *testing.T, method, path string, body any) (int, string) {
	t.Helper()
	target := liveTarget{baseURL: os.Getenv(envTestBaseURL), publicKey: os.Getenv(envTestPublicKey), secretKey: os.Getenv(envTestSecretKey)}
	return target.send(ctx, t, method, path, body, sleepCtx)
}

// otlpSpan is one seeded span of an OTLP/HTTP JSON export, named after its
// label, carrying Langfuse's input, output, metadata and environment
// attributes.
func (s ioSeed) otlpSpan(ids seededSpan, label string, start time.Time) map[string]any {
	name := s.name(label)
	attr := func(k, v string) map[string]any {
		return map[string]any{"key": k, "value": map[string]any{"stringValue": v}}
	}
	return map[string]any{
		"traceId": ids.traceID, "spanId": ids.spanID, "name": name, "kind": 1,
		"startTimeUnixNano": strconv.FormatInt(start.UnixNano(), 10),
		"endTimeUnixNano":   strconv.FormatInt(start.Add(500*time.Millisecond).UnixNano(), 10),
		"attributes": []any{
			attr("langfuse.observation.input", `{"q":"`+name+`"}`),
			attr("langfuse.observation.output", `{"a":"out-`+name+`"}`),
			attr("langfuse.observation.metadata.probe", name),
			attr("langfuse.environment", s.environment),
		},
	}
}

// seedIOWindow exports the seeded spans through the deployment's OTLP
// endpoint, waits until all of them are queryable, and deletes their traces
// when the test ends.
func seedIOWindow(t *testing.T, cs *mcp.ClientSession) ioSeed {
	t.Helper()
	run := "it-io-" + randomHex(t, 4)
	s := ioSeed{run: run, environment: run, spans: map[string]seededSpan{}, now: time.Now().UTC()}
	var spans []any
	for label, age := range seededAges() {
		span := seededSpan{traceID: randomHex(t, 16), spanID: randomHex(t, 8)}
		s.spans[label] = span
		spans = append(spans, s.otlpSpan(span, label, s.now.Add(-age)))
	}
	s.bulkTraceID = randomHex(t, 16)
	for i := range bulkSpans {
		bulk := seededSpan{traceID: s.bulkTraceID, spanID: randomHex(t, 8)}
		spans = append(spans, s.otlpSpan(bulk, "bulk", s.now.Add(-2*probeDay+time.Duration(i)*time.Second)))
	}

	t.Cleanup(func() { deleteSeededTraces(t, s) })
	exportOTLPSpans(t, "iowindow-probe", spans)
	waitSeeded(t, cs, s, len(spans))
	return s
}

// exportOTLPSpans exports spans, under the instrumentation scope named scope,
// through the deployment's OTLP/HTTP JSON endpoint, failing the test unless
// Langfuse accepts them.
func exportOTLPSpans(t *testing.T, scope string, spans []any) {
	t.Helper()
	status, body := langfuseDirect(t.Context(), t, http.MethodPost, "/api/public/otel/v1/traces", map[string]any{
		"resourceSpans": []any{map[string]any{
			"resource":   map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "langfuse-mcp-integration"}}}},
			"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": scope}, "spans": spans}},
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("OTLP export answered %d: %s", status, body)
	}
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
		if err := sleepCtx(t.Context(), 6*time.Second); err != nil {
			t.Fatalf("waiting for ingestion: %v", err)
		}
	}
}

// deleteSeededTraces removes what seedIOWindow created. A failed delete is
// reported but does not fail the probe: the rows carry a unique run name.
func deleteSeededTraces(t *testing.T, s ioSeed) {
	t.Helper()
	ids := []string{s.bulkTraceID}
	for _, span := range s.spans {
		ids = append(ids, span.traceID)
	}
	// t.Context() is already cancelled when cleanups run.
	status, body := langfuseDirect(context.WithoutCancel(t.Context()), t, http.MethodDelete, "/api/public/traces", map[string]any{"traceIds": ids})
	t.Logf("cleanup: DELETE /api/public/traces (%d seeded traces) → %d %s", len(ids), status, body)
}

// probeRows calls observations_getMany with params and returns its rows,
// failing the test on a tool error.
func probeRows(t *testing.T, cs *mcp.ClientSession, params map[string]any) []map[string]any {
	t.Helper()
	var page struct {
		Data []map[string]any `json:"data"`
	}
	liveData(t, readLive(t, cs, map[string]any{"operationId": "observations_getMany", "parameters": params}, sleepCtx), &page)
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
	// idFilter is the observations filter JSON selecting one seeded span by id.
	idFilter := func(label string) string {
		f, err := json.Marshal([]map[string]any{{"type": "string", "column": "id", "operator": "=", "value": s.spans[label].spanID}})
		if err != nil {
			t.Fatal(err)
		}
		return string(f)
	}

	// byName selects the seeded span, or the bulk spans, of label.
	byName := func(label string) map[string]any { return map[string]any{"name": s.name(label)} }
	// probe requests the field group for the rows selector names, within the
	// last `days` days of the seed (0: no window).
	probe := func(group string, selector map[string]any, days int) map[string]any {
		params := mergedParams(map[string]any{"fields": "core," + group}, selector)
		if days > 0 {
			maps.Copy(params, s.window(days))
		}
		return params
	}
	probes := []struct {
		name     string
		params   map[string]any
		wantRows int
		field    string // present on every row: the field group was served
	}{
		{"io, 13-day window", probe("io", byName("inside13d"), 13), 1, "input"},
		{"io, 14-day window", probe("io", byName("inside14d"), 14), 1, "input"},
		{"io, 15-day window", probe("io", byName("inside15d"), 15), 1, "input"},
		{"io, 30-day window", probe("io", byName("inside30d"), 30), 1, "output"},
		{"io, no window", probe("io", byName("inside30d"), 0), 1, "input"},
		{"metadata, 13-day window", probe("metadata", byName("inside13d"), 13), 1, "metadata"},
		{"metadata, 14-day window", probe("metadata", byName("inside14d"), 14), 1, "metadata"},
		{"metadata, 15-day window", probe("metadata", byName("inside15d"), 15), 1, "metadata"},
		{"metadata, 30-day window", probe("metadata", byName("inside30d"), 30), 1, "metadata"},
		{"io, trace id, no window", probe("io", map[string]any{"traceId": s.spans["inside30d"].traceID}, 0), 1, "input"},
		{"io, trace id, 15-day window", probe("io", map[string]any{"traceId": s.spans["inside15d"].traceID}, 15), 1, "input"},
		{"io, id filter, no window", probe("io", map[string]any{"filter": idFilter("inside30d")}, 0), 1, "input"},
		{"io, id filter, 15-day window", probe("io", map[string]any{"filter": idFilter("inside15d")}, 15), 1, "input"},
		// The combination the official MCP caps at 50 rows: a date window, the io
		// group, and no trace id or id filter (nor any other filter).
		{"io, 13-day window, limit 50", probe("io", map[string]any{"limit": 50}, 13), 50, "input"},
		{"io, 13-day window, limit 51", probe("io", map[string]any{"limit": 51}, 13), 51, "input"},
	}
	// Sequential subtests: parallel probes would burst the Cloud rate budget
	// (Hobby: 30 req/min). The other live tests run alongside, but spend only
	// two calls between them.
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			probeServes(t, cs, p.params, p.wantRows, p.field)
		})
	}
}

// probeServes asserts that one probe answers with wantRows rows, each carrying
// field, and logs the answer for the research notes. execute_read does not
// expose the HTTP status: a successful call means Langfuse answered 2xx (the
// client turns any other status into a tool error), so the log says "2xx".
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
	t.Logf("→ 2xx rows=%d with %s=%d first=%.300s", len(rows), field, withField, first)
	if len(rows) != wantRows || withField != wantRows {
		t.Errorf("%d rows, %d with %s; want %d, all with %s", len(rows), withField, field, wantRows, field)
	}
}
