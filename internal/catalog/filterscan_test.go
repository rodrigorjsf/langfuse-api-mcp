package catalog_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam S1 of the catalog (spec #172, ticket #183): the closed list of filters
// the server applies itself on a deployment that does not serve them, and what
// the catalog hands the executor for such a call.

// The list holds exactly two pairs, each with the field of a row that the
// filter's value is compared with. A pair added without its row field, or a
// third pair, fails here: the list is closed (ADR-0012 amendment).
func TestTheFilterScanListHoldsExactlyTheTwoPairsEachWithItsRowField(t *testing.T) {
	t.Parallel()

	got := catalog.FilterScans()

	want := []catalog.FilterScan{
		{OperationID: "scores_get-many", Parameter: "traceId", RowField: "traceId"},
		{OperationID: "legacy_observationsV1_getMany", Parameter: "level", RowField: "level"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("filter scan pairs = %+v, want exactly %+v", got, want)
	}
	for _, f := range got {
		if f.RowField == "" {
			t.Errorf("%s/%s has no row field", f.OperationID, f.Parameter)
		}
	}
}

// A pair is only meaningful if its parameter is a string query parameter of an
// operation of the catalog, in some release: the scan compares the row field
// with that string.
func TestEveryFilterScanPairNamesAStringQueryParameterOfAnOperationOfTheCatalog(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	for _, f := range catalog.FilterScans() {
		op, ok := cat.Lookup(f.OperationID)
		if !ok || !op.IsRead() {
			t.Errorf("%s is not a read operation of the catalog", f.OperationID)
			continue
		}
		found := false
		for _, p := range op.Params {
			if p.Name == f.Parameter && p.In == "query" && p.Schema.Type == "string" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s has no string query parameter %s in the newest release", f.OperationID, f.Parameter)
		}
	}
}

// The scan applies only where the deployment does not serve the filter; where
// it does, Scan reports nothing and the call is one ordinary request.
func TestAFilterIsScannedOnlyOnADeploymentThatDoesNotServeIt(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	legacy := []catalog.Family{catalog.LegacyFamily}

	for _, tc := range []struct {
		version, operationID, param, value string
		want                               bool
	}{
		{"3.80.0", "scores_get-many", "traceId", "t-1", true},
		{"3.132.0", "scores_get-many", "traceId", "t-1", true},
		{"3.133.0", "scores_get-many", "traceId", "t-1", false},
		{"4.46.0", "scores_get-many", "traceId", "t-1", false},
		{"", "scores_get-many", "traceId", "t-1", false}, // undetected: the newest release's parameters
		{"3.80.0", "legacy_observationsV1_getMany", "level", "ERROR", true},
		{"3.81.9", "legacy_observationsV1_getMany", "level", "ERROR", true},
		{"3.82.0", "legacy_observationsV1_getMany", "level", "ERROR", false},
	} {
		op, ok := cat.Resolve(catalog.Profile{Version: tc.version, Families: legacy}).Lookup(tc.operationID)
		if !ok {
			t.Fatalf("%s is not offered at %q", tc.operationID, tc.version)
		}

		_, got, err := op.Scan(map[string]any{tc.param: tc.value})

		if err != nil || got != tc.want {
			t.Errorf("%s %s on %q: scanned = %v, err = %v, want scanned = %v", tc.operationID, tc.param, tc.version, got, err, tc.want)
		}
	}
}

// Any other parameter of the two operations, and any other operation, is not
// scanned: the refusal of a parameter the version does not serve (#173) stays.
func TestNoOtherParameterAndNoOtherOperationIsScanned(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t).Resolve(catalog.Profile{Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}})

	for _, tc := range []struct {
		operationID string
		params      map[string]any
	}{
		{"trace_get", map[string]any{"traceId": "t-1", "fields": "core"}},
		{"scores_get-many", map[string]any{"observationId": "o-1"}},
		{"legacy_observationsV1_getMany", map[string]any{"traceId": "t-1"}}, // served since 3.0.0: ordinary
		{"scores_get-many", map[string]any{"page": float64(1)}},
	} {
		op, ok := cat.Lookup(tc.operationID)
		if !ok {
			t.Fatalf("%s is not offered", tc.operationID)
		}

		_, scanned, err := op.Scan(tc.params)

		if scanned || err != nil {
			t.Errorf("%s %v: scanned = %v, err = %v, want neither", tc.operationID, tc.params, scanned, err)
		}
	}
}

// The scan's request is the call's other parameters, checked like any call's,
// with the filter left out and the page size at its maximum.
func TestAScanRequestKeepsTheOtherParametersAndReadsAtThePageSizeMaximum(t *testing.T) {
	t.Parallel()
	op := scanOperation(t, "scores_get-many")

	scan, scanned, err := op.Scan(map[string]any{"traceId": "t-1", "name": "quality", "limit": float64(5), "environment": []any{"prod", "dev"}})

	if err != nil || !scanned {
		t.Fatalf("Scan = scanned %v, err %v, want a scan", scanned, err)
	}
	if scan.Parameter != "traceId" || scan.RowField != "traceId" || scan.Value != "t-1" {
		t.Errorf("scan = %+v, want traceId compared with the row field traceId and the value t-1", scan)
	}
	if scan.Request.Path != "/api/public/v2/scores" || scan.Request.Method != "GET" {
		t.Errorf("request = %s %s, want GET /api/public/v2/scores", scan.Request.Method, scan.Request.Path)
	}
	// Encode sorts by name and keeps the order of a repeated value.
	if got, want := scan.Request.Query.Encode(), "environment=prod&environment=dev&limit=100&name=quality"; got != want {
		t.Errorf("query = %q, want the other parameters, no traceId, no page, limit 100: %q", got, want)
	}
}

// `page` together with a scanned filter is refused: the scan chooses the pages.
func TestAPageTogetherWithAScannedFilterIsRefused(t *testing.T) {
	t.Parallel()
	op := scanOperation(t, "legacy_observationsV1_getMany")

	_, _, err := op.Scan(map[string]any{"level": "ERROR", "page": float64(2)})

	if !errors.Is(err, catalog.ErrInvalidParameter) || !errors.Is(err, catalog.ErrPageWithFilterScan) {
		t.Fatalf("err = %v, want ErrPageWithFilterScan, an invalid parameter", err)
	}
}

// scanOperation returns the named operation as a 3.80.0 deployment offers it,
// which serves neither scores_get-many's traceId nor the observations' level.
func scanOperation(t *testing.T, operationID string) catalog.Operation {
	t.Helper()
	op, ok := mustLoad(t).Resolve(catalog.Profile{Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}}).Lookup(operationID)
	if !ok {
		t.Fatalf("%s is not offered on 3.80.0", operationID)
	}
	return op
}
