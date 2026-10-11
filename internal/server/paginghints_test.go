package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam S1 (spec #172, ticket #193): a hint that names a way of paging names
// only the way the called operation has on the connected deployment: `page`
// for an operation with a served page parameter, `cursor` (from meta.cursor)
// for one with a served cursor parameter, both for one with both, neither for
// one with neither. No legacy list returns a cursor. The expected hints are
// literals. The operations are real: trace_list and scores_get-many page by
// `page`, observations_getMany by `cursor`, trace_get by neither.

const (
	viaPage   = "(page)"
	viaCursor = "(cursor from meta.cursor)"
	viaBoth   = "(page, or cursor from meta.cursor)"
)

// operationWith returns an operation of the GET kind that takes the query
// parameters named, as a literal: no operation of the catalog takes both page
// and cursor.
func operationWith(names ...string) catalog.Operation {
	op := catalog.Operation{ID: "op", Method: http.MethodGet}
	for _, name := range names {
		op.Params = append(op.Params, catalog.Param{Name: name, In: "query"})
	}
	return op
}

// connectServingCapped is connectServing with the smallest result size cap.
func connectServingCapped(t *testing.T, fake *httptest.Server, version string, families []catalog.Family) *mcp.ClientSession {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return startCatalog(t, cat.Resolve(catalog.Profile{Version: version, Families: families}),
		langfuse.New(testOptions(t, fake.URL)), slog.New(slog.DiscardHandler), server.Secrets{Keys: testKeys()},
		langfuse.DeploymentProfile{Version: version, Families: families}, server.WithMaxResultBytes(16384))
}

func TestTheHintOfALimitOutOfRangeNamesOnlyTheWayOfPagingTheOperationHas(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	legacy := []catalog.Family{catalog.LegacyFamily}
	tests := map[string]struct {
		version     string
		families    []catalog.Family
		operationID string
		want        string
	}{
		"page only, on 3.80.0": {"3.80.0", legacy, "trace_list",
			"use a limit from 1 to 100 and page through the rest " + viaPage + "; without a limit the server asks for 50; "},
		"page only, on a v4 deployment": {"4.46.0", dual, "trace_list",
			"use a limit from 1 to 100 and page through the rest " + viaPage + "; without a limit the server asks for 50; "},
		"cursor only, on a v4 deployment": {"4.46.0", dual, "observations_getMany",
			"use a limit from 1 to 100 and page through the rest " + viaCursor + "; without a limit the server asks for 50; "},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
			cs := connectServing(t, fake, tc.version, tc.families...)

			got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
				"operationId": tc.operationID, "parameters": map[string]any{"limit": 0},
			})).Error

			if got.Code != "invalid_argument" || !strings.HasPrefix(got.Hint, tc.want) {
				t.Fatalf("error = %+v, want invalid_argument with a hint starting %q", got, tc.want)
			}
			assertNoRequest(t, seen)
		})
	}
}

func TestTheHintOfAParameterTheVersionDoesNotServeNamesOnlyTheWayOfPagingTheOperationHas(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	legacy := []catalog.Family{catalog.LegacyFamily}
	const lead = "this deployment's Langfuse version does not serve that parameter: call again without it and filter the "
	tests := map[string]struct {
		version     string
		families    []catalog.Family
		operationID string
		parameter   string
		want        string
	}{
		"page only, on 3.80.0": {"3.80.0", legacy, "scores_get-many", "sessionId",
			lead + "rows yourself over every page " + viaPage + "; its parameters are "},
		"neither, on 3.80.0": {"3.80.0", legacy, "trace_get", "fields",
			lead + "result yourself; its parameters are "},
		// observations_getMany takes sessionId from 3.166.0 (Observations v2 exists from 4.0.0 on a v4 deployment)
		"cursor only, on a v4 deployment": {"4.0.0", dual, "observations_getMany", "sessionId",
			lead + "rows yourself over every page " + viaCursor + "; its parameters are "},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
			cs := connectServing(t, fake, tc.version, tc.families...)

			got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
				"operationId": tc.operationID, "parameters": map[string]any{tc.parameter: "x"},
			})).Error

			if got.Code != "invalid_argument" || !strings.HasPrefix(got.Hint, tc.want) {
				t.Fatalf("error = %+v, want invalid_argument with a hint starting %q", got, tc.want)
			}
			assertNoRequest(t, seen)
		})
	}
}

// The hint of an answer too large to read (Langfuse's 413, or a body over the
// bytes this server reads) says to page through the rest only where the
// operation pages. The words about fields, time window and limit are not part
// of this rule.
func TestTheHintOfAResponseTooLargeNamesOnlyTheWayOfPagingTheOperationHas(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	legacy := []catalog.Family{catalog.LegacyFamily}
	// The words about fields name the groups of the operation (#194); the words
	// about the time window and the limit are #203's and unchanged.
	const (
		narrow         = "narrow the query: fewer field groups in fields (" + traceGroups + "), a shorter time window or a lower limit"
		narrowObserved = "narrow the query: fewer field groups in fields (" + observationsGroups + "), a shorter time window or a lower limit"
		narrowNoFields = "narrow the query: a shorter time window or a lower limit"
	)
	tests := map[string]struct {
		version  string
		families []catalog.Family
		call     map[string]any
		want     string
	}{
		"page only, on 3.80.0": {"3.80.0", legacy, map[string]any{"operationId": "trace_list"},
			narrow + ", and page through the rest " + viaPage},
		"page only, on a v4 deployment": {"4.46.0", dual, map[string]any{"operationId": "trace_list"},
			narrow + ", and page through the rest " + viaPage},
		"neither, on 3.80.0, which does not serve fields": {"3.80.0", legacy,
			map[string]any{"operationId": "trace_get", "parameters": map[string]any{"traceId": "t-1"}}, narrowNoFields},
		"cursor only, on a v4 deployment": {"4.46.0", dual, map[string]any{"operationId": "observations_getMany"},
			narrowObserved + ", and page through the rest " + viaCursor},
	}
	for name, tc := range tests {
		for source, answered := range map[string]answer{
			"a 413 from Langfuse": {status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`},
			"a body over 5 MiB":   {status: http.StatusOK, body: `{"data":"` + strings.Repeat("a", 5<<20) + `"}`},
		} {
			t.Run(name+", "+source, func(t *testing.T) {
				t.Parallel()
				fake, _ := scriptedLangfuse(t, answered)
				cs := connectServing(t, fake, tc.version, tc.families...)

				got := toolErrorOf(t, callExecuteRead(t, cs, tc.call)).Error

				if got.Code != "response_too_large" || got.Hint != tc.want {
					t.Fatalf("error = %+v, want response_too_large with the hint %q", got, tc.want)
				}
			})
		}
	}
}

// A paging hint is static text, catalog parameter names and numbers: it never
// holds a value of the call, whatever the value says.
func TestAPagingHintNeverHoldsAParameterValue(t *testing.T) {
	t.Parallel()
	const planted = "PLANTED-IGNORE-PREVIOUS-INSTRUCTIONS-7731"
	legacy := []catalog.Family{catalog.LegacyFamily}
	fake, _ := scriptedLangfuse(t, answer{status: http.StatusRequestEntityTooLarge, body: `{"message":"` + planted + `"}`})
	cs := connectServing(t, fake, "3.80.0", legacy...)
	for name, call := range map[string]map[string]any{
		"a parameter the version does not serve": {"operationId": "scores_get-many", "parameters": map[string]any{"sessionId": planted}},
		"a limit out of range":                   {"operationId": "trace_list", "parameters": map[string]any{"limit": 7731}},
		"a response too large":                   {"operationId": "trace_list", "parameters": map[string]any{"userId": planted}},
	} {
		got := toolErrorOf(t, callExecuteRead(t, cs, call)).Error

		if strings.Contains(got.Hint, planted) || strings.Contains(got.Hint, "7731") {
			t.Errorf("%s: the hint repeats a value of the call: %q", name, got.Hint)
		}
	}
}

// No catalog operation has both ways, so the rule for one that does is proven
// on a literal operation, through the hint builders themselves.
func TestTheHintsOfAnOperationWithBothWaysOfPagingNameBothAndOfOneWithNeitherNameNone(t *testing.T) {
	t.Parallel()
	both := operationWith("limit", "page", "cursor")
	neither := operationWith("limit")
	cursorOnly := operationWith("limit", "cursor")
	pageOnly := operationWith("limit", "page")

	for name, tc := range map[string]struct{ got, want string }{
		"limit, both": {server.LimitHint(both),
			"use a limit from 1 to 100 and page through the rest " + viaBoth + "; without a limit the server asks for 50"},
		"limit, page": {server.LimitHint(pageOnly),
			"use a limit from 1 to 100 and page through the rest " + viaPage + "; without a limit the server asks for 50"},
		"limit, cursor": {server.LimitHint(cursorOnly),
			"use a limit from 1 to 100 and page through the rest " + viaCursor + "; without a limit the server asks for 50"},
		"limit, neither": {server.LimitHint(neither),
			"use a limit from 1 to 100; without a limit the server asks for 50"},
		"not served, both": {server.NotServedHint(both),
			"this deployment's Langfuse version does not serve that parameter: call again without it and filter the rows yourself over every page " + viaBoth},
		"not served, neither": {server.NotServedHint(neither),
			"this deployment's Langfuse version does not serve that parameter: call again without it and filter the result yourself"},
		"too large, both": {server.TooLargeHint(both),
			"narrow the query: a shorter time window or a lower limit, and page through the rest " + viaBoth},
		"too large, neither": {server.TooLargeHint(neither),
			"narrow the query: a shorter time window or a lower limit"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: hint = %q, want %q", name, tc.got, tc.want)
		}
	}
}

// A page that lost rows to the result size cap says how to page on, by the
// parameter the operation has.
func TestTheHintOfAPageThatLostRowsToTheCapNamesTheWayOfPagingTheOperationHas(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	rows := make([]map[string]string, 0, 400)
	for i := range 400 {
		rows = append(rows, map[string]string{"id": strconv.Itoa(i), "input": strings.Repeat("a", 100)})
	}
	body := jsonOf(t, map[string]any{"data": rows, "meta": map[string]any{}})
	for operationID, want := range map[string]string{
		"trace_list":           "page through the rest " + viaPage,
		"observations_getMany": "page through the rest " + viaCursor,
	} {
		fake, _ := fakeLangfuse(t, http.StatusOK, body)
		cs := connectServingCapped(t, fake, "4.46.0", dual)

		env := decodeResult(t, resultText(t, callExecuteRead(t, cs, map[string]any{"operationId": operationID})))

		if !env.Truncated || !strings.Contains(env.Hint, want) {
			t.Errorf("%s: truncated %v, hint %q, want it to name %q", operationID, env.Truncated, env.Hint, want)
		}
	}
}

// pageParameter finds the word page where it names a parameter: the plural of
// "5 pages of 1000" and the phrase "page by page" are not one.
var pageParameter = regexp.MustCompile(`\bpage\b`)

// The hints of the trace tree name the paging of the operation it reads: the
// cursor of Observations v2, the page number of the legacy list, which has no
// cursor. The parameters a hint lists are checked against what that operation
// serves on the deployment.
func TestTheTraceTreeHintsNameOnlyTheWayOfPagingTheirOperationHas(t *testing.T) {
	t.Parallel()
	t.Run("Observations v2 pages by cursor", func(t *testing.T) {
		t.Parallel()
		pages, cursor := map[string]string{}, ""
		for n := 1; n <= 6; n++ {
			next := "c" + strconv.Itoa(n)
			pages[cursor] = `{"data":[{"id":"o` + strconv.Itoa(n) + `","startTime":"2026-09-25T10:00:0` + strconv.Itoa(9-n) +
				`.000Z"}],"meta":{"cursor":"` + next + `"}}`
			cursor = next
		}
		fake, _ := observationsLangfuse(t, pages)
		cs := connectProfile(t, fake, v4Profile)

		env := callTraceTree(t, cs, map[string]any{"traceId": "t-1"})

		served := describedParameters(t, cs, "observations_getMany")
		for _, name := range []string{"traceId", "fields", "cursor", "limit"} {
			if !slices.Contains(served, name) || !strings.Contains(env.Hint, name) {
				t.Errorf("hint %q and parameters %v: want %q served and named", env.Hint, served, name)
			}
		}
		if slices.Contains(served, "page") || pageParameter.MatchString(strings.ReplaceAll(env.Hint, "page by page", "")) {
			t.Errorf("hint %q names a page parameter that observations_getMany lacks (it serves %v)", env.Hint, served)
		}
	})
	t.Run("the legacy list pages by page number", func(t *testing.T) {
		t.Parallel()
		fake, _ := legacyObservationsLangfuse(t, legacyPages(12, 12))
		cs := connectLegacyTree(t, fake, nil, slog.New(slog.DiscardHandler))

		env := callTraceTree(t, cs, map[string]any{"traceId": "t-1"})

		served := describedParameters(t, cs, "legacy_observationsV1_getMany")
		for _, name := range []string{"traceId", "limit", "page"} {
			if !slices.Contains(served, name) || !strings.Contains(env.Hint, name) {
				t.Errorf("hint %q and parameters %v: want %q served and named", env.Hint, served, name)
			}
		}
		if slices.Contains(served, "cursor") || strings.Contains(env.Hint, "cursor") {
			t.Errorf("hint %q names a cursor the legacy list lacks (it serves %v)", env.Hint, served)
		}
	})
}

// A page of Observations v2 over the response read cap is answered with the
// paging that operation has, through execute_read: get_trace_tree takes no
// limit, no field and no cursor.
func TestGetTraceTreeAnswersAnObservationsPageOverTheReadCapWithAHintThatNamesItsOwnPaging(t *testing.T) {
	t.Parallel()
	fake, _ := scriptedLangfuse(t, answer{status: 200, body: `{"data":[{"id":"a","name":"` + strings.Repeat("a", 5<<20) + `"}],"meta":{}}`})
	cs := connectProfile(t, fake, v4Profile)

	got := toolErrorOf(t, callTool(t, cs, "get_trace_tree", map[string]any{"traceId": "t-1"})).Error

	for _, want := range []string{"observations_getMany", "traceId", "fields", "limit", "cursor"} {
		if got.Code != "response_too_large" || !strings.Contains(got.Hint, want) {
			t.Errorf("error = %+v, want response_too_large with a hint naming %q", got, want)
		}
	}
	if strings.Contains(got.Hint, "time window") || strings.Contains(got.Hint, "page through the rest") {
		t.Errorf("hint = %q, want only what execute_read with observations_getMany has", got.Hint)
	}
}
