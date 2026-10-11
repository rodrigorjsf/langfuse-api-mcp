package catalog_test

import (
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam S3 (spec #172, ticket #177): the server words its truncation hint from
// the query parameters an operation serves on the connected deployment.
func TestAnOperationReportsTheQueryParametersAndTimeFiltersItServes(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	legacy := []catalog.Family{catalog.LegacyFamily}
	for _, tc := range []struct {
		name        string
		version     string
		operationID string
		fields      bool
		limit       bool
		timeFilter  bool
	}{
		{"trace_get does not serve fields on 3.80.0", "3.80.0", "trace_get", false, false, false},
		{"trace_get serves fields on 3.225.11", "3.225.11", "trace_get", true, false, false},
		{"trace_list has fields, a limit and a time filter", "3.225.11", "trace_list", true, true, true},
		{"datasets_list has a limit and no fields or time filter", "3.225.11", "datasets_list", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op, ok := cat.Resolve(catalog.Profile{Version: tc.version, Families: legacy}).Lookup(tc.operationID)
			if !ok {
				t.Fatalf("%s missing on %s", tc.operationID, tc.version)
			}
			if op.HasQueryParam("fields") != tc.fields || op.HasQueryParam("limit") != tc.limit || op.HasTimeFilter() != tc.timeFilter {
				t.Fatalf("fields %v, limit %v, time filter %v; want %v, %v, %v",
					op.HasQueryParam("fields"), op.HasQueryParam("limit"), op.HasTimeFilter(), tc.fields, tc.limit, tc.timeFilter)
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
