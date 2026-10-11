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
// of prompts_get and the fields parameter of the seven operations that take it
// (#178, #194) carry static guidance; every other parameter of every catalog
// operation has none.
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
	"experiments_list/fields", "experiments_listItems/fields", "legacy_metricsV1_metrics/query", "metrics_metrics/query",
	"observations_getMany/fields", "prompts_get/label", "prompts_get/version", "prompts_list/filter", "prompts_list/tag",
	"scoresV3_getManyV3/fields", "scores_get-many/fields", "trace_get/fields", "trace_list/fields",
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

// Every operation of the union catalog that takes fields has guidance on it,
// and names its groups in the hints: a release that gives another operation a
// fields parameter fails here, naming the operation, until its values are
// recorded (docs/research/langfuse.md, section 1.5) and compiled (#194).
func TestEveryOperationWithAFieldsParameterHasGuidanceAndFieldGroups(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	seen := 0
	for _, op := range cat.Operations() {
		if !op.HasQueryParam("fields") {
			continue
		}
		seen++
		p, _ := paramNamed(op, "fields")
		if op.ParamGuidance(p) == "" {
			t.Errorf("%s takes fields and has no guidance on it", op.ID)
		}
		if op.FieldGroups() == "" {
			t.Errorf("%s takes fields and names no field group for the hints", op.ID)
		}
	}
	if seen < 7 {
		t.Fatalf("the walk found %d operations with fields, want at least the 7 of the union catalog", seen)
	}
}

// An operation carries the guidance on fields, and names its groups, only on a
// deployment whose version serves the parameter (the union catalog's
// x-introduced): on an older one it has neither the parameter nor the text.
func TestAnOperationCarriesTheFieldsGuidanceOnlyOnADeploymentThatServesFields(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	for _, tc := range []struct{ operationID, before, from string }{
		{"trace_get", "3.165.0", "3.166.0"},
		{"scores_get-many", "3.143.0", "3.144.0"},
		{"observations_getMany", "3.140.0", "3.141.0"},
		{"scoresV3_getManyV3", "3.178.0", "3.179.0"},
		{"experiments_list", "3.205.0", "3.206.0"},
		{"experiments_listItems", "3.205.0", "3.206.0"},
	} {
		for version, want := range map[string]bool{tc.before: false, tc.from: true, "4.50.0": true, "": true} {
			resolved := cat.Resolve(catalog.Profile{Version: version, Families: catalog.AllFamilies()})
			got := slices.Contains(guidedParameters(resolved), tc.operationID+"/fields")
			groups := ""
			if op, ok := resolved.Lookup(tc.operationID); ok {
				groups = op.FieldGroups()
			}
			if got != want || (groups != "") != want {
				t.Errorf("%s on %q: guidance %v, field groups %q, want guidance %v", tc.operationID, version, got, groups, want)
			}
		}
	}
}

// The guidance and the hints name the same groups, in the same order: both are
// built from one compiled source (#194). The groups are the literals of the
// release specs, docs/research/langfuse.md section 1.5.
func TestTheGuidanceAndTheHintsNameTheSameFieldGroups(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	traceGroups := []string{"core", "io", "scores", "observations", "metrics"}
	tests := map[string]struct {
		groups []string
		hint   string
	}{
		"trace_list": {traceGroups, "core, io, scores, observations and metrics"},
		"trace_get":  {traceGroups, "core, io, scores, observations and metrics"},
		"observations_getMany": {
			[]string{"core", "basic", "time", "io", "metadata", "model", "usage", "prompt", "metrics", "trace_context"},
			"core, basic, time, io, metadata, model, usage, prompt, metrics and trace_context from Langfuse 3.175.0"},
		"scores_get-many":    {[]string{"score", "trace"}, "score and trace"},
		"scoresV3_getManyV3": {[]string{"details", "subject", "annotation"}, "details, subject and annotation"},
		"experiments_list":   {[]string{"core", "metadata", "scores"}, "core, metadata and scores"},
		"experiments_listItems": {
			[]string{"core", "dataset", "io", "metadata", "itemMetadata", "experimentMetadata", "scores"},
			"core, dataset, io, metadata, itemMetadata, experimentMetadata and scores"},
	}
	for operationID, tc := range tests {
		op, ok := cat.Lookup(operationID)
		if !ok {
			t.Fatalf("%s is not in the catalog", operationID)
		}
		p, _ := paramNamed(op, "fields")
		guidance := op.ParamGuidance(p)

		if got := op.FieldGroups(); got != tc.hint {
			t.Errorf("%s: field groups for the hints = %q, want %q", operationID, got, tc.hint)
		}
		for name, text := range map[string]string{"guidance": guidance, "hint": op.FieldGroups()} {
			at := 0
			for _, group := range tc.groups {
				i := strings.Index(text[at:], group)
				if i < 0 {
					t.Errorf("%s: the %s does not name the group %s after offset %d: %q", operationID, name, group, at, text)
					break
				}
				at += i + len(group)
			}
		}
	}
}

// An operation that does not take fields names no group, and no field of the
// hints is left to a literal operation that has none.
func TestAnOperationWithoutFieldsNamesNoFieldGroup(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	older := cat.Resolve(catalog.Profile{Version: "3.165.0", Families: catalog.AllFamilies()})

	for _, tc := range []struct {
		cat         catalog.Catalog
		operationID string
	}{{cat, "datasets_list"}, {cat, "prompts_get"}, {older, "trace_get"}} {
		op, ok := tc.cat.Lookup(tc.operationID)
		if !ok {
			t.Fatalf("%s is not in the catalog", tc.operationID)
		}
		if got := op.FieldGroups(); got != "" {
			t.Errorf("%s names the field groups %q, want none", tc.operationID, got)
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

// paramNamed returns the operation's query parameter called name.
func paramNamed(op catalog.Operation, name string) (catalog.Param, bool) {
	for _, p := range op.Params {
		if p.In == "query" && p.Name == name {
			return p, true
		}
	}
	return catalog.Param{}, false
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
