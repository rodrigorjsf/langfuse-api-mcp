package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam (#194, spec #172 stories 16 and 64): a hint that recommends fewer fields
// names the field groups of the operation, from the same compiled source as the
// guidance, and no hint recommends fields for an operation that does not serve
// them on the connected deployment. The words about a lower limit and a time
// window are the subject of narrowinghints_test.go (#203); here they follow the
// ways the operation has.

// The groups of each operation as its hints name them, as literals (the
// release specs, docs/research/langfuse.md section 1.5).
const (
	traceGroups        = "core, io, scores, observations and metrics"
	observationsGroups = "core, basic, time, io, metadata, model, usage, prompt, metrics and trace_context from Langfuse 3.175.0"
	scoresGroups       = "score and trace"
	scoresV3Groups     = "details, subject and annotation"
	experimentsGroups  = "core, metadata and scores"
	itemsGroups        = "core, dataset, io, metadata, itemMetadata, experimentMetadata and scores"
)

// fieldGroupsInHints is the groups each operation with fields names in a hint.
var fieldGroupsInHints = map[string]string{
	"trace_list": traceGroups, "trace_get": traceGroups, "observations_getMany": observationsGroups,
	"scores_get-many": scoresGroups, "scoresV3_getManyV3": scoresV3Groups,
	"experiments_list": experimentsGroups, "experiments_listItems": itemsGroups,
}

// paramsFor are the parameters a call of the operation needs to be valid.
func paramsFor(operationID string) map[string]any {
	switch operationID {
	case "trace_get":
		return map[string]any{"traceId": "t-1"}
	case "experiments_list", "experiments_listItems":
		return map[string]any{"fromStartTime": "2025-01-01T00:00:00Z"}
	}
	return nil
}

// readCall is a valid execute_read call of operationID.
func readCall(operationID string) map[string]any {
	call := map[string]any{"operationId": operationID}
	if p := paramsFor(operationID); p != nil {
		call["parameters"] = p
	}
	return call
}

// connectServingSlow is connectServing with a request deadline of 100 ms.
func connectServingSlow(t *testing.T, fake *httptest.Server, version string, families ...catalog.Family) *mcp.ClientSession {
	t.Helper()
	cat := sharedCatalog(t)
	opts := testOptions(t, fake.URL)
	opts.RequestTimeout = 100 * time.Millisecond
	return startCatalog(t, cat.Resolve(catalog.Profile{Version: version, Families: families}), langfuse.New(opts),
		slog.New(slog.DiscardHandler), server.Secrets{Keys: testKeys()}, langfuse.DeploymentProfile{Version: version, Families: families})
}

// The truncation hint of a result cut to the cap names the groups of the
// operation that served it.
func TestTheTruncationHintNamesTheFieldGroupsOfTheOperation(t *testing.T) {
	t.Parallel()
	big := jsonOf(t, map[string]any{"data": []map[string]string{{"id": "x", "input": strings.Repeat("a", 100<<10)}}})
	tests := map[string]struct {
		operationID, version, want string
	}{
		"trace_list": {"trace_list", "4.46.0",
			"narrow the query: fewer field groups in fields (core, io, scores, observations and metrics), a lower limit or a shorter time window"},
		"trace_get": {"trace_get", "4.46.0", "narrow the query: fewer field groups in fields (" + traceGroups + ")"},
		"observations_getMany": {"observations_getMany", "4.46.0",
			"narrow the query: fewer field groups in fields (" + observationsGroups + "), a lower limit or a shorter time window"},
		"scores_get-many": {"scores_get-many", "4.46.0",
			"narrow the query: fewer field groups in fields (score and trace), a lower limit or a shorter time window"},
		"scoresV3_getManyV3": {"scoresV3_getManyV3", "4.46.0",
			"narrow the query: fewer field groups in fields (details, subject and annotation), a lower limit or a shorter time window"},
		"experiments_list": {"experiments_list", "4.46.0",
			"narrow the query: fewer field groups in fields (core, metadata and scores), a lower limit or a shorter time window"},
		"experiments_listItems": {"experiments_listItems", "4.46.0",
			"narrow the query: fewer field groups in fields (" + itemsGroups + "), a lower limit or a shorter time window"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, _ := fakeLangfuse(t, http.StatusOK, big)
			cs := connectServingCapped(t, fake, tc.version, catalog.AllFamilies())

			env := decodeResult(t, resultText(t, callExecuteRead(t, cs, readCall(tc.operationID))))

			if !env.Truncated || !strings.Contains(env.Hint, tc.want) {
				t.Fatalf("truncated %v, hint %q; want it to contain %q", env.Truncated, env.Hint, tc.want)
			}
		})
	}
}

// The hint of an answer too large to read names the groups of the operation
// where it serves fields, and no group where it does not.
func TestTheHintOfAResponseTooLargeNamesTheFieldGroupsOnlyOfAnOperationThatServesThem(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	legacy := []catalog.Family{catalog.LegacyFamily}
	const window = ", a lower limit or a shorter time window"
	tests := map[string]struct {
		operationID, version string
		families             []catalog.Family
		want                 string
	}{
		"trace_list": {"trace_list", "3.80.0", legacy,
			"narrow the query: fewer field groups in fields (" + traceGroups + ")" + window + ", and page through the rest " + viaPage},
		"trace_get where fields is served": {"trace_get", "3.166.0", legacy,
			"narrow the query: fewer field groups in fields (" + traceGroups + ")"},
		"trace_get on 3.80.0, which does not serve fields": {"trace_get", "3.80.0", legacy, tooLargeNoWay},
		"observations_getMany": {"observations_getMany", "4.46.0", dual,
			"narrow the query: fewer field groups in fields (" + observationsGroups + ")" + window + ", and page through the rest " + viaCursor},
		"scores_get-many": {"scores_get-many", "3.144.0", legacy,
			"narrow the query: fewer field groups in fields (" + scoresGroups + ")" + window + ", and page through the rest " + viaPage},
		"scores_get-many before fields": {"scores_get-many", "3.143.0", legacy,
			"narrow the query: a lower limit or a shorter time window, and page through the rest " + viaPage},
		"scoresV3_getManyV3": {"scoresV3_getManyV3", "4.46.0", dual,
			"narrow the query: fewer field groups in fields (" + scoresV3Groups + ")" + window + ", and page through the rest " + viaCursor},
		"experiments_list": {"experiments_list", "4.46.0", dual,
			"narrow the query: fewer field groups in fields (" + experimentsGroups + ")" + window + ", and page through the rest " + viaCursor},
		"experiments_listItems": {"experiments_listItems", "4.46.0", dual,
			"narrow the query: fewer field groups in fields (" + itemsGroups + ")" + window + ", and page through the rest " + viaCursor},
		"an operation without fields": {"datasets_list", "4.46.0", dual,
			"narrow the query: a lower limit, and page through the rest " + viaPage},
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

				got := toolErrorOf(t, callExecuteRead(t, cs, readCall(tc.operationID))).Error

				if got.Code != "response_too_large" || got.Hint != tc.want {
					t.Fatalf("error = %+v, want response_too_large with the hint %q", got, tc.want)
				}
			})
		}
	}
}

// The hint of a call that got no answer in time names the groups of the
// operation where it serves fields, and none where it does not.
func TestTheHintOfATimeoutNamesTheFieldGroupsOnlyOfAnOperationThatServesThem(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	legacy := []catalog.Family{catalog.LegacyFamily}
	tests := map[string]struct {
		operationID, version string
		families             []catalog.Family
		want                 string
	}{
		"trace_list": {"trace_list", "4.46.0", dual,
			"narrow the query before calling again: fewer field groups in fields (" + traceGroups + "), a lower limit or a shorter time window"},
		"trace_get where fields is served": {"trace_get", "3.166.0", legacy,
			"narrow the query before calling again: fewer field groups in fields (" + traceGroups + ")"},
		"trace_get on 3.80.0, which does not serve fields": {"trace_get", "3.80.0", legacy, slowNoWay},
		"observations_getMany": {"observations_getMany", "4.46.0", dual,
			"narrow the query before calling again: fewer field groups in fields (" + observationsGroups + "), a lower limit or a shorter time window"},
		"scoresV3_getManyV3": {"scoresV3_getManyV3", "4.46.0", dual,
			"narrow the query before calling again: fewer field groups in fields (" + scoresV3Groups + "), a lower limit or a shorter time window"},
		"an operation without fields": {"datasets_list", "4.46.0", dual,
			"narrow the query before calling again: a lower limit"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cs := connectServingSlow(t, stallingLangfuse(t), tc.version, tc.families...)

			got := toolErrorOf(t, callExecuteRead(t, cs, readCall(tc.operationID))).Error

			if got.Code != "timeout" || got.Hint != tc.want {
				t.Fatalf("error = %+v, want timeout with the hint %q", got, tc.want)
			}
		})
	}
}

// A call canceled while it waited has the hint of a timeout behind a lead-in.
// The client of a canceled call reads no result, so the hint is checked where it
// is built.
func TestTheHintOfACanceledCallNamesTheFieldGroupsOnlyOfAnOperationThatServesThem(t *testing.T) {
	t.Parallel()
	cat := sharedCatalog(t)
	older := cat.Resolve(catalog.Profile{Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}})
	traceList, _ := cat.Lookup("trace_list")
	traceGet, _ := older.Lookup("trace_get")

	for name, tc := range map[string]struct {
		op   catalog.Operation
		want string
	}{
		"trace_list": {traceList,
			"if the call was canceled because it was slow, narrow the query before calling again: " +
				"fewer field groups in fields (" + traceGroups + "), a lower limit or a shorter time window"},
		"trace_get on 3.80.0": {traceGet, canceledNoWay},
	} {
		code, hint := server.FailureOf(tc.op, langfuse.ErrCanceled)

		if code != "canceled" || hint != tc.want {
			t.Errorf("%s: code %q, hint %q; want canceled with the hint %q", name, code, hint, tc.want)
		}
	}
}

// Walks every read operation of each deployment profile of the fixtures: a hint
// names fields, and the groups the guidance names, exactly for an operation that
// serves fields there.
func TestNoHintRecommendsFieldsForAnOperationThatDoesNotServeThemOnTheDeployment(t *testing.T) {
	t.Parallel()
	cat := sharedCatalog(t)
	legacy := []catalog.Family{catalog.LegacyFamily}
	for name, profile := range map[string]catalog.Profile{
		"3.80.0":                   {Version: "3.80.0", Families: legacy},
		"3.165.0":                  {Version: "3.165.0", Families: legacy},
		"3.225.11":                 {Version: "3.225.11", Families: legacy},
		"4.46.0 dual":              {Version: "4.46.0", Families: catalog.AllFamilies()},
		"4.46.0 events_only":       {Version: "4.46.0", Families: []catalog.Family{catalog.V4ReadFamily, catalog.ExperimentsFamily}},
		"no detected version":      {Families: catalog.AllFamilies()},
		"3.141.0, v4 read at last": {Version: "3.141.0", Families: []catalog.Family{catalog.V4ReadFamily}},
	} {
		resolved := cat.Resolve(profile)
		walked := 0
		for _, op := range resolved.Operations() {
			if !op.IsRead() {
				continue
			}
			walked++
			for what, hint := range map[string]string{
				"too large": server.TooLargeHint(op), "slow": server.TimeoutHint(op),
			} {
				serves := op.HasQueryParam("fields")
				if got := strings.Contains(hint, "fields"); got != serves {
					t.Errorf("%s: the %s hint of %s names fields: %v, the operation serves them: %v (%q)",
						name, what, op.ID, got, serves, hint)
				}
				if want := fieldGroupsInHints[op.ID]; serves && !strings.Contains(hint, "("+want+")") {
					t.Errorf("%s: the %s hint of %s does not name the groups %q: %q", name, what, op.ID, want, hint)
				}
			}
		}
		if walked == 0 {
			t.Errorf("%s: the walk found no read operation", name)
		}
	}
}

// namesFieldsForTheTool reports whether hint names fields anywhere but as a
// parameter of the execute_read call it sends the agent to: get_trace_tree takes
// no fields argument.
func namesFieldsForTheTool(hint string) bool {
	return strings.Contains(strings.ReplaceAll(hint, "execute_read with observations_getMany (traceId, fields, a lower limit, cursor)", ""), "fields")
}

// get_trace_tree takes no fields argument, so no hint of a failed call of the
// tool recommends it, whatever the operation it reads serves.
func TestTheHintsOfAFailedTraceTreeNeverRecommendFields(t *testing.T) {
	t.Parallel()
	dual := catalog.AllFamilies()
	legacy := []catalog.Family{catalog.LegacyFamily}
	for name, tc := range map[string]struct {
		version  string
		families []catalog.Family
	}{
		"reading Observations v2": {"4.46.0", dual},
		"reading the legacy list": {"3.80.0", legacy},
	} {
		for source, answered := range map[string]answer{
			"a 413 from Langfuse": {status: http.StatusRequestEntityTooLarge, body: `{"message":"Response too large"}`},
			"a body over 5 MiB":   {status: http.StatusOK, body: `{"data":[{"id":"a","name":"` + strings.Repeat("a", 5<<20) + `"}],"meta":{}}`},
		} {
			t.Run(name+", "+source, func(t *testing.T) {
				t.Parallel()
				fake, _ := scriptedLangfuse(t, answered)
				cs := connectServing(t, fake, tc.version, tc.families...)

				got := toolErrorOf(t, callTool(t, cs, "get_trace_tree", map[string]any{"traceId": "t-1"})).Error

				if got.Code != "response_too_large" || namesFieldsForTheTool(got.Hint) {
					t.Fatalf("error = %+v, want response_too_large without fields as a way to narrow the tool's call", got)
				}
			})
		}
		t.Run(name+", a timeout", func(t *testing.T) {
			t.Parallel()
			cs := connectServingSlow(t, stallingLangfuse(t), tc.version, tc.families...)

			got := toolErrorOf(t, callTool(t, cs, "get_trace_tree", map[string]any{"traceId": "t-1"})).Error

			if got.Code != "timeout" || namesFieldsForTheTool(got.Hint) {
				t.Fatalf("error = %+v, want a timeout whose hint does not recommend fields as an argument of the tool", got)
			}
		})
	}
}

// The hints of the trace tree read from Observations v2 send the agent to
// execute_read with observations_getMany and its fields parameter. They are
// right wherever that read exists: the operation serves fields on every
// deployment that has the v4 read family on, and the legacy list the other
// source reads has none, so its hints name none.
func TestTheTraceTreeHintsThatNameFieldsNameAnOperationThatServesThemOnTheDeployment(t *testing.T) {
	t.Parallel()
	cat := sharedCatalog(t)
	for _, version := range []string{"3.141.0", "4.0.0", "4.46.0", ""} {
		op, ok := cat.Resolve(catalog.Profile{Version: version, Families: []catalog.Family{catalog.V4ReadFamily}}).Lookup("observations_getMany")
		if !ok || !op.HasQueryParam("fields") {
			t.Errorf("on %q observations_getMany is offered: %v, serves fields: %v; the trace tree hints name both", version, ok, ok && op.HasQueryParam("fields"))
		}
	}
	legacy, ok := cat.Resolve(catalog.Profile{Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}}).Lookup("legacy_observationsV1_getMany")
	if !ok || legacy.HasQueryParam("fields") {
		t.Errorf("legacy_observationsV1_getMany is offered: %v, serves fields: %v; the legacy trace tree hints name none", ok, ok && legacy.HasQueryParam("fields"))
	}
}
