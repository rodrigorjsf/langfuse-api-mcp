package server_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam (#203, spec #172 story 16): the hint of a response too large and the
// hint of a call that got no answer in time name the ways of narrowing the
// called operation has, from the words of the truncation hint, and no other. An
// operation with no way gets fixed text that says so, never an invitation to
// call again.

// The fixed hints of an operation with no way of narrowing, as literals.
const (
	tooLargeNoWay = "no parameter of this operation makes the answer smaller: tell the user instead of calling again"
	slowNoWay     = "no parameter of this operation makes the call cheaper: tell the user instead of calling again"
	canceledNoWay = "if the call was canceled because it was slow, no parameter of this operation makes the call cheaper: " +
		"tell the user instead of calling again"
)

// wordsOfAWayToNarrow are the words that name a way of narrowing or of paging:
// the hint of an operation with none holds no one of them.
var wordsOfAWayToNarrow = []string{"limit", "time window", "time range", "fields", "page", "cursor", "narrow the query", "call again later"}

func assertNamesNoWayToNarrow(t *testing.T, hint string) {
	t.Helper()
	for _, word := range wordsOfAWayToNarrow {
		if strings.Contains(hint, word) {
			t.Errorf("hint %q names %q, but the operation has no way of narrowing", hint, word)
		}
	}
}

// trace_get on 3.80.0 serves no fields, no limit, no time window and no paging:
// a Langfuse 413 and a body over the bytes this server reads both say that no
// parameter makes the answer smaller.
func TestTheHintOfAResponseTooLargeSaysThatNoParameterMakesTheAnswerSmallerForAnOperationWithNoWayOfNarrowing(t *testing.T) {
	t.Parallel()
	legacy := []catalog.Family{catalog.LegacyFamily}
	call := map[string]any{"operationId": "trace_get", "parameters": map[string]any{"traceId": "t-1"}}
	for source, answered := range map[string]answer{
		"a 413 from Langfuse": {status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`},
		"a body over 5 MiB":   {status: http.StatusOK, body: `{"data":"` + strings.Repeat("a", 5<<20) + `"}`},
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			fake, _ := scriptedLangfuse(t, answered)
			cs := connectServing(t, fake, "3.80.0", legacy...)

			got := toolErrorOf(t, callExecuteRead(t, cs, call)).Error

			if got.Code != "response_too_large" || got.Retryable || got.Hint != tooLargeNoWay {
				t.Fatalf("error = %+v, want response_too_large, not retryable, with the hint %q", got, tooLargeNoWay)
			}
			assertNamesNoWayToNarrow(t, got.Hint)
		})
	}
}

// A slow call of an operation with no way of narrowing is told so, and is not
// invited to call again: the error stays not retryable, and "call again later"
// is what the server says only for a retryable error.
func TestTheHintOfASlowCallSaysThatNoParameterMakesItCheaperForAnOperationWithNoWayOfNarrowing(t *testing.T) {
	t.Parallel()
	legacy := []catalog.Family{catalog.LegacyFamily}
	t.Run("a timeout", func(t *testing.T) {
		t.Parallel()
		cs := connectServingSlow(t, stallingLangfuse(t), "3.80.0", legacy...)

		got := toolErrorOf(t, callExecuteRead(t, cs, readCall("trace_get"))).Error

		if got.Code != "timeout" || got.Retryable || got.Hint != slowNoWay {
			t.Fatalf("error = %+v, want a timeout, not retryable, with the hint %q", got, slowNoWay)
		}
		assertNamesNoWayToNarrow(t, got.Hint)
	})
	// The client of a canceled call reads no result, so the hint is checked
	// where it is built.
	t.Run("a canceled call", func(t *testing.T) {
		t.Parallel()
		traceGet, ok := sharedCatalog(t).Resolve(catalog.Profile{Version: "3.80.0", Families: legacy}).Lookup("trace_get")
		if !ok {
			t.Fatal("trace_get is not offered on 3.80.0")
		}

		code, hint := server.FailureOf(traceGet, langfuse.ErrCanceled)

		if code != "canceled" || hint != canceledNoWay {
			t.Fatalf("code %q, hint %q; want canceled with the hint %q", code, hint, canceledNoWay)
		}
		assertNamesNoWayToNarrow(t, hint)
	})
}

// operationWithTimeWindow is a GET operation that takes the query parameters
// named and one more, fromTimestamp, which the catalog lists as a time window
// (#204) and the release spec gives the format date-time.
func operationWithTimeWindow(names ...string) catalog.Operation {
	op := operationWith(names...)
	op.Params = append(op.Params, catalog.Param{Name: "fromTimestamp", In: "query", Schema: catalog.Schema{Type: "string", Format: "date-time"}})
	return op
}

// An operation names only the ways of narrowing it has: a limit and no time
// window gets the limit alone, a time window and no limit the time window
// alone, proven on literal operations so that every combination is covered, as
// #193 proved "both ways of paging". The catalog has only the first kind left
// (datasets_list takes a limit and no time window; since #204 no operation has a
// time window and no limit, because legacy_observationsV1_get, with a startTime
// and no limit, has no window), so the second is proven on a literal operation
// alone. The real operations are run through the tool in timewindowhints_test.go.
func TestAnOperationIsToldOnlyTheWaysOfNarrowingItHas(t *testing.T) {
	t.Parallel()
	const slowLead = "narrow the query before calling again: "
	tests := map[string]struct {
		op                       catalog.Operation
		tooLarge, slow, canceled string
	}{
		"a limit and no time window": {operationWith("limit"),
			"narrow the query: a lower limit", slowLead + "a lower limit", canceledLead + slowLead + "a lower limit"},
		"a time window and no limit": {operationWithTimeWindow(),
			"narrow the query: a shorter time window", slowLead + "a shorter time window", canceledLead + slowLead + "a shorter time window"},
		"a limit and a time window": {operationWithTimeWindow("limit"),
			"narrow the query: a lower limit or a shorter time window", slowLead + "a lower limit or a shorter time window",
			canceledLead + slowLead + "a lower limit or a shorter time window"},
		"a limit and paging": {operationWith("limit", "page"),
			"narrow the query: a lower limit, and page through the rest " + viaPage, slowLead + "a lower limit",
			canceledLead + slowLead + "a lower limit"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, canceledHint := server.FailureOf(tc.op, langfuse.ErrCanceled)
			_, timeoutHint := server.FailureOf(tc.op, langfuse.ErrTimeout)

			if got := server.TooLargeHint(tc.op); got != tc.tooLarge {
				t.Errorf("too large: hint = %q, want %q", got, tc.tooLarge)
			}
			if timeoutHint != tc.slow {
				t.Errorf("timeout: hint = %q, want %q", timeoutHint, tc.slow)
			}
			if canceledHint != tc.canceled {
				t.Errorf("canceled: hint = %q, want %q", canceledHint, tc.canceled)
			}
		})
	}
}

// The metrics operations take no limit and no time parameter of their own: their
// query JSON holds the time range, the filters and config.row_limit, so they are
// told that, not that nothing narrows them.
func TestAMetricsOperationIsToldToNarrowItsQueryJSON(t *testing.T) {
	t.Parallel()
	const way = "a shorter time range, more filters or a lower config.row_limit in the query JSON"
	cat := sharedCatalog(t)
	for name, tc := range map[string]struct {
		profile catalog.Profile
		id      string
	}{
		"metrics_metrics":          {catalog.Profile{Version: "4.46.0", Families: catalog.AllFamilies()}, "metrics_metrics"},
		"legacy_metricsV1_metrics": {catalog.Profile{Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}}, "legacy_metricsV1_metrics"},
	} {
		op, ok := cat.Resolve(tc.profile).Lookup(tc.id)
		if !ok {
			t.Fatalf("%s: not offered", name)
		}
		_, timeoutHint := server.FailureOf(op, langfuse.ErrTimeout)

		if got, want := server.TooLargeHint(op), "narrow the query: "+way; got != want {
			t.Errorf("%s: too large: hint = %q, want %q", name, got, want)
		}
		if want := "narrow the query before calling again: " + way; timeoutHint != want {
			t.Errorf("%s: timeout: hint = %q, want %q", name, timeoutHint, want)
		}
		if got := server.TruncationHint(op); !strings.Contains(got, "narrow the query: "+way) {
			t.Errorf("%s: truncation hint = %q, want it to name the same way", name, got)
		}
	}
}

// The ways each hint names, found by their words.
var (
	wordsOfFields  = "fewer field groups in fields"
	wordsOfLimit   = "a lower limit"
	wordsOfWindow  = "a shorter time window"
	wordsOfMetrics = "a shorter time range"
	wordsOfPaging  = "page through the rest"
	wordsOfWays    = []string{wordsOfFields, wordsOfLimit, wordsOfWindow, wordsOfMetrics, wordsOfPaging}
)

// profilesOfTheFixtures are the deployment profiles the catalog tests resolve.
func profilesOfTheFixtures() map[string]catalog.Profile {
	legacy := []catalog.Family{catalog.LegacyFamily}
	return map[string]catalog.Profile{
		"3.80.0":                   {Version: "3.80.0", Families: legacy},
		"3.165.0":                  {Version: "3.165.0", Families: legacy},
		"3.225.11":                 {Version: "3.225.11", Families: legacy},
		"4.46.0 dual":              {Version: "4.46.0", Families: catalog.AllFamilies()},
		"4.46.0 events_only":       {Version: "4.46.0", Families: []catalog.Family{catalog.V4ReadFamily, catalog.ExperimentsFamily}},
		"no detected version":      {Families: catalog.AllFamilies()},
		"3.141.0, v4 read at last": {Version: "3.141.0", Families: []catalog.Family{catalog.V4ReadFamily}},
	}
}

// Walks every read operation of each deployment profile of the fixtures. The
// hint of a response too large and the hint of a timeout or canceled call name a
// limit, a time window or fields only where the operation serves them there, and
// no way of narrowing that its truncation hint would not: the three come from one
// source. An operation with no way gets the fixed text, and no hint is empty.
func TestTheTooLargeAndTheSlowHintsNameNoWayThatTheTruncationHintWouldNot(t *testing.T) {
	t.Parallel()
	cat := sharedCatalog(t)
	for name, profile := range profilesOfTheFixtures() {
		walked := 0
		for _, op := range cat.Resolve(profile).Operations() {
			if !op.IsRead() {
				continue
			}
			walked++
			_, canceledHint := server.FailureOf(op, langfuse.ErrCanceled)
			hints := map[string]string{
				"too large": server.TooLargeHint(op), "timeout": server.TimeoutHint(op), "canceled": canceledHint,
			}
			truncation := server.TruncationHint(op)
			serves := map[string]bool{
				wordsOfFields: op.HasQueryParam("fields"), wordsOfLimit: op.HasQueryParam("limit"),
				wordsOfWindow: op.HasTimeWindow(), wordsOfMetrics: op.HasMetricsQuery(),
			}
			narrows := false
			for _, served := range serves {
				narrows = narrows || served
			}
			for what, hint := range hints {
				if hint == "" {
					t.Errorf("%s: the %s hint of %s is empty", name, what, op.ID)
				}
				for _, way := range wordsOfWays {
					named := strings.Contains(hint, way)
					if served, isWay := serves[way]; isWay && named != served {
						t.Errorf("%s: the %s hint of %s names %q: %v, the operation serves it: %v (%q)", name, what, op.ID, way, named, served, hint)
					}
					if named && !strings.Contains(truncation, way) {
						t.Errorf("%s: the %s hint of %s names %q, which its truncation hint does not (%q)", name, what, op.ID, way, truncation)
					}
				}
				if !narrows && !strings.Contains(hint, "tell the user") {
					t.Errorf("%s: the %s hint of %s has no way of narrowing and does not say so: %q", name, what, op.ID, hint)
				}
				if narrows && strings.Contains(hint, "tell the user") {
					t.Errorf("%s: the %s hint of %s has a way of narrowing and says it has none: %q", name, what, op.ID, hint)
				}
			}
		}
		if walked == 0 {
			t.Errorf("%s: the walk found no read operation", name)
		}
	}
}

// The hints of a failed trace tree, as literals (#203). The tool takes no limit,
// no fields and no time window, so a slow call is sent to execute_read with the
// operation the tool reads, whose parameters are the ones that read less.
const (
	traceTreeSlowObservations = "get_trace_tree has no argument that reads less: " +
		"execute_read with observations_getMany (traceId, fields, a lower limit, cursor) reads the trace in smaller pages"
	traceTreeSlowLegacy = "get_trace_tree has no argument that reads less: " +
		"execute_read with legacy_observationsV1_getMany (traceId, a limit below 100, page) reads the trace in smaller pages"
	traceTreeTooLargeObservations = "a page of 1000 observations is larger than the response this server reads; " +
		"execute_read with observations_getMany (traceId, fields, a lower limit, cursor) reads the trace in smaller pages"
	traceTreeTooLargeLegacy = "a page of 100 observations is larger than the response this server reads; " +
		"execute_read with legacy_observationsV1_getMany (traceId, a limit below 100, page) reads the trace in smaller pages"
)

// get_trace_tree reads a trace through Observations v2 or the legacy list. A page
// that got no answer in time is answered with fixed text for the source, and not
// with a limit, fields or time window as an argument of the tool.
func TestTheHintOfATraceTreeThatTimedOutIsTheFixedTextOfTheSourceItReads(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	legacy := []catalog.Family{catalog.LegacyFamily}
	for name, tc := range map[string]struct {
		version  string
		families []catalog.Family
		want     string
	}{
		"reading Observations v2": {"4.46.0", dual, traceTreeSlowObservations},
		"reading the legacy list": {"3.80.0", legacy, traceTreeSlowLegacy},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cs := connectServingSlow(t, stallingLangfuse(t), tc.version, tc.families...)

			got := toolErrorOf(t, callTool(t, cs, "get_trace_tree", map[string]any{"traceId": "t-1"})).Error

			if got.Code != "timeout" || got.Retryable || got.Hint != tc.want {
				t.Fatalf("error = %+v, want a timeout, not retryable, with the hint %q", got, tc.want)
			}
			if strings.Contains(got.Hint, "narrow the query") || strings.Contains(got.Hint, "time window") {
				t.Errorf("hint %q presents a way of narrowing as an argument of the tool", got.Hint)
			}
		})
	}
}

// A canceled call has the hint of a timeout behind a lead-in; the client of a
// canceled call reads no result, so the hint is checked where it is built.
func TestTheHintOfACanceledTraceTreeIsTheFixedTextOfTheSourceItReads(t *testing.T) {
	t.Parallel()
	cat := sharedCatalog(t)
	for name, tc := range map[string]struct {
		profile langfuse.DeploymentProfile
		want    string
	}{
		"reading Observations v2": {langfuse.DeploymentProfile{Version: "4.46.0", Families: catalog.AllFamilies()},
			"if the call was canceled because it was slow, " + traceTreeSlowObservations},
		"reading the legacy list": {langfuse.DeploymentProfile{Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}},
			"if the call was canceled because it was slow, " + traceTreeSlowLegacy},
	} {
		code, hint := server.TraceTreeFailureOf(cat, tc.profile, langfuse.ErrCanceled)

		if code != "canceled" || hint != tc.want {
			t.Errorf("%s: code %q, hint %q; want canceled with the hint %q", name, code, hint, tc.want)
		}
	}
}

// A page that Langfuse answers 413 gets the too-large hint of the source, as a
// page over the bytes this server reads does (#193): the tool takes no limit, so
// the generic hint would send the agent to an argument it lacks.
func TestTheHintOfATraceTreePageAnswered413IsTheTooLargeHintOfTheSourceItReads(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	legacy := []catalog.Family{catalog.LegacyFamily}
	for name, tc := range map[string]struct {
		version  string
		families []catalog.Family
		want     string
	}{
		"reading Observations v2": {"4.46.0", dual, traceTreeTooLargeObservations},
		"reading the legacy list": {"3.80.0", legacy, traceTreeTooLargeLegacy},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, _ := scriptedLangfuse(t, answer{status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`})
			cs := connectServing(t, fake, tc.version, tc.families...)

			got := toolErrorOf(t, callTool(t, cs, "get_trace_tree", map[string]any{"traceId": "t-1"})).Error

			if got.Code != "response_too_large" || got.HTTPStatus != http.StatusRequestEntityTooLarge || got.Hint != tc.want {
				t.Fatalf("error = %+v, want response_too_large (413) with the hint %q", got, tc.want)
			}
		})
	}
}

// A filter scan replaces the limit and reads from page 1, so the hint of a failed
// scan (a page too large, a timeout, a canceled call) names neither a lower limit
// nor paging, by the rule the truncation hint of the scan already follows (#183).
// It names a time window where the operation takes a time window.
func TestTheHintOfAFailedFilterScanNamesNeitherALowerLimitNorPaging(t *testing.T) {
	t.Parallel()
	legacy := []catalog.Family{catalog.LegacyFamily}
	for name, call := range map[string]map[string]any{
		"scores_get-many with traceId": {"operationId": "scores_get-many", "parameters": map[string]any{"traceId": "t-1"}},
		"legacy_observationsV1_getMany with level": {"operationId": "legacy_observationsV1_getMany",
			"parameters": map[string]any{"level": "ERROR"}},
	} {
		for source, answered := range map[string]answer{
			"a 413 from Langfuse": {status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`},
			"a body over 5 MiB":   {status: http.StatusOK, body: `{"data":"` + strings.Repeat("a", 5<<20) + `"}`},
		} {
			t.Run(name+", "+source, func(t *testing.T) {
				t.Parallel()
				fake, _ := scriptedLangfuse(t, answered)
				cs := connectServing(t, fake, "3.80.0", legacy...)

				got := toolErrorOf(t, callExecuteRead(t, cs, call)).Error

				if want := "narrow the query: a shorter time window"; got.Code != "response_too_large" || got.Hint != want {
					t.Fatalf("error = %+v, want response_too_large with the hint %q", got, want)
				}
			})
		}
		t.Run(name+", a timeout", func(t *testing.T) {
			t.Parallel()
			cs := connectServingSlow(t, stallingLangfuse(t), "3.80.0", legacy...)

			got := toolErrorOf(t, callExecuteRead(t, cs, call)).Error

			if want := "narrow the query before calling again: a shorter time window"; got.Code != "timeout" || got.Hint != want {
				t.Fatalf("error = %+v, want a timeout with the hint %q", got, want)
			}
		})
	}
}

// A canceled scan has the hint of a timeout behind a lead-in; the client of a
// canceled call reads no result, so the hint is checked where it is built, from
// the operation as the scan reads it.
func TestTheHintOfACanceledFilterScanNamesNeitherALowerLimitNorPaging(t *testing.T) {
	t.Parallel()
	older := sharedCatalog(t).Resolve(catalog.Profile{Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}})
	for _, id := range []string{"scores_get-many", "legacy_observationsV1_getMany"} {
		op, ok := older.Lookup(id)
		if !ok {
			t.Fatalf("%s is not offered on 3.80.0", id)
		}

		code, hint := server.FailureOf(server.ScanOperation(op), langfuse.ErrCanceled)

		want := "if the call was canceled because it was slow, narrow the query before calling again: a shorter time window"
		if code != "canceled" || hint != want {
			t.Errorf("%s: code %q, hint %q; want canceled with the hint %q", id, code, hint, want)
		}
	}
}

// notRetriedOnly is the hint of a failed write that has no way of narrowing: the
// note that the write was not retried and may have been applied, and nothing
// after it. The note itself is not changed by #203.
const notRetriedOnly = "the write was not retried and may or may not have been applied: read the resource before calling again"

// A write that timed out is not sent again, and its hint holds the not-retried
// note and nothing more: scores_create has no limit, no time window and no fields.
func TestTheHintOfAWriteThatTimedOutIsTheNotRetriedNoteAndNothingMore(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	stalling := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body) // the server notices a closed connection only once the body is read
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(stalling.Close)
	opts := testOptions(t, stalling.URL)
	opts.RequestTimeout = 100 * time.Millisecond
	cs := connectServer(t, langfuse.New(opts), slog.New(slog.DiscardHandler), server.Secrets{Keys: testKeys()}, server.WithWriteMode())

	got := toolErrorOf(t, callExecuteWrite(t, cs, map[string]any{"operationId": "scores_create", "body": scoreBody()})).Error

	if got.Code != "timeout" || got.Retryable || got.Hint != notRetriedOnly {
		t.Fatalf("error = %+v, want a timeout, not retryable, with the hint %q", got, notRetriedOnly)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("Langfuse received %d requests, want 1: a write is never sent again", n)
	}
}

// A canceled write has the same hint. The client of a canceled call reads no
// result, so the hint is checked where it is built. A write operation that does
// take a way of narrowing, on a literal operation (no catalog operation does),
// names it after the note.
func TestTheHintOfACanceledWriteIsTheNotRetriedNoteAndNamesOnlyTheWaysTheOperationHas(t *testing.T) {
	t.Parallel()
	scoresCreate, ok := sharedCatalog(t).Resolve(catalog.Profile{Families: catalog.AllFamilies()}).Lookup("scores_create")
	if !ok {
		t.Fatal("scores_create is not offered")
	}
	withWindow := operationWithTimeWindow()
	withWindow.Method = http.MethodPost
	for name, tc := range map[string]struct {
		op   catalog.Operation
		err  error
		code string
		want string
	}{
		"scores_create, canceled": {scoresCreate, langfuse.ErrCanceled, "canceled", notRetriedOnly},
		"scores_create, timeout":  {scoresCreate, langfuse.ErrTimeout, "timeout", notRetriedOnly},
		"a write with a time window, canceled": {withWindow, langfuse.ErrCanceled, "canceled",
			notRetriedOnly + "; if the call was canceled because it was slow, narrow the query before calling again: a shorter time window"},
		"a write with a time window, timeout": {withWindow, langfuse.ErrTimeout, "timeout",
			notRetriedOnly + "; narrow the query before calling again: a shorter time window"},
	} {
		code, hint := server.WriteFailureOf(tc.op, tc.err)

		if code != tc.code || hint != tc.want {
			t.Errorf("%s: code %q, hint %q; want %q with the hint %q", name, code, hint, tc.code, tc.want)
		}
	}
}

// The hint of a call held by the server's own rate limit or concurrency cap past
// its deadline is not about narrowing, and is unchanged: it was never sent, it is
// retryable, and it says to call again later.
func TestTheHintOfACallHeldByTheServersOwnLimitIsUnchanged(t *testing.T) {
	t.Parallel()
	const want = "wait a few seconds, then call again, one call at a time; the operator sets the limits with " +
		"LANGFUSE_MCP_RATE_LIMIT and LANGFUSE_MCP_MAX_CONCURRENCY"
	cat := sharedCatalog(t).Resolve(catalog.Profile{Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}})
	for _, id := range []string{"trace_get", "trace_list"} {
		op, ok := cat.Lookup(id)
		if !ok {
			t.Fatalf("%s is not offered on 3.80.0", id)
		}

		code, hint := server.FailureOf(op, langfuse.ErrThrottled)

		if code != "timeout" || hint != want {
			t.Errorf("%s: code %q, hint %q; want timeout with the hint %q", id, code, hint, want)
		}
	}
	scoresCreate, ok := sharedCatalog(t).Resolve(catalog.Profile{Families: catalog.AllFamilies()}).Lookup("scores_create")
	if !ok {
		t.Fatal("scores_create is not offered")
	}
	if code, hint := server.WriteFailureOf(scoresCreate, langfuse.ErrThrottled); code != "timeout" || hint != notRetriedOnly+"; "+want {
		t.Errorf("a throttled write: code %q, hint %q; want timeout with the note and %q", code, hint, want)
	}
}

// trace_list serves fields from 3.73.0: on an older deployment it names a lower
// limit and a time window and no field group, for the three failures.
func TestTheHintsOfTraceListOnADeploymentBeforeFieldsNameNoFieldGroup(t *testing.T) {
	t.Parallel()
	legacy := []catalog.Family{catalog.LegacyFamily}
	const (
		tooLarge = "narrow the query: a lower limit or a shorter time window, and page through the rest " + viaPage
		timeout  = "narrow the query before calling again: a lower limit or a shorter time window"
	)
	t.Run("a 413", func(t *testing.T) {
		t.Parallel()
		fake, _ := scriptedLangfuse(t, answer{status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`})
		cs := connectServing(t, fake, "3.72.0", legacy...)

		got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{"operationId": "trace_list"})).Error

		if got.Code != "response_too_large" || got.Hint != tooLarge {
			t.Fatalf("error = %+v, want response_too_large with the hint %q", got, tooLarge)
		}
	})
	t.Run("a timeout", func(t *testing.T) {
		t.Parallel()
		cs := connectServingSlow(t, stallingLangfuse(t), "3.72.0", legacy...)

		got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{"operationId": "trace_list"})).Error

		if got.Code != "timeout" || got.Hint != timeout {
			t.Fatalf("error = %+v, want a timeout with the hint %q", got, timeout)
		}
	})
	t.Run("a canceled call", func(t *testing.T) {
		t.Parallel()
		op, ok := sharedCatalog(t).Resolve(catalog.Profile{Version: "3.72.0", Families: legacy}).Lookup("trace_list")
		if !ok || op.HasQueryParam("fields") {
			t.Fatalf("trace_list offered: %v, serves fields: %v; want offered and not serving fields on 3.72.0", ok, ok && op.HasQueryParam("fields"))
		}

		code, hint := server.FailureOf(op, langfuse.ErrCanceled)

		if want := "if the call was canceled because it was slow, " + timeout; code != "canceled" || hint != want {
			t.Fatalf("code %q, hint %q; want canceled with the hint %q", code, hint, want)
		}
	})
}

// The metrics operations take their narrowing inside the JSON query, so a failed
// call is told to narrow it, through the tool as well as in the truncation hint.
func TestAFailedMetricsCallIsToldToNarrowItsQueryJSON(t *testing.T) {
	t.Parallel()
	const way = "a shorter time range, more filters or a lower config.row_limit in the query JSON"
	call := map[string]any{"operationId": "metrics_metrics", "parameters": map[string]any{"query": `{"view":"traces"}`}}
	t.Run("a 413", func(t *testing.T) {
		t.Parallel()
		fake, _ := scriptedLangfuse(t, answer{status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`})
		cs := connectServing(t, fake, "4.46.0", catalog.AllFamilies()...)

		got := toolErrorOf(t, callExecuteRead(t, cs, call)).Error

		if want := "narrow the query: " + way; got.Code != "response_too_large" || got.Hint != want {
			t.Fatalf("error = %+v, want response_too_large with the hint %q", got, want)
		}
	})
	t.Run("a timeout", func(t *testing.T) {
		t.Parallel()
		cs := connectServingSlow(t, stallingLangfuse(t), "4.46.0", catalog.AllFamilies()...)

		got := toolErrorOf(t, callExecuteRead(t, cs, call)).Error

		if want := "narrow the query before calling again: " + way; got.Code != "timeout" || got.Hint != want {
			t.Fatalf("error = %+v, want a timeout with the hint %q", got, want)
		}
	})
}

// A call held by the server's own rate limit past its deadline keeps its hint, on
// an operation that has ways of narrowing and on one that has none: it was never
// sent, and the hint is about the limits, not about the query.
func TestTheHintOfACallHeldByTheRateLimitIsTheSameForEveryOperation(t *testing.T) {
	t.Parallel()
	const want = "wait a few seconds, then call again, one call at a time; the operator sets the limits with " +
		"LANGFUSE_MCP_RATE_LIMIT and LANGFUSE_MCP_MAX_CONCURRENCY"
	for name, call := range map[string]map[string]any{
		"trace_list, which has ways of narrowing": traceList,
		"trace_get, which has none":               {"operationId": "trace_get", "parameters": map[string]any{"traceId": "t-1"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, calls := scriptedLangfuse(t, answer{status: http.StatusOK, body: `{"data":[],"meta":{}}`})
			opts := clientOptions(t, fake)
			opts.RateLimit, opts.MaxConcurrency = 1, 1 // the next call may leave in a minute
			opts.RequestTimeout = 5 * time.Second
			cs := connectClient(t, langfuse.New(opts), slog.New(slog.DiscardHandler))
			if first := callExecuteRead(t, cs, traceList); first.IsError {
				t.Fatalf("first call returned a tool error: %s", resultText(t, first))
			}

			got := toolErrorOf(t, callExecuteRead(t, cs, call)).Error

			if got.Code != "timeout" || !got.Retryable || got.Hint != want {
				t.Fatalf("error = %+v, want a retryable timeout with the hint %q", got, want)
			}
			if calls.Load() != 1 {
				t.Errorf("Langfuse received %d requests, want 1: the held call is never sent", calls.Load())
			}
		})
	}
}

// A write answered 413, or whose answer is over the bytes this server reads, has
// no way of narrowing either: the hint says so and does not invite another call.
func TestTheHintOfAWriteWhoseAnswerIsTooLargeSaysThatNoParameterMakesItSmaller(t *testing.T) {
	t.Parallel()
	for source, answered := range map[string]answer{
		"a 413 from Langfuse": {status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`},
		"a body over 5 MiB":   {status: http.StatusOK, body: `{"id":"` + strings.Repeat("a", 5<<20) + `"}`},
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			fake, _ := scriptedLangfuse(t, answered)
			cs := connectWrites(t, fake)

			got := toolErrorOf(t, callExecuteWrite(t, cs, map[string]any{"operationId": "scores_create", "body": scoreBody()})).Error

			if got.Code != "response_too_large" || got.Hint != tooLargeNoWay {
				t.Fatalf("error = %+v, want response_too_large with the hint %q", got, tooLargeNoWay)
			}
		})
	}
}
