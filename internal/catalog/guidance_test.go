package catalog_test

import (
	"slices"
	"testing"
)

// Only the metrics_metrics query (#104) and the prompts_list tag and filter
// parameters (#141) carry static guidance; every other parameter of every
// catalog operation has none.
func TestOnlyTheMetricsQueryAndThePromptsListTagAndFilterCarryGuidance(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	var got []string
	for _, op := range cat.Operations() {
		for _, p := range op.Params {
			if op.ParamGuidance(p) != "" {
				got = append(got, op.ID+"/"+p.Name)
			}
		}
	}

	slices.Sort(got)
	want := []string{"metrics_metrics/query", "prompts_list/filter", "prompts_list/tag"}
	if !slices.Equal(got, want) {
		t.Fatalf("parameters with guidance = %v, want %v", got, want)
	}
}
