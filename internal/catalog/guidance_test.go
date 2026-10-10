package catalog_test

import (
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Only the query of the two metrics operations (#104, #178), the tag and
// filter parameters of prompts_list (#141), the label and version parameters
// of prompts_get and the fields parameter of trace_list (#178) carry static
// guidance; every other parameter of every catalog operation has none.
func TestOnlyTheGuidedParametersCarryGuidance(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	got := guidedParameters(cat)

	if !slices.Equal(got, guidedParams) {
		t.Fatalf("parameters with guidance = %v, want %v", got, guidedParams)
	}
}

// guidedParams are the parameters that carry guidance, as "operation/parameter",
// sorted.
var guidedParams = []string{
	"legacy_metricsV1_metrics/query", "metrics_metrics/query", "prompts_get/label", "prompts_get/version",
	"prompts_list/filter", "prompts_list/tag", "trace_list/fields",
}

// Whatever the deployment's version, no parameter outside the guided ones
// gains guidance.
func TestNoDeploymentGivesGuidanceToAParameterOutsideTheGuidedOnes(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	for _, version := range []string{"3.0.0", "3.57.0", "3.72.0", "3.80.0", "3.225.11", "4.46.0", ""} {
		got := guidedParameters(cat.Resolve(catalog.Profile{Version: version, Families: catalog.AllFamilies()}))

		for _, g := range got {
			if !slices.Contains(guidedParams, g) {
				t.Errorf("on %q %s carries guidance, want only %v", version, g, guidedParams)
			}
		}
	}
}

// trace_list serves fields from 3.73.0 (the union catalog's x-introduced), so
// only from then does the operation carry the guidance on it.
func TestTraceListCarriesTheFieldsGuidanceOnlyOnADeploymentThatServesFields(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	families := []catalog.Family{catalog.LegacyFamily}

	for version, want := range map[string]bool{"3.72.0": false, "3.73.0": true, "3.80.0": true, "": true} {
		got := slices.Contains(guidedParameters(cat.Resolve(catalog.Profile{Version: version, Families: families})), "trace_list/fields")
		if got != want {
			t.Errorf("trace_list/fields has guidance on %q: %v, want %v", version, got, want)
		}
	}
}

// Guidance is static text: one line of printable characters, with no control,
// invisible or bidi character that a spec or a payload could have put there.
func TestEveryGuidanceIsOneLineOfVisibleText(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	for _, op := range cat.Operations() {
		for _, p := range op.Params {
			g := op.ParamGuidance(p)
			if strings.TrimSpace(g) != g {
				t.Errorf("%s/%s guidance has leading or trailing space", op.ID, p.Name)
			}
			for _, r := range g {
				if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
					t.Errorf("%s/%s guidance holds the character %U", op.ID, p.Name, r)
				}
			}
		}
	}
}

// guidedParameters lists "operation/parameter" for every served parameter of
// the catalog that carries guidance, sorted.
func guidedParameters(cat catalog.Catalog) []string {
	var got []string
	for _, op := range cat.Operations() {
		for _, p := range op.Params {
			if op.ParamGuidance(p) != "" {
				got = append(got, op.ID+"/"+p.Name)
			}
		}
	}
	slices.Sort(got)
	return got
}
