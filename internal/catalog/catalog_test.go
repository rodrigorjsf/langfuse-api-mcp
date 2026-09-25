package catalog_test

import (
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

func mustLoad(t *testing.T) catalog.Catalog {
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

// The catalog is the embedded spec minus the exclusions: organization reads,
// deprecated reads and every other operation stay in.
func TestCatalogKeepsEveryOperationOfTheSpecThatIsNotExcluded(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	// The embedded spec has 117 operations, 13 of them excluded.
	if got := len(cat.Operations()); got != 104 {
		t.Fatalf("catalog has %d operations, want 104", got)
	}
	for id, want := range map[string]string{ //nolint:gosec // G101: operation IDs, not credentials
		"trace_list":                           "GET /api/public/traces",
		"trace_get":                            "GET /api/public/traces/{traceId}",
		"legacy_observationsV1_getMany":        "GET /api/public/observations",
		"organizations_getOrganizationApiKeys": "GET /api/public/organizations/apiKeys",
		"prompts_create":                       "POST /api/public/v2/prompts",
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
