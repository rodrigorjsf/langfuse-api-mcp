package catalog

import (
	"slices"
	"strings"
	"testing"
)

// Seam S3 (spec #172, ticket #195): which filter parameters are JSON texts is a
// closed list of the catalog, never decided from a parameter's name, so a new
// filter must be classified before it ships.

// unclassifiedFilters names every operation of c with a query parameter called
// filter, served or not, that is on neither the list of JSON filters nor the
// list of plain ones, or on both.
func unclassifiedFilters(c Catalog) []string {
	var found []string
	for _, op := range c.Operations() {
		for _, p := range slices.Concat(op.Params, op.unserved) {
			key := operationParam{op.ID, p.Name}
			if p.In == "query" && p.Name == "filter" && jsonFilters[key] == plainFilters[key] {
				found = append(found, op.ID)
			}
		}
	}
	return found
}

// Every filter parameter of the union catalog is a JSON filter or a plain one.
func TestEveryFilterParameterOfTheUnionCatalogIsClassified(t *testing.T) {
	t.Parallel()
	cat, err := Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}

	if got := unclassifiedFilters(cat); len(got) != 0 {
		t.Errorf("operations with a filter on neither list (or on both): %s", strings.Join(got, ", "))
	}
}

// A filter of an operation that is on neither list fails the walk, naming the
// operation: the new one is classified by a person, not by its name.
func TestAnUnclassifiedFilterFailsTheWalkNamingItsOperation(t *testing.T) {
	t.Parallel()
	cat, err := load([]byte(`{"x-oldest-version":"3.0.0","x-newest-version":"4.10.0","paths":{
 "/api/public/things":{"get":{"operationId":"things_list","tags":["T"],"description":"d","x-introduced":"3.0.0","parameters":[
  {"name":"filter","in":"query","required":false,"x-introduced":"3.0.0","schema":{"type":"string"}}]}},
 "/api/public/traces":{"get":{"operationId":"trace_list","tags":["T"],"description":"d","x-introduced":"3.0.0","parameters":[
  {"name":"filter","in":"query","required":false,"x-introduced":"3.0.0","schema":{"type":"string"}}]}}}}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := unclassifiedFilters(cat)

	if want := []string{"things_list"}; !slices.Equal(got, want) {
		t.Errorf("unclassified filters = %v, want %v (trace_list is on the list of JSON filters)", got, want)
	}
}

// Every pair on a list names a filter parameter that the union catalog has, so
// a renamed or removed operation cannot leave a stale entry behind.
func TestEveryClassifiedFilterIsAFilterParameterOfTheUnionCatalog(t *testing.T) {
	t.Parallel()
	cat, err := Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	for _, list := range []map[operationParam]bool{jsonFilters, plainFilters} {
		for pair := range list {
			op, ok := cat.Lookup(pair.operationID)
			if !ok {
				t.Errorf("%s is on a list of filters and is not in the catalog", pair.operationID)
				continue
			}
			if !slices.ContainsFunc(slices.Concat(op.Params, op.unserved), func(p Param) bool {
				return p.In == "query" && p.Name == pair.param
			}) {
				t.Errorf("%s has no query parameter %s", pair.operationID, pair.param)
			}
		}
	}
}
