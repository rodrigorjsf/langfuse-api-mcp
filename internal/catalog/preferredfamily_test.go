package catalog_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam S1 (spec #172, ticket #212): which operations of a resolved catalog
// carry the fact "my preferred operation reads another operation family and
// can return less than I do". It is decided from the catalog's own data (the
// operation's family, its preferred operation and that operation's family) and
// the deployment profile, so the catalog stays pure and the server asks one
// question. Expected values are literals.

// tenPreferredAcross are the ten legacy read operations of the union catalog
// whose preferred operation belongs to another operation family, with that
// operation (the table of the triage of #212). The other four lines that name
// a preferred operation (scores_get-many, scores_get-by-id, the dataset run
// item create and the ingestion batch) name an operation with no family.
var tenPreferredAcross = map[string]string{
	"trace_get":                     "observations_getMany",
	"trace_list":                    "observations_getMany",
	"sessions_get":                  "observations_getMany",
	"sessions_list":                 "observations_getMany",
	"legacy_observationsV1_get":     "observations_getMany",
	"legacy_observationsV1_getMany": "observations_getMany",
	"legacy_metricsV1_metrics":      "metrics_metrics",
	"datasets_getRuns":              "experiments_list",
	"datasets_getRun":               "experiments_list",
	"datasetRunItems_list":          "experiments_listItems",
}

// carriers returns, by operation ID, the preferred operation of every
// operation of cat that carries the fact.
func carriers(cat catalog.Catalog) map[string]string {
	out := map[string]string{}
	for _, op := range cat.Operations() {
		if p := op.PreferredOfOtherFamily(); p != "" {
			out[op.ID] = p
		}
	}
	return out
}

func TestOnTheCatalogResolvedForAllFamiliesExactlyTenLegacyReadsPreferAnOperationOfAnotherFamily(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t).Resolve(catalog.Profile{Version: "4.46.0", Families: catalog.AllFamilies()})

	got := carriers(cat)

	for id, want := range tenPreferredAcross {
		if got[id] != want {
			t.Errorf("%s: preferred operation of another family = %q, want %q", id, got[id], want)
		}
	}
	for id, p := range got {
		if _, ok := tenPreferredAcross[id]; !ok {
			t.Errorf("%s carries the fact (prefers %s), want only the ten operations of the triage", id, p)
		}
	}
}

// The catalog that Load returns is resolved for an unknown version with every
// family on, as startup does when detection decides nothing (ADR-0012 §3).
func TestOnTheUnresolvedCatalogTheSameTenOperationsCarryTheFact(t *testing.T) {
	t.Parallel()

	got := carriers(mustLoad(t))

	if !slices.Equal(sortedKeys(got), sortedKeys(tenPreferredAcross)) {
		t.Errorf("carriers = %v, want %v", sortedKeys(got), sortedKeys(tenPreferredAcross))
	}
}

func TestWithTheExperimentsFamilyOffOnlyTheSevenReadsPreferringAV4ReadOperationCarryTheFact(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t).Resolve(catalog.Profile{
		Version: "4.46.0", Families: []catalog.Family{catalog.LegacyFamily, catalog.V4ReadFamily},
	})

	got := carriers(cat)

	want := map[string]string{}
	for id, p := range tenPreferredAcross {
		if p == "observations_getMany" || p == "metrics_metrics" {
			want[id] = p
		}
	}
	if len(want) != 7 {
		t.Fatalf("the table holds %d operations preferring a v4 read operation, want 7", len(want))
	}
	if !slices.Equal(sortedKeys(got), sortedKeys(want)) {
		t.Fatalf("carriers = %v, want %v", sortedKeys(got), sortedKeys(want))
	}
	for id, p := range want {
		if got[id] != p {
			t.Errorf("%s: preferred operation = %q, want %q", id, got[id], p)
		}
	}
	// The dataset run operations lose the fact because their preferred operation
	// is not offered; their line drops the mention as before (#174).
	for _, id := range []string{"datasets_getRuns", "datasets_getRun", "datasetRunItems_list"} {
		op, ok := cat.Lookup(id)
		if !ok {
			t.Fatalf("%s is not in the catalog", id)
		}
		if op.PreferredOfOtherFamily() != "" {
			t.Errorf("%s carries the fact although its preferred operation is not offered", id)
		}
	}
}

func TestWithTheLegacyAndTheExperimentsFamiliesOnOnlyTheThreeDatasetRunReadsCarryTheFact(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t).Resolve(catalog.Profile{
		Version: "4.46.0", Families: []catalog.Family{catalog.LegacyFamily, catalog.ExperimentsFamily},
	})

	got := carriers(cat)

	want := map[string]string{
		"datasets_getRuns": "experiments_list", "datasets_getRun": "experiments_list", "datasetRunItems_list": "experiments_listItems",
	}
	if !slices.Equal(sortedKeys(got), sortedKeys(want)) {
		t.Fatalf("carriers = %v, want %v", sortedKeys(got), sortedKeys(want))
	}
	for id, p := range want {
		if got[id] != p {
			t.Errorf("%s: preferred operation = %q, want %q", id, got[id], p)
		}
	}
}

func TestOnADeploymentWithoutBothFamiliesNoOperationCarriesTheFact(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]catalog.Profile{
		"3.80.0":                      {Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}},
		"3.225.11":                    {Version: "3.225.11", Families: []catalog.Family{catalog.LegacyFamily}},
		"4.46.0 events_only":          {Version: "4.46.0", Families: []catalog.Family{catalog.V4ReadFamily, catalog.ExperimentsFamily}},
		"4.46.0 v4 read only":         {Version: "4.46.0", Families: []catalog.Family{catalog.V4ReadFamily}},
		"4.46.0 experiments only":     {Version: "4.46.0", Families: []catalog.Family{catalog.ExperimentsFamily}},
		"no version, no families":     {},
		"4.46.0, no families":         {Version: "4.46.0"},
		"below the supported floor":   {Version: "2.0.0", Families: catalog.AllFamilies()},
		"version text that is no one": {Version: "4.46.0-rc1", Families: []catalog.Family{catalog.LegacyFamily}},
	} {
		if got := carriers(mustLoad(t).Resolve(p)); len(got) != 0 {
			t.Errorf("%s: %v carry the fact, want none", name, sortedKeys(got))
		}
	}
}

// A walk of the union catalog over the profiles the server is pinned to and
// the ones that differ by one family: whenever an operation carries the fact,
// each condition holds, checked here from the catalog's other data and not
// from the code that decides it. A failure names the operation.
func TestEveryOperationThatCarriesTheFactMeetsEveryConditionOfIt(t *testing.T) {
	t.Parallel()
	profiles := map[string]catalog.Profile{
		"3.80.0":             {Version: "3.80.0", Families: []catalog.Family{catalog.LegacyFamily}},
		"4.46.0 events_only": {Version: "4.46.0", Families: []catalog.Family{catalog.V4ReadFamily, catalog.ExperimentsFamily}},
		"4.46.0 dual":        {Version: "4.46.0", Families: catalog.AllFamilies()},
		"legacy, v4 read":    {Version: "4.46.0", Families: []catalog.Family{catalog.LegacyFamily, catalog.V4ReadFamily}},
		"legacy, experiments": {
			Version: "4.46.0", Families: []catalog.Family{catalog.LegacyFamily, catalog.ExperimentsFamily},
		},
		"unknown version, every family": {Families: catalog.AllFamilies()},
		"no families":                   {},
	}
	for name, p := range profiles {
		cat := mustLoad(t).Resolve(p)
		for _, op := range cat.Operations() {
			preferred := op.PreferredOfOtherFamily()
			if preferred == "" {
				continue
			}
			other, offered := cat.Lookup(preferred)
			switch {
			case !op.IsRead():
				t.Errorf("%s: %s is not a read operation but carries the fact", name, op.ID)
			case op.Family != catalog.LegacyFamily:
				t.Errorf("%s: %s is of family %q, want the legacy family", name, op.ID, op.Family)
			case !offered:
				t.Errorf("%s: %s carries the fact but its preferred operation %s is not offered", name, op.ID, preferred)
			case !other.IsRead():
				t.Errorf("%s: %s: preferred operation %s is not a read operation", name, op.ID, preferred)
			case other.Family == "" || other.Family == catalog.LegacyFamily:
				t.Errorf("%s: %s: preferred operation %s is of family %q, want another family", name, op.ID, preferred, other.Family)
			case !slices.Contains(p.Families, catalog.LegacyFamily) || !slices.Contains(p.Families, other.Family):
				t.Errorf("%s: %s carries the fact but the family of %s (%q) or the legacy family is off in the profile",
					name, op.ID, preferred, other.Family)
			case !strings.Contains(op.DescriptionLine, "(legacy: prefer "+preferred+" when it is available)"):
				t.Errorf("%s: %s: the line %q does not name %s as the operation to prefer", name, op.ID, op.DescriptionLine, preferred)
			}
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
