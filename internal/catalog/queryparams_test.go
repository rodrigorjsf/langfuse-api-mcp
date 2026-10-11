package catalog_test

import (
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam S3 (spec #172, tickets #177 and #204): the server words its truncation hint
// from the query parameters an operation serves on the connected deployment. A
// time window is a parameter the catalog names as one (timewindow.go), not any
// date-time parameter: the startTime of legacy_observationsV1_get and the version
// of datasetItems_list have that format and bound nothing.
func TestAnOperationReportsTheQueryParametersAndTimeWindowsItServes(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	legacy := catalog.Profile{Version: "3.225.11", Families: []catalog.Family{catalog.LegacyFamily}}
	dual := catalog.Profile{Version: "4.46.0", Families: catalog.AllFamilies()}
	for _, tc := range []struct {
		name        string
		profile     catalog.Profile
		operationID string
		fields      bool
		limit       bool
		timeWindow  bool
	}{
		{"trace_get does not serve fields on 3.80.0", catalog.Profile{Version: "3.80.0", Families: legacy.Families}, "trace_get", false, false, false},
		{"trace_get serves fields on 3.225.11", legacy, "trace_get", true, false, false},
		{"trace_list has fields, a limit and a time window", legacy, "trace_list", true, true, true},
		{"datasets_list has a limit and no fields or time window", legacy, "datasets_list", false, true, false},
		{"legacy_observationsV1_get serves a startTime that is no window", dual, "legacy_observationsV1_get", false, false, false},
		{"datasetItems_list serves a version that is no window", dual, "datasetItems_list", false, true, false},
		{"datasetItems_list on 3.80.0 does not serve version at all", catalog.Profile{Version: "3.80.0", Families: legacy.Families}, "datasetItems_list", false, true, false},
		{"unstable_skills_list has a window by updated time", dual, "unstable_skills_list", false, true, true},
		{"prompts_list has a window by updated time", dual, "prompts_list", false, true, true},
		{"metrics_daily has a window by timestamp", catalog.Profile{Version: "3.61.0", Families: legacy.Families}, "metrics_daily", false, true, true},
		{"experiments_list has a window by start time", dual, "experiments_list", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op, ok := cat.Resolve(tc.profile).Lookup(tc.operationID)
			if !ok {
				t.Fatalf("%s missing on %s", tc.operationID, tc.profile.Version)
			}
			if op.HasQueryParam("fields") != tc.fields || op.HasQueryParam("limit") != tc.limit || op.HasTimeWindow() != tc.timeWindow {
				t.Fatalf("fields %v, limit %v, time window %v; want %v, %v, %v",
					op.HasQueryParam("fields"), op.HasQueryParam("limit"), op.HasTimeWindow(), tc.fields, tc.limit, tc.timeWindow)
			}
		})
	}
	op, _ := cat.Lookup("trace_get")
	if op.HasQueryParam("traceId") {
		t.Errorf("trace_get reports its path parameter traceId as a query parameter")
	}
}

// Seam S3 (spec #172, ticket #203): the two metrics operations narrow an answer
// through the JSON query they take, not through a limit or a time parameter of
// their own, and no other operation does.
func TestOnlyTheMetricsOperationsReportAMetricsQuery(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	legacy := []catalog.Family{catalog.LegacyFamily}
	for _, tc := range []struct {
		name        string
		profile     catalog.Profile
		operationID string
		want        bool
	}{
		{"metrics_metrics", catalog.Profile{Version: "4.46.0", Families: catalog.AllFamilies()}, "metrics_metrics", true},
		{"legacy_metricsV1_metrics", catalog.Profile{Version: "3.80.0", Families: legacy}, "legacy_metricsV1_metrics", true},
		{"datasets_list has a limit instead", catalog.Profile{Version: "3.80.0", Families: legacy}, "datasets_list", false},
		{"trace_list", catalog.Profile{Version: "3.80.0", Families: legacy}, "trace_list", false},
		{"trace_get", catalog.Profile{Version: "3.80.0", Families: legacy}, "trace_get", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op, ok := cat.Resolve(tc.profile).Lookup(tc.operationID)
			if !ok {
				t.Fatalf("%s missing", tc.operationID)
			}
			if got := op.HasMetricsQuery(); got != tc.want {
				t.Fatalf("HasMetricsQuery = %v, want %v", got, tc.want)
			}
		})
	}
}
