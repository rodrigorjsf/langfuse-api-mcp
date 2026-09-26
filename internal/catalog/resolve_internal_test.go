package catalog

import (
	"slices"
	"testing"
)

// unionSpec is a small union catalog: two releases known (3.0.0 to 4.10.0),
// an operation ID that moved to a new path in 3.5.0, one operation removed in
// 4.0.0 and one per operation family.
var unionSpec = []byte(`{"x-oldest-version":"3.0.0","x-newest-version":"4.10.0","paths":{
 "/api/public/old/{id}":{"patch":{"operationId":"thing_update","tags":["T"],"description":"d","x-introduced":"3.0.0","x-removed":"3.5.0"}},
 "/api/public/new/{id}":{"patch":{"operationId":"thing_update","tags":["T"],"description":"d","x-introduced":"3.5.0"}},
 "/api/public/gone":{"get":{"operationId":"gone_get","tags":["T"],"description":"d","x-introduced":"3.0.0","x-removed":"4.0.0"}},
 "/api/public/traces":{"get":{"operationId":"trace_list","tags":["T"],"description":"d","x-introduced":"3.0.0","x-family":"legacy"}},
 "/api/public/v2/observations":{"get":{"operationId":"observations_getMany","tags":["T"],"description":"d","x-introduced":"3.141.0","x-family":"v4 read"}},
 "/api/public/experiments":{"get":{"operationId":"experiments_list","tags":["T"],"description":"d","x-introduced":"3.206.0","x-family":"experiments"}},
 "/api/public/health":{"get":{"operationId":"health_health","tags":["T"],"description":"d","x-introduced":"3.0.0"}}}}`)

func mustLoadUnion(t *testing.T) Catalog {
	t.Helper()
	cat, err := load(unionSpec)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cat
}

func opIDs(c Catalog) []string {
	out := []string{}
	for _, op := range c.Operations() {
		out = append(out, op.ID)
	}
	return out
}

// With the version unknown, nothing is hidden by range: an ID shared by two
// operations names the newer one.
func TestAnUnknownVersionKeepsEveryRangeAndAnIDNamesItsNewestOperation(t *testing.T) {
	t.Parallel()
	cat := mustLoadUnion(t)

	for _, version := range []string{"", "latest", "4.10", "v4.10.0", "4.10.0-rc.1"} {
		got := cat.Resolve(Profile{Version: version, Families: AllFamilies()})
		want := []string{"experiments_list", "gone_get", "health_health", "observations_getMany", "thing_update", "trace_list"}
		if !slices.Equal(opIDs(got), want) {
			t.Errorf("Resolve(%q) = %v, want %v", version, opIDs(got), want)
		}
		if op, _ := got.Lookup("thing_update"); op.Path != "/api/public/new/{id}" {
			t.Errorf("Resolve(%q): thing_update = %s, want the operation introduced last", version, op.Path)
		}
	}
	if got := opIDs(cat); !slices.Equal(got, opIDs(cat.Resolve(Profile{Families: AllFamilies()}))) {
		t.Errorf("Load() = %v, want the unknown-version resolution with every family on", got)
	}
}

func TestResolutionFollowsTheVersionRangesAndTheFamiliesOn(t *testing.T) {
	t.Parallel()
	cat := mustLoadUnion(t)

	for _, tc := range []struct {
		name    string
		profile Profile
		want    []string
		path    string // thing_update's path
	}{
		{"old range", Profile{Version: "3.4.9", Families: AllFamilies()},
			[]string{"gone_get", "health_health", "thing_update", "trace_list"}, "/api/public/old/{id}"},
		{"removal is exclusive", Profile{Version: "4.0.0", Families: AllFamilies()},
			[]string{"experiments_list", "health_health", "observations_getMany", "thing_update", "trace_list"}, "/api/public/new/{id}"},
		{"families off", Profile{Version: "4.10.0", Families: []Family{V4ReadFamily}},
			[]string{"health_health", "observations_getMany", "thing_update"}, "/api/public/new/{id}"},
		{"newer than known is the newest known", Profile{Version: "9.0.0", Families: []Family{LegacyFamily}},
			[]string{"health_health", "thing_update", "trace_list"}, "/api/public/new/{id}"},
		{"below the floor: ranges alone at the floor", Profile{Version: "2.95.3"},
			[]string{"gone_get", "health_health", "thing_update", "trace_list"}, "/api/public/old/{id}"},
		{"unknown version, families still apply", Profile{Families: []Family{ExperimentsFamily}},
			[]string{"experiments_list", "gone_get", "health_health", "thing_update"}, "/api/public/new/{id}"},
	} {
		got := cat.Resolve(tc.profile)
		if !slices.Equal(opIDs(got), tc.want) {
			t.Errorf("%s: Resolve(%+v) = %v, want %v", tc.name, tc.profile, opIDs(got), tc.want)
		}
		if op, _ := got.Lookup("thing_update"); op.Path != tc.path {
			t.Errorf("%s: thing_update = %s, want %s", tc.name, op.Path, tc.path)
		}
	}
}

// Resolving twice narrows the union, never the previous resolution.
func TestResolvingAResolvedCatalogStartsFromTheUnion(t *testing.T) {
	t.Parallel()
	cat := mustLoadUnion(t)

	narrow := cat.Resolve(Profile{Version: "3.1.0", Families: []Family{LegacyFamily}})
	got := narrow.Resolve(Profile{Version: "4.10.0", Families: []Family{V4ReadFamily}})

	if want := []string{"health_health", "observations_getMany", "thing_update"}; !slices.Equal(opIDs(got), want) {
		t.Fatalf("Resolve after Resolve = %v, want %v", opIDs(got), want)
	}
}
