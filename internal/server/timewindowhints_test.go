package server_test

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam (#204, spec #172 story 16): a hint names "a shorter time window" only for an
// operation that has one, and the catalog says which parameters are windows
// (the from... and to... pairs), not the format date-time. Two date-time
// parameters bound none: the startTime of legacy_observationsV1_get, a lookup hint
// for reading one observation by ID, and the version of datasetItems_list, the
// state of the dataset at that timestamp. Expected hints are literals.

// The two operations with a date-time parameter that bounds no window. 4.46.0
// serves their startTime (from 4.33.0) and version (from 3.153.0).
const (
	observationByID = "legacy_observationsV1_get"
	datasetItems    = "datasetItems_list"
)

// The hints of datasetItems_list (a limit and a page, no fields, no window).
const (
	datasetItemsTooLarge = "narrow the query: a lower limit, and page through the rest " + viaPage
	datasetItemsSlow     = "narrow the query before calling again: a lower limit"
)

// The truncation hint of one observation larger than the result size cap, as a
// literal: its large values clipped, no way of narrowing.
const observationTruncated = "values ending in \"… [clipped]\" were shortened to fit the result size cap, each keeping its start; " +
	"every other value is whole; a single value is larger than the result size cap and cannot be read whole through this server: " +
	"tell the user instead of retrying"

var observationByIDCall = map[string]any{"operationId": observationByID, "parameters": map[string]any{"observationId": "o-1"}}

// canceledLead is the lead-in of the hint of a canceled call.
const canceledLead = "if the call was canceled because it was slow, "

// legacy_observationsV1_get reads one observation by ID: no limit, no fields, no
// paging and, since #204, no time window, so a response too large and a slow call
// are told that no parameter makes it smaller or cheaper, whichever way they happen.
func TestTheHintsOfAnObservationReadByIDSayThatNoParameterNarrowsIt(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	for source, answered := range map[string]answer{
		"a 413 from Langfuse": {status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`},
		"a body over 5 MiB":   {status: http.StatusOK, body: `{"id":"o-1","input":"` + strings.Repeat("a", 5<<20) + `"}`},
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			fake, _ := scriptedLangfuse(t, answered)
			cs := connectServing(t, fake, "4.46.0", dual...)

			got := toolErrorOf(t, callExecuteRead(t, cs, observationByIDCall)).Error

			if got.Code != "response_too_large" || got.Retryable || got.Hint != tooLargeNoWay {
				t.Fatalf("error = %+v, want response_too_large, not retryable, with the hint %q", got, tooLargeNoWay)
			}
			assertNamesNoWayToNarrow(t, got.Hint)
		})
	}
	t.Run("a timeout", func(t *testing.T) {
		t.Parallel()
		cs := connectServingSlow(t, stallingLangfuse(t), "4.46.0", dual...)

		got := toolErrorOf(t, callExecuteRead(t, cs, observationByIDCall)).Error

		if got.Code != "timeout" || got.Retryable || got.Hint != slowNoWay {
			t.Fatalf("error = %+v, want a timeout, not retryable, with the hint %q", got, slowNoWay)
		}
		assertNamesNoWayToNarrow(t, got.Hint)
	})
	// The client of a canceled call reads no result, so the hint is checked where
	// it is built.
	t.Run("a canceled call", func(t *testing.T) {
		t.Parallel()
		op, ok := sharedCatalog(t).Resolve(catalog.Profile{Version: "4.46.0", Families: dual}).Lookup(observationByID)
		if !ok || !op.HasQueryParam("startTime") {
			t.Fatalf("%s offered: %v, serves startTime: %v; want both on 4.46.0", observationByID, ok, ok && op.HasQueryParam("startTime"))
		}

		code, hint := server.FailureOf(op, langfuse.ErrCanceled)

		if code != "canceled" || hint != canceledNoWay {
			t.Fatalf("code %q, hint %q; want canceled with the hint %q", code, hint, canceledNoWay)
		}
		assertNamesNoWayToNarrow(t, hint)
	})
}

// The truncation hint of one observation larger than the result size cap names no
// time window (#177 owns the words, #204 the fact): the observation is clipped, and
// no parameter makes it smaller.
func TestTheTruncationHintOfAnObservationReadByIDNamesNoTimeWindow(t *testing.T) {
	t.Parallel()
	big := jsonOf(t, map[string]any{"id": "o-1", "input": strings.Repeat("a", 100<<10)})
	fake, _ := fakeLangfuse(t, http.StatusOK, big)
	cs := connectServingCapped(t, fake, "4.46.0", catalog.AllFamilies())

	env := decodeResult(t, resultText(t, callExecuteRead(t, cs, observationByIDCall)))

	if !env.Truncated || env.Hint != observationTruncated {
		t.Fatalf("truncated %v, hint %q; want an observation of 100 KiB cut to the smallest cap with the hint %q",
			env.Truncated, env.Hint, observationTruncated)
	}
}

// datasetItems_list pages (limit, page) and takes a version that is a point in time:
// an earlier timestamp is another state of the dataset, not a smaller page. Its
// four hints name a lower limit and the way of paging and no time window, on a
// deployment that serves version (4.46.0) and on one that does not (3.80.0), where
// they are what they were.
func TestTheHintsOfADatasetItemListNameALowerLimitAndNoTimeWindow(t *testing.T) {
	t.Parallel()
	rows := make([]map[string]string, 400)
	for i := range rows {
		rows[i] = map[string]string{"id": strconv.Itoa(i), "input": strings.Repeat("a", 100)}
	}
	page := jsonOf(t, map[string]any{"data": rows, "meta": map[string]any{}})
	dual := catalog.AllFamilies()
	legacy := []catalog.Family{catalog.LegacyFamily}
	call := map[string]any{"operationId": datasetItems}
	for name, deployment := range map[string]struct {
		version  string
		families []catalog.Family
	}{
		"4.46.0, which serves version":      {"4.46.0", dual},
		"3.80.0, which does not serve it":   {"3.80.0", legacy},
		"3.153.0, the release that adds it": {"3.153.0", legacy},
		"3.152.0, the release before it":    {"3.152.0", legacy},
		"4.46.0 on the events-only profile": {"4.46.0", []catalog.Family{catalog.V4ReadFamily, catalog.ExperimentsFamily}},
		"no detected version":               {"", dual},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for source, answered := range map[string]answer{
				"a 413":             {status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`},
				"a body over 5 MiB": {status: http.StatusOK, body: `{"data":"` + strings.Repeat("a", 5<<20) + `"}`},
			} {
				t.Run(source, func(t *testing.T) {
					fake, _ := scriptedLangfuse(t, answered)

					got := toolErrorOf(t, callExecuteRead(t, connectServing(t, fake, deployment.version, deployment.families...), call)).Error

					if got.Code != "response_too_large" || got.Hint != datasetItemsTooLarge {
						t.Errorf("error = %+v, want response_too_large with the hint %q", got, datasetItemsTooLarge)
					}
				})
			}
			t.Run("a timeout", func(t *testing.T) {
				cs := connectServingSlow(t, stallingLangfuse(t), deployment.version, deployment.families...)

				got := toolErrorOf(t, callExecuteRead(t, cs, call)).Error

				if got.Code != "timeout" || got.Hint != datasetItemsSlow {
					t.Errorf("error = %+v, want a timeout with the hint %q", got, datasetItemsSlow)
				}
			})
			// The client of a canceled call reads no result, so the hint is checked
			// where it is built.
			t.Run("a canceled call", func(t *testing.T) {
				op, ok := sharedCatalog(t).Resolve(catalog.Profile{Version: deployment.version, Families: deployment.families}).Lookup(datasetItems)
				if !ok {
					t.Fatalf("%s is not offered", datasetItems)
				}

				code, hint := server.FailureOf(op, langfuse.ErrCanceled)

				if want := canceledLead + datasetItemsSlow; code != "canceled" || hint != want {
					t.Errorf("code %q, hint %q; want canceled with the hint %q", code, hint, want)
				}
			})
			t.Run("a page cut at the cap", func(t *testing.T) {
				fake, _ := fakeLangfuse(t, http.StatusOK, page)

				env := decodeResult(t, resultText(t, callExecuteRead(t, connectServingCapped(t, fake, deployment.version, deployment.families), call)))

				const wantTail = "narrow the query: a lower limit; page through the rest " + viaPage
				if !env.Truncated || !strings.HasPrefix(env.Hint, "only the first ") || !strings.HasSuffix(env.Hint, wantTail) {
					t.Errorf("truncated %v, hint %q; want it to start with \"only the first \" and end with %q", env.Truncated, env.Hint, wantTail)
				}
			})
		})
	}
}

// What counts as a time window is the catalog's list, not a parameter's name or
// format: a date-time parameter named fromVersion or to, or the startTime of an
// operation that is not the one listed, is none, so the operation is told it has
// no way of narrowing (or only the other ways it has).
func TestAParameterThatOnlyLooksLikeATimeWindowIsNotRecommended(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"fromVersion", "to", "from", "startTime", "version", "since"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			op := operationWith()
			op.Params = append(op.Params, catalog.Param{Name: name, In: "query", Schema: catalog.Schema{Type: "string", Format: "date-time"}})
			withLimit := operationWith("limit")
			withLimit.Params = append(withLimit.Params, op.Params...)
			_, timeoutHint := server.FailureOf(op, langfuse.ErrTimeout)
			_, canceledHint := server.FailureOf(op, langfuse.ErrCanceled)

			if got := server.TooLargeHint(op); got != tooLargeNoWay {
				t.Errorf("too large: hint = %q, want %q", got, tooLargeNoWay)
			}
			if timeoutHint != slowNoWay || canceledHint != canceledNoWay {
				t.Errorf("timeout hint %q, canceled hint %q; want %q and %q", timeoutHint, canceledHint, slowNoWay, canceledNoWay)
			}
			if got := server.TooLargeHint(withLimit); got != "narrow the query: a lower limit" {
				t.Errorf("with a limit: too large: hint = %q, want only the limit", got)
			}
			if got := server.TruncationHint(op); strings.Contains(got, "time window") {
				t.Errorf("truncation hint %q names a time window", got)
			}
		})
	}
}

// Every other operation with a time-window parameter keeps the window in the three
// hints, and only those do: the operations told to use a shorter time window on
// each deployment profile are exactly these, as literals. None is an operation
// with a date-time parameter that bounds no window.
func TestTheOperationsToldToUseATimeWindowAreExactlyTheOnesWithOne(t *testing.T) {
	t.Parallel()
	dualWindows := []string{
		"experiments_list", "experiments_listItems", "legacy_observationsV1_getMany", "observations_getMany", "prompts_list",
		"scores_get-many", "scoresV3_getManyV3", "sessions_list", "trace_list", "unstable_skills_list",
	}
	legacy := []catalog.Family{catalog.LegacyFamily}
	for name, tc := range map[string]struct {
		profile catalog.Profile
		want    []string
	}{
		"4.46.0 dual": {catalog.Profile{Version: "4.46.0", Families: catalog.AllFamilies()}, dualWindows},
		"3.80.0": {catalog.Profile{Version: "3.80.0", Families: legacy},
			[]string{"legacy_observationsV1_getMany", "prompts_list", "scores_get-many", "sessions_list", "trace_list"}},
		// score_get is served up to before 3.53.0 and metrics_daily up to before 3.62.0, scores_get-many from 3.53.0.
		"3.52.0": {catalog.Profile{Version: "3.52.0", Families: legacy},
			[]string{"legacy_observationsV1_getMany", "metrics_daily", "prompts_list", "score_get", "sessions_list", "trace_list"}},
	} {
		var got []string
		for _, op := range sharedCatalog(t).Resolve(tc.profile).Operations() {
			if !op.IsRead() {
				continue
			}
			_, canceledHint := server.FailureOf(op, langfuse.ErrCanceled)
			named := []bool{
				strings.Contains(server.TooLargeHint(op), "time window"), strings.Contains(server.TimeoutHint(op), "time window"),
				strings.Contains(canceledHint, "time window"), strings.Contains(server.TruncationHint(op), "time window"),
			}
			if slices.Contains(named, true) && slices.Contains(named, false) {
				t.Errorf("%s: %s names the time window in some of its four hints and not in all: %v", name, op.ID, named)
			}
			if named[0] {
				got = append(got, op.ID)
			}
		}
		slices.Sort(got)
		if want := slices.Sorted(slices.Values(tc.want)); !slices.Equal(got, want) {
			t.Errorf("%s: operations told to use a time window = %v, want %v", name, got, want)
		}
		for _, id := range []string{observationByID, datasetItems} {
			if slices.Contains(got, id) {
				t.Errorf("%s: %s is told to use a time window, but its date-time parameter bounds none", name, id)
			}
		}
	}
}
