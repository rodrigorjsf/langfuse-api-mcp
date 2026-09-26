package catalog_test

import (
	"slices"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Deployment-profile fixtures (ADR-0012 Consequences, spec #68 seam 2): the
// embedded union catalog resolves each profile to exactly these operation
// sets. They are the offline freshness check of every pull request.
//
// Independent evidence behind the literals: the 3.80.0 set is every
// operation a live 3.80.0 answered with something other than an HTML 404
// (prototype/langfuse-io-window, scan-3.80.0.json), except six routes that
// exist only for another method there (405); the 4.46.0 dual set is the 124
// operations of the 4.46.0 spec, all routed on a live 4.46.0 dual
// (prototype/langfuse-api-versions), minus the 13 ADR-0004 exclusions.
//
// The weekly regeneration (.github/workflows/union-catalog.yml) changes the
// "newer than any known version" row when a new release adds or drops an
// operation: update that literal in the regeneration pull request.

// inEveryFixture are the operations every fixture below keeps.
var inEveryFixture = []string{
	"comments_create", "comments_get", "comments_get-by-id", "datasetItems_create", "datasetItems_get",
	"datasetItems_list", "datasets_create", "datasets_get", "datasets_list", "health_health",
	"legacy_scoreV1_delete", "media_get", "media_getUploadUrl", "media_patch", "models_create", "models_delete",
	"models_get", "models_list", "projects_get", "prompts_create", "prompts_get", "prompts_list",
	"scoreConfigs_create", "scoreConfigs_get", "scoreConfigs_get-by-id", "scores_create",
}

func TestEachDeploymentProfileResolvesToExactlyItsOperations(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	legacy, v4, experiments := catalog.LegacyFamily, catalog.V4ReadFamily, catalog.ExperimentsFamily
	dual4460 := []string{
		"annotationQueues_createQueue", "annotationQueues_createQueueAssignment",
		"annotationQueues_createQueueItem", "annotationQueues_deleteQueueAssignment",
		"annotationQueues_deleteQueueItem", "annotationQueues_getQueue", "annotationQueues_getQueueItem",
		"annotationQueues_listQueueItems", "annotationQueues_listQueues", "annotationQueues_updateQueueItem",
		"blobStorageIntegrations_deleteBlobStorageIntegration",
		"blobStorageIntegrations_getBlobStorageIntegrationStatus",
		"blobStorageIntegrations_getBlobStorageIntegrations",
		"blobStorageIntegrations_upsertBlobStorageIntegration", "datasetItems_delete", "datasetRunItems_create",
		"datasetRunItems_list", "datasets_deleteRun", "datasets_getRun", "datasets_getRuns",
		"evaluationRules_create", "evaluationRules_delete", "evaluationRules_get", "evaluationRules_list",
		"evaluationRules_update", "evaluators_create", "evaluators_delete", "evaluators_get", "evaluators_list",
		"evaluators_listVersions", "evaluators_update", "experiments_list", "experiments_listItems",
		"feedback_submit", "legacy_metricsV1_metrics", "legacy_observationsV1_get", "legacy_observationsV1_getMany",
		"llmConnections_delete", "llmConnections_list", "llmConnections_upsert", "metrics_metrics", "models_upsert",
		"observations_getMany", "organizations_getOrganizationApiKeys", "organizations_getOrganizationMemberships",
		"organizations_getOrganizationProjects", "organizations_getProjectMemberships", "projects_getApiKeys",
		"promptVersion_update", "prompts_delete", "scim_getResourceTypes", "scim_getSchemas",
		"scim_getServiceProviderConfig", "scim_getUser", "scim_listUsers", "scoreConfigs_update",
		"scoresV3_getManyV3", "scores_get-by-id", "scores_get-many", "sessions_get", "sessions_list",
		"trace_delete", "trace_deleteMultiple", "trace_get", "trace_list", "unstable_dashboardWidgets_create",
		"unstable_dashboardWidgets_delete", "unstable_dashboardWidgets_get", "unstable_dashboardWidgets_list",
		"unstable_dashboardWidgets_update", "unstable_dashboards_addPlacement", "unstable_dashboards_create",
		"unstable_dashboards_delete", "unstable_dashboards_deletePlacement", "unstable_dashboards_get",
		"unstable_dashboards_list", "unstable_dashboards_update", "unstable_dashboards_updatePlacement",
		"unstable_skills_createVersion", "unstable_skills_deleteVersion", "unstable_skills_get",
		"unstable_skills_getFileContent", "unstable_skills_list", "unstable_skills_setLabels",
		"unstable_skills_update",
	}

	for _, tc := range []struct {
		name    string
		profile catalog.Profile
		also    []string // besides inEveryFixture
	}{
		{"3.80.0, legacy only", catalog.Profile{Version: "3.80.0", Families: []catalog.Family{legacy}}, []string{
			"annotationQueues_createQueueItem", "annotationQueues_deleteQueueItem", "annotationQueues_getQueue",
			"annotationQueues_getQueueItem", "annotationQueues_listQueueItems", "annotationQueues_listQueues",
			"annotationQueues_updateQueueItem", "datasetItems_delete", "datasetRunItems_create", "datasetRunItems_list",
			"datasets_deleteRun", "datasets_getRun", "datasets_getRuns", "legacy_metricsV1_metrics",
			"legacy_observationsV1_get", "legacy_observationsV1_getMany", "organizations_getOrganizationMemberships",
			"organizations_getOrganizationProjects", "organizations_getProjectMemberships", "projects_getApiKeys",
			"promptVersion_update", "scim_getResourceTypes", "scim_getSchemas", "scim_getServiceProviderConfig",
			"scim_getUser", "scim_listUsers", "scores_get-by-id", "scores_get-many", "sessions_get", "sessions_list",
			"trace_delete", "trace_deleteMultiple", "trace_get", "trace_list",
		}},
		{"latest 3.x, legacy and experiments", catalog.Profile{Version: "3.225.11", Families: []catalog.Family{legacy, experiments}}, []string{
			"annotationQueues_createQueue", "annotationQueues_createQueueAssignment",
			"annotationQueues_createQueueItem", "annotationQueues_deleteQueueAssignment",
			"annotationQueues_deleteQueueItem", "annotationQueues_getQueue", "annotationQueues_getQueueItem",
			"annotationQueues_listQueueItems", "annotationQueues_listQueues", "annotationQueues_updateQueueItem",
			"blobStorageIntegrations_deleteBlobStorageIntegration",
			"blobStorageIntegrations_getBlobStorageIntegrationStatus",
			"blobStorageIntegrations_getBlobStorageIntegrations",
			"blobStorageIntegrations_upsertBlobStorageIntegration", "datasetItems_delete", "datasetRunItems_create",
			"datasetRunItems_list", "datasets_deleteRun", "datasets_getRun", "datasets_getRuns", "experiments_list",
			"experiments_listItems", "legacy_metricsV1_metrics", "legacy_observationsV1_get",
			"legacy_observationsV1_getMany", "llmConnections_delete", "llmConnections_list", "llmConnections_upsert",
			"organizations_getOrganizationApiKeys", "organizations_getOrganizationMemberships",
			"organizations_getOrganizationProjects", "organizations_getProjectMemberships", "projects_getApiKeys",
			"promptVersion_update", "prompts_delete", "scim_getResourceTypes", "scim_getSchemas",
			"scim_getServiceProviderConfig", "scim_getUser", "scim_listUsers", "scoreConfigs_update",
			"scoresV3_getManyV3", "scores_get-by-id", "scores_get-many", "sessions_get", "sessions_list",
			"trace_delete", "trace_deleteMultiple", "trace_get", "trace_list", "unstable_dashboardWidgets_create",
			"unstable_dashboardWidgets_delete", "unstable_dashboardWidgets_get", "unstable_dashboardWidgets_list",
			"unstable_dashboardWidgets_update", "unstable_dashboards_addPlacement", "unstable_dashboards_create",
			"unstable_dashboards_delete", "unstable_dashboards_deletePlacement", "unstable_dashboards_get",
			"unstable_dashboards_list", "unstable_dashboards_update", "unstable_dashboards_updatePlacement",
			"unstable_evaluationRules_create", "unstable_evaluationRules_delete", "unstable_evaluationRules_get",
			"unstable_evaluationRules_list", "unstable_evaluationRules_update", "unstable_evaluators_create",
			"unstable_evaluators_delete", "unstable_evaluators_get", "unstable_evaluators_list",
		}},
		{"4.x events_only", catalog.Profile{Version: "4.46.0", Families: []catalog.Family{v4, experiments}}, []string{
			"annotationQueues_createQueue", "annotationQueues_createQueueAssignment",
			"annotationQueues_createQueueItem", "annotationQueues_deleteQueueAssignment",
			"annotationQueues_deleteQueueItem", "annotationQueues_getQueue", "annotationQueues_getQueueItem",
			"annotationQueues_listQueueItems", "annotationQueues_listQueues", "annotationQueues_updateQueueItem",
			"blobStorageIntegrations_deleteBlobStorageIntegration",
			"blobStorageIntegrations_getBlobStorageIntegrationStatus",
			"blobStorageIntegrations_getBlobStorageIntegrations",
			"blobStorageIntegrations_upsertBlobStorageIntegration", "datasetItems_delete", "evaluationRules_create",
			"evaluationRules_delete", "evaluationRules_get", "evaluationRules_list", "evaluationRules_update",
			"evaluators_create", "evaluators_delete", "evaluators_get", "evaluators_list", "evaluators_listVersions",
			"evaluators_update", "experiments_list", "experiments_listItems", "feedback_submit",
			"llmConnections_delete", "llmConnections_list", "llmConnections_upsert", "metrics_metrics", "models_upsert",
			"observations_getMany", "organizations_getOrganizationApiKeys", "organizations_getOrganizationMemberships",
			"organizations_getOrganizationProjects", "organizations_getProjectMemberships", "projects_getApiKeys",
			"promptVersion_update", "prompts_delete", "scim_getResourceTypes", "scim_getSchemas",
			"scim_getServiceProviderConfig", "scim_getUser", "scim_listUsers", "scoreConfigs_update",
			"scoresV3_getManyV3", "trace_delete", "trace_deleteMultiple", "unstable_dashboardWidgets_create",
			"unstable_dashboardWidgets_delete", "unstable_dashboardWidgets_get", "unstable_dashboardWidgets_list",
			"unstable_dashboardWidgets_update", "unstable_dashboards_addPlacement", "unstable_dashboards_create",
			"unstable_dashboards_delete", "unstable_dashboards_deletePlacement", "unstable_dashboards_get",
			"unstable_dashboards_list", "unstable_dashboards_update", "unstable_dashboards_updatePlacement",
			"unstable_skills_createVersion", "unstable_skills_deleteVersion", "unstable_skills_get",
			"unstable_skills_getFileContent", "unstable_skills_list", "unstable_skills_setLabels",
			"unstable_skills_update",
		}},
		{"4.x dual", catalog.Profile{Version: "4.46.0", Families: []catalog.Family{legacy, v4, experiments}}, dual4460},
		{"newer than any known version: the newest known one", catalog.Profile{Version: "5.0.0", Families: []catalog.Family{legacy, v4, experiments}}, dual4460},
		// Families are ignored: none is on, yet the legacy reads stay.
		{"below v3.0.0: ranges alone", catalog.Profile{Version: "2.95.0"}, []string{
			"datasetRunItems_create", "datasets_getRun", "datasets_getRuns", "legacy_observationsV1_get",
			"legacy_observationsV1_getMany", "metrics_daily", "score_get", "score_get-by-id", "sessions_get",
			"sessions_list", "trace_get", "trace_list",
		}},
	} {
		want := slices.Sorted(slices.Values(append(slices.Clone(inEveryFixture), tc.also...)))
		if got := ids(cat.Resolve(tc.profile).Operations()); !slices.Equal(got, want) {
			t.Errorf("%s: resolved %d operations, want %d\n  extra:   %v\n  missing: %v",
				tc.name, len(got), len(want), without(got, want), without(want, got))
		}
	}
}

// without returns the elements of a that are not in b.
func without(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}
