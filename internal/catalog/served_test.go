package catalog_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// The campaign finding H1 (#173): Langfuse 3.80.0 ignores traceId on
// scores_get-many, level on legacy_observationsV1_getMany and fields on
// trace_get, and answers the unfiltered list. The release that introduced each
// is the one the Langfuse release specs show (literals from the generated
// union catalog, scripts/gen-union-catalog.py).
func TestTheEmbeddedCatalogOffersTheThreeParametersOnlyWhereTheirReleaseServesThem(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	dual := []catalog.Family{catalog.LegacyFamily, catalog.V4ReadFamily, catalog.ExperimentsFamily}
	for _, tc := range []struct {
		operationID, param, servedFrom string
	}{
		{"scores_get-many", "traceId", "3.133.0"},
		{"legacy_observationsV1_getMany", "level", "3.82.0"},
		{"trace_get", "fields", "3.166.0"},
	} {
		old := cat.Resolve(catalog.Profile{Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}})
		op, ok := old.Lookup(tc.operationID)
		if !ok {
			t.Fatalf("%s: missing on 3.80.0", tc.operationID)
		}
		if slices.ContainsFunc(op.Params, func(p catalog.Param) bool { return p.Name == tc.param }) {
			t.Errorf("%s on 3.80.0 offers %s", tc.operationID, tc.param)
		}
		_, err := op.Request(map[string]any{tc.param: "ERROR"})
		if !errors.Is(err, catalog.ErrParameterNotServed) {
			t.Errorf("%s with %s on 3.80.0: error %v, want ErrParameterNotServed", tc.operationID, tc.param, err)
		} else if !strings.Contains(err.Error(), "3.80.0") || !strings.Contains(err.Error(), "from "+tc.servedFrom) ||
			!strings.Contains(err.Error(), tc.operationID) || strings.Contains(err.Error(), "ERROR") {
			t.Errorf("%s with %s on 3.80.0: error %q, want the operation, 3.80.0 and 'from %s', not the value",
				tc.operationID, tc.param, err, tc.servedFrom)
		}

		for _, version := range []string{tc.servedFrom, "4.46.0", ""} {
			op, _ := cat.Resolve(catalog.Profile{Version: version, Families: dual}).Lookup(tc.operationID)
			if !slices.ContainsFunc(op.Params, func(p catalog.Param) bool { return p.Name == tc.param }) {
				t.Errorf("%s on %q does not offer %s", tc.operationID, version, tc.param)
			}
			if _, err := op.Request(map[string]any{tc.param: "ERROR"}); err != nil && strings.Contains(err.Error(), "does not serve") {
				t.Errorf("%s with %s on %q: refused as not served: %v", tc.operationID, tc.param, version, err)
			}
		}
	}
}

// Releases 3.57.1 to 3.78.x list the response schema of datasetRunItems_list as
// a required query parameter: a spec error the generator drops, so that the
// operation never asks for it.
func TestADatasetRunItemsListNeverRequiresAResponseParameter(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	for _, version := range []string{"3.60.0", "3.79.0", "4.46.0", ""} {
		op, ok := cat.Resolve(catalog.Profile{Version: version, Families: catalog.AllFamilies()}).Lookup("datasetRunItems_list")
		if !ok {
			t.Fatalf("datasetRunItems_list missing on %q", version)
		}
		if slices.ContainsFunc(op.Params, func(p catalog.Param) bool { return p.Name == "response" }) {
			t.Errorf("datasetRunItems_list on %q offers response", version)
		}
	}
}
