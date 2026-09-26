package catalog_test

import (
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

func mustLoad(t testing.TB) catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return cat
}

// ADR-0004: ingestion, OTLP and organization admin mutations are never in the
// catalog. A spec update that adds an operation of these kinds must be triaged.
func TestCatalogContainsNoExcludedOperation(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	for _, id := range []string{
		"ingestion_batch",
		"opentelemetry_exportTraces",
		"projects_create", "projects_update", "projects_delete",
		"projects_createApiKey", "projects_deleteApiKey",
		"organizations_updateOrganizationMembership", "organizations_deleteOrganizationMembership",
		"organizations_updateProjectMembership", "organizations_deleteProjectMembership",
		"scim_createUser", "scim_deleteUser",
	} {
		if _, ok := cat.Lookup(id); ok {
			t.Errorf("excluded operation %s is in the catalog", id)
		}
	}
	for _, op := range cat.Operations() {
		switch op.Path {
		case "/api/public/ingestion", "/api/public/otel/v1/traces":
			t.Errorf("operation %s on excluded path %s is in the catalog", op.ID, op.Path)
		}
	}
}

// Until it is resolved, the catalog is the whole union catalog minus the
// exclusions: organization reads, deprecated reads, operations of older
// releases and every other operation stay in.
func TestCatalogKeepsEveryOperationOfTheUnionThatIsNotExcluded(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	// The union catalog (v3.0.0 to v4.46.0) has 137 operations, 13 of them
	// excluded; promptVersion_update names two of them (its path changed in
	// 3.18.0), and the newer one wins.
	if got := len(cat.Operations()); got != 123 {
		t.Fatalf("catalog has %d operations, want 123", got)
	}
	for id, want := range map[string]string{ //nolint:gosec // G101: operation IDs, not credentials
		"trace_list":                           "GET /api/public/traces",
		"trace_get":                            "GET /api/public/traces/{traceId}",
		"legacy_observationsV1_getMany":        "GET /api/public/observations",
		"organizations_getOrganizationApiKeys": "GET /api/public/organizations/apiKeys",
		"prompts_create":                       "POST /api/public/v2/prompts",
		"score_get":                            "GET /api/public/scores",
		"promptVersion_update":                 "PATCH /api/public/v2/prompts/{name}/versions/{version}",
	} {
		op, ok := cat.Lookup(id)
		if !ok {
			t.Errorf("operation %s missing from the catalog", id)
			continue
		}
		if got := op.Method + " " + op.Path; got != want {
			t.Errorf("operation %s = %s, want %s", id, got, want)
		}
	}
}

// ADR-0012 §1: each operation carries its version range, taken from the
// release specs or from an earlier floor the docs state, and its family.
func TestEveryUnionOperationCarriesItsRangeAndFamily(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	for id, want := range map[string][3]string{ // introduced, removed, family
		"trace_list":                {"3.0.0", "", "legacy"},
		"datasets_getRuns":          {"3.0.0", "", "legacy"},
		"observations_getMany":      {"3.141.0", "", "v4 read"},
		"metrics_metrics":           {"3.141.0", "", "v4 read"},
		"experiments_list":          {"3.206.0", "", "experiments"},
		"scoresV3_getManyV3":        {"3.179.0", "", ""}, // docs floor; first in the spec at 3.180.0
		"score_get":                 {"3.0.0", "3.53.0", ""},
		"metrics_daily":             {"3.0.0", "3.62.0", ""},
		"unstable_evaluators_list":  {"3.170.0", "4.31.0", ""}, // deprecated, then removed: bounded by range only
		"unstable_skills_list":      {"4.46.0", "", ""},
		"annotationQueues_getQueue": {"3.42.0", "", ""},
	} {
		op, ok := cat.Lookup(id)
		if !ok {
			t.Errorf("operation %s missing from the catalog", id)
			continue
		}
		if got := [3]string{op.Introduced, op.Removed, string(op.Family)}; got != want {
			t.Errorf("%s = {introduced, removed, family} %q, want %q", id, got, want)
		}
	}
}
