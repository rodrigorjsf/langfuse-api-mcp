package catalog

import (
	"slices"
	"strings"
	"testing"
)

// Seam S3 (spec #172, ticket #204): which date-time query parameters bound a time
// window is a closed list of the catalog, and so is which ones do not; a new
// date-time parameter must be classified by a person before it ships, never by
// its name or its format alone.

// dateTimeQueryParams returns every query parameter of c, served or not, whose
// schema has the format date-time, as its operation and name.
func dateTimeQueryParams(c Catalog) []timeParam {
	var found []timeParam
	for _, op := range c.Operations() {
		for _, p := range slices.Concat(op.Params, op.unserved) {
			if p.In == "query" && p.Schema.Format == "date-time" {
				found = append(found, timeParam{op.ID, p.Name})
			}
		}
	}
	return found
}

// unclassifiedTimeParams names, as "operationId.parameter", every date-time query
// parameter of c that is on neither list, or on both.
func unclassifiedTimeParams(c Catalog) []string {
	var found []string
	for _, p := range dateTimeQueryParams(c) {
		if timeWindowParams[p.param] == notTimeWindows[p] {
			found = append(found, p.operationID+"."+p.param)
		}
	}
	return found
}

// Every date-time query parameter of the union catalog is a time window or is
// listed as none.
func TestEveryDateTimeQueryParameterOfTheUnionCatalogIsClassified(t *testing.T) {
	t.Parallel()
	cat, err := Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}

	if got := unclassifiedTimeParams(cat); len(got) != 0 {
		t.Errorf("date-time query parameters on neither list (or on both): %s", strings.Join(got, ", "))
	}
	if len(dateTimeQueryParams(cat)) == 0 {
		t.Error("the walk found no date-time query parameter: it proves nothing")
	}
}

// A date-time query parameter on neither list fails the walk, naming the
// operation and the parameter; one on the window list, one on the other list and
// one that is not date-time pass. The catalog is a literal, so the embedded one is
// not edited to prove it.
func TestAnUnclassifiedDateTimeParameterFailsTheWalkNamingTheOperationAndTheParameter(t *testing.T) {
	t.Parallel()
	cat, err := load([]byte(`{"x-oldest-version":"3.0.0","x-newest-version":"4.10.0","paths":{
 "/api/public/things":{"get":{"operationId":"things_list","tags":["T"],"description":"d","x-introduced":"3.0.0","parameters":[
  {"name":"asOf","in":"query","required":false,"x-introduced":"3.0.0","schema":{"type":"string","format":"date-time"}},
  {"name":"fromTimestamp","in":"query","required":false,"x-introduced":"3.0.0","schema":{"type":"string","format":"date-time"}},
  {"name":"label","in":"query","required":false,"x-introduced":"3.0.0","schema":{"type":"string"}}]}},
 "/api/public/observations/{observationId}":{"get":{"operationId":"legacy_observationsV1_get","tags":["T"],"description":"d","x-introduced":"3.0.0","parameters":[
  {"name":"startTime","in":"query","required":false,"x-introduced":"3.0.0","schema":{"type":"string","format":"date-time"}}]}},
 "/api/public/traces":{"get":{"operationId":"trace_list","tags":["T"],"description":"d","x-introduced":"3.0.0","parameters":[
  {"name":"startTime","in":"query","required":false,"x-introduced":"3.0.0","schema":{"type":"string","format":"date-time"}},
  {"name":"toTimestamp","in":"query","required":false,"x-introduced":"3.0.0","x-removed":"4.0.0","schema":{"type":"string","format":"date-time"}}]}}}}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := unclassifiedTimeParams(cat)
	slices.Sort(got)

	// things_list.asOf is on neither list, and so is trace_list.startTime: only the
	// startTime of legacy_observationsV1_get is listed as no window.
	if want := []string{"things_list.asOf", "trace_list.startTime"}; !slices.Equal(got, want) {
		t.Errorf("unclassified date-time parameters = %v, want %v", got, want)
	}
}

// A parameter that only looks like a window is none unless it is on the list.
func TestAParameterThatOnlyLooksLikeATimeWindowIsNoTimeWindow(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"fromVersion", "to", "from", "startTime", "since", "fromtimestamp", "FromTimestamp"} {
		op := Operation{ID: "things_list", Params: []Param{{Name: name, In: "query", Schema: Schema{Type: "string", Format: "date-time"}}}}
		if op.HasTimeWindow() {
			t.Errorf("a date-time query parameter %q counts as a time window", name)
		}
	}
	listed := Operation{ID: "things_list", Params: []Param{{Name: "toUpdatedAt", In: "query"}}}
	if !listed.HasTimeWindow() {
		t.Error("toUpdatedAt, on the list, is not a time window")
	}
	inHeader := Operation{ID: "things_list", Params: []Param{{Name: "fromTimestamp", In: "header"}}}
	if inHeader.HasTimeWindow() {
		t.Error("a header named fromTimestamp counts as a time window")
	}
}

// Every entry of the two lists is a date-time query parameter that the union
// catalog has, so a renamed or removed parameter cannot leave a stale entry
// behind, and a pair that is a window by name is not also listed as none.
func TestEveryListedTimeParameterIsADateTimeQueryParameterOfTheUnionCatalog(t *testing.T) {
	t.Parallel()
	cat, err := Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	found := dateTimeQueryParams(cat)
	for name := range timeWindowParams {
		if !slices.ContainsFunc(found, func(p timeParam) bool { return p.param == name }) {
			t.Errorf("%s is on the list of time windows and no operation has it as a date-time query parameter", name)
		}
	}
	for pair := range notTimeWindows {
		if !slices.Contains(found, pair) {
			t.Errorf("%s.%s is on the list of date-time parameters that bound no window and is not one of the catalog", pair.operationID, pair.param)
		}
		if timeWindowParams[pair.param] {
			t.Errorf("%s.%s is on both lists", pair.operationID, pair.param)
		}
	}
}

// The twelve list operations that take a window, by the date-time parameters the
// release specs give them: nothing else in the union catalog has a time window.
func TestTheTwelveOperationsWithATimeWindowAreTheOnesOfTheUnionCatalog(t *testing.T) {
	t.Parallel()
	cat, err := Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	var got []string
	for _, op := range cat.Operations() {
		window := slices.ContainsFunc(slices.Concat(op.Params, op.unserved), func(p Param) bool {
			return p.In == "query" && timeWindowParams[p.Name]
		})
		if window {
			got = append(got, op.ID)
		}
	}
	slices.Sort(got)
	want := []string{
		"experiments_list", "experiments_listItems", "legacy_observationsV1_getMany", "metrics_daily", "observations_getMany",
		"prompts_list", "score_get", "scoresV3_getManyV3", "scores_get-many", "sessions_list", "trace_list", "unstable_skills_list",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("operations with a time window = %v, want %v", got, want)
	}
}
