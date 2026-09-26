package catalog_test

import (
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// param returns the named parameter of the named operation.
func param(t *testing.T, cat catalog.Catalog, operationID, name string) catalog.Param {
	t.Helper()
	op, ok := cat.Lookup(operationID)
	if !ok {
		t.Fatalf("operation %s is not in the catalog", operationID)
	}
	for _, p := range op.Params {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("operation %s has no parameter %s", operationID, name)
	return catalog.Param{}
}

// The page size of a list operation is bounded by the catalog itself (1 to
// 100, 50 when absent), so its schema carries those bounds.
func TestTheLimitOfAListOperationCarriesItsBoundsAndDefault(t *testing.T) {
	t.Parallel()
	s := param(t, mustLoad(t), "trace_list", "limit").Schema

	if s.Minimum == nil || *s.Minimum != 1 || s.Maximum == nil || *s.Maximum != 100 || s.Default != 50 {
		t.Fatalf("limit schema = {minimum %v, maximum %v, default %v}, want 1, 100, 50", s.Minimum, s.Maximum, s.Default)
	}
}

func TestAParameterWithoutBoundsCarriesNone(t *testing.T) {
	t.Parallel()
	s := param(t, mustLoad(t), "trace_list", "page").Schema

	if s.Minimum != nil || s.Maximum != nil || s.MinLength != nil || s.MaxLength != nil || s.Default != nil {
		t.Fatalf("page schema = %+v, want no bounds and no default", s)
	}
}

func TestADateTimeParameterCarriesItsFormat(t *testing.T) {
	t.Parallel()
	if got := param(t, mustLoad(t), "trace_list", "fromTimestamp").Schema.Format; got != "date-time" {
		t.Fatalf("fromTimestamp format = %q, want date-time", got)
	}
}
