package catalog_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// #202: a write operation has the request body of the connected deployment's
// Langfuse version, the body of the newest release that is not newer than the
// deployment (the rule the served parameters use, #173). The expected values are
// literals from the release specs (docs/research/langfuse.md, section 1.14):
// CreateScoreRequest requires traceId and has neither sessionId nor
// datasetRunId up to 3.52.0, gains sessionId in 3.53.0 (and stops requiring
// traceId) and datasetRunId in 3.55.0; CreateModelRequest requires unit at
// 3.0.0, does not and allows null from 3.8.0, and requires it again from 4.19.0.

// resolvedAt returns the operation as the catalog resolves it for a deployment
// of the given version with every family on; "" is a version not detected.
func resolvedAt(t *testing.T, version, id string) catalog.Operation {
	t.Helper()
	op, ok := mustLoad(t).Resolve(catalog.Profile{Version: version, Families: catalog.AllFamilies()}).Lookup(id)
	if !ok {
		t.Fatalf("%s is not offered at %q", id, version)
	}
	return op
}

// bodyShape is the part of a body schema the cases below compare.
type bodyShape struct {
	Required   []string                   `json:"required"`
	Properties map[string]json.RawMessage `json:"properties"`
}

func shapeOf(t *testing.T, op catalog.Operation) bodyShape {
	t.Helper()
	if op.Body == nil {
		t.Fatalf("%s has no body", op.ID)
	}
	var s bodyShape
	if err := json.Unmarshal(op.Body.Schema, &s); err != nil {
		t.Fatalf("%s body schema: %v", op.ID, err)
	}
	return s
}

func (s bodyShape) has(name string) bool { _, ok := s.Properties[name]; return ok }

func TestScoresCreateHasTheBodySchemaOfTheDeploymentsVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version                 string
		sessionID, datasetRunID bool
		traceIDRequired         bool
		wantRequired            []string
		description             string
	}{
		{"3.30.0", false, false, true, []string{"traceId", "name", "value"}, "before sessionId"},
		{"3.52.0", false, false, true, []string{"traceId", "name", "value"}, "the last release before sessionId"},
		{"3.53.0", true, false, false, []string{"name", "value"}, "the release that adds sessionId"},
		{"3.54.1", true, false, false, []string{"name", "value"}, "sessionId and no datasetRunId"},
		{"3.55.0", true, true, false, []string{"name", "value"}, "the release that adds datasetRunId"},
		{"4.46.0", true, true, false, []string{"name", "value"}, "the 4.x pin"},
		{"", true, true, false, []string{"name", "value"}, "a version not detected is the newest release"},
		{"9.9.9", true, true, false, []string{"name", "value"}, "a version newer than any known one is the newest release"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			op := resolvedAt(t, tc.version, "scores_create")
			s := shapeOf(t, op)
			if s.has("sessionId") != tc.sessionID || s.has("datasetRunId") != tc.datasetRunID {
				t.Errorf("at %q: sessionId %v, datasetRunId %v; want %v, %v", tc.version,
					s.has("sessionId"), s.has("datasetRunId"), tc.sessionID, tc.datasetRunID)
			}
			if !slices.Equal(s.Required, tc.wantRequired) {
				t.Errorf("at %q: required = %v, want %v", tc.version, s.Required, tc.wantRequired)
			}
			if !op.Body.Required {
				t.Errorf("at %q: the body itself is not required", tc.version)
			}
		})
	}
}

func TestModelsCreateRequiresUnitOnlyWhereTheReleaseSpecDoes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version      string
		wantRequired []string
	}{
		{"3.0.0", []string{"modelName", "matchPattern", "unit"}},
		{"3.80.0", []string{"modelName", "matchPattern"}},
		{"3.225.11", []string{"modelName", "matchPattern"}},
		{"4.18.0", []string{"modelName", "matchPattern"}},
		{"4.19.0", []string{"modelName", "matchPattern", "unit"}},
		{"4.46.0", []string{"modelName", "matchPattern", "unit"}},
		{"", []string{"modelName", "matchPattern", "unit"}},
	} {
		if got := shapeOf(t, resolvedAt(t, tc.version, "models_create")).Required; !slices.Equal(got, tc.wantRequired) {
			t.Errorf("at %q: required = %v, want %v", tc.version, got, tc.wantRequired)
		}
	}
}

// A body is checked against the schema of the deployment's version, never
// another's: the body below is refused where traceId is required and sent where
// the release has sessionId.
func TestCheckBodyChecksAgainstTheBodyOfTheDeploymentsVersion(t *testing.T) {
	t.Parallel()
	const sessionScore = `{"name":"x","value":1,"sessionId":"s"}`
	const traceScore = `{"name":"x","value":1,"traceId":"t"}`
	for _, tc := range []struct {
		version, body string
		refused       bool
	}{
		{"3.30.0", sessionScore, true},
		{"3.30.0", traceScore, false},
		{"3.54.1", sessionScore, false},
		{"4.46.0", sessionScore, false},
		{"4.46.0", traceScore, false},
		{"", sessionScore, false},
	} {
		got, err := resolvedAt(t, tc.version, "scores_create").CheckBody(json.RawMessage(tc.body))
		switch {
		case tc.refused && !errors.Is(err, catalog.ErrInvalidBody):
			t.Errorf("at %q: %s: error %v, want ErrInvalidBody", tc.version, tc.body, err)
		case tc.refused && (!strings.HasSuffix(err.Error(), `at /traceId: fails schema keyword "required"`) ||
			strings.Contains(err.Error(), `"s"`) || strings.Contains(err.Error(), "sessionId")):
			// The refusal names the JSON location of the schema's own property and the
			// keyword, never a key or a value of the caller.
			t.Errorf("at %q: refusal %q, want location /traceId and keyword required, no key or value", tc.version, err)
		case !tc.refused && err != nil:
			t.Errorf("at %q: %s refused: %v", tc.version, tc.body, err)
		case !tc.refused && string(got) != tc.body:
			t.Errorf("at %q: body sent = %s, want %s", tc.version, got, tc.body)
		}
	}
}

func TestModelsCreateWithoutUnitIsSentWhereTheReleaseSpecDoesNotRequireItAndRefusedWhereItDoes(t *testing.T) {
	t.Parallel()
	const noUnit = `{"modelName":"m","matchPattern":"(?i)^m$"}`
	const nullUnit = `{"modelName":"m","matchPattern":"(?i)^m$","unit":null}`
	for _, version := range []string{"3.80.0", "3.225.11"} {
		for _, body := range []string{noUnit, nullUnit} {
			if _, err := resolvedAt(t, version, "models_create").CheckBody(json.RawMessage(body)); err != nil {
				t.Errorf("at %s: %s refused: %v", version, body, err)
			}
		}
	}
	for _, version := range []string{"4.46.0", ""} {
		for _, body := range []string{noUnit, nullUnit} {
			_, err := resolvedAt(t, version, "models_create").CheckBody(json.RawMessage(body))
			if !errors.Is(err, catalog.ErrInvalidBody) {
				t.Errorf("at %q: %s: error %v, want ErrInvalidBody", version, body, err)
			}
		}
	}
}

// writeOperationsWithABody are the write operations of the catalog that take a
// request body in every release they are in, as the release specs give it (v3.0.0
// to v4.50.0; the three unstable_ ones left the spec in 4.31.0). A regeneration
// that adds one changes this list with the deployment-profile fixtures of
// TestEachDeploymentProfileResolvesToExactlyItsOperations.
var writeOperationsWithABody = []string{
	"annotationQueues_createQueue", "annotationQueues_createQueueAssignment",
	"annotationQueues_createQueueItem", "annotationQueues_deleteQueueAssignment",
	"annotationQueues_updateQueueItem", "comments_create", "datasetItems_create", "datasetRunItems_create",
	"datasets_create", "evaluationRules_create", "evaluationRules_update", "evaluators_create",
	"evaluators_update", "feedback_submit", "models_create", "models_upsert", "promptVersion_update",
	"prompts_create", "scoreConfigs_create", "scoreConfigs_update", "scores_create", "trace_deleteMultiple",
	"unstable_dashboardWidgets_create", "unstable_dashboardWidgets_update", "unstable_dashboards_addPlacement",
	"unstable_dashboards_create", "unstable_dashboards_update", "unstable_dashboards_updatePlacement",
	"unstable_evaluationRules_create", "unstable_evaluationRules_update", "unstable_evaluators_create",
	"unstable_skills_createVersion", "unstable_skills_setLabels", "unstable_skills_update",
}

// Every write operation of every deployment-profile fixture has the one body
// the release specs give it (and none where they give it none): the operations
// of writeOperationsWithABody take one in every release they are in.
func TestEveryWriteOperationOfEveryDeploymentProfileHasExactlyTheBodyTheReleaseSpecsGiveIt(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)
	legacy, v4, experiments := catalog.LegacyFamily, catalog.V4ReadFamily, catalog.ExperimentsFamily
	for name, profile := range map[string]catalog.Profile{
		"3.80.0, legacy only":      {Version: "3.80.0", Families: []catalog.Family{legacy}},
		"3.225.11, legacy only":    {Version: "3.225.11", Families: []catalog.Family{legacy}},
		"4.46.0, events_only":      {Version: "4.46.0", Families: []catalog.Family{v4, experiments}},
		"4.46.0, dual":             {Version: "4.46.0", Families: catalog.AllFamilies()},
		"newer than any known one": {Version: "5.0.0", Families: catalog.AllFamilies()},
		"below v3.0.0":             {Version: "2.95.0"},
		"not detected":             {Families: catalog.AllFamilies()},
	} {
		writes := 0
		for _, op := range cat.Resolve(profile).Operations() {
			if op.IsRead() {
				continue
			}
			writes++
			takesOne := slices.Contains(writeOperationsWithABody, op.ID)
			if (op.Body != nil) != takesOne {
				t.Errorf("%s: %s has a body = %v, want %v", name, op.ID, op.Body != nil, takesOne)
			}
		}
		if writes == 0 {
			t.Errorf("%s: no write operation resolved", name)
		}
	}
}

// The release specs changed the body of these operations; the catalog keeps each
// distinct body once, however many releases and times it came back. The numbers
// are what the generator found in v3.0.0 to v4.50.0: a later release can only add
// to them.
func TestTheCatalogKeepsEveryDistinctBodyTheReleaseSpecsGaveAnOperation(t *testing.T) {
	t.Parallel()
	got := mustLoad(t).StoredBodies()
	for key, atLeast := range map[string]int{
		"POST /api/public/scores":                    11, // sessionId, datasetRunId, queueId, source ...
		"POST /api/public/models":                    5,  // unit required, optional, required; 3.28.2 comes back
		"POST /api/public/v2/prompts":                4,
		"POST /api/public/comments":                  2,
		"POST /api/public/v2/datasets":               3,
		"POST /api/public/score-configs":             2,
		"POST /api/public/unstable/evaluation-rules": 6,
		"POST /api/public/v2/evaluation-rules":       1,
	} {
		if len(got[key]) < atLeast {
			t.Errorf("%s: %d stored bodies, want at least %d", key, len(got[key]), atLeast)
		}
	}
}

// ADR-0004 amendment: a write body property that looks like a credential is a
// reason to exclude the operation, whichever release's body holds it. The check
// walks every stored body of every operation the server offers, not only the
// newest, so an older release cannot bring one in.
func TestNoStoredWriteBodyOfAnInScopeOperationHasACredentialProperty(t *testing.T) {
	t.Parallel()
	bodies := 0
	for key, stored := range mustLoad(t).StoredBodies() {
		for i, body := range stored {
			bodies++
			var schema any
			if err := json.Unmarshal(body.Schema, &schema); err != nil {
				t.Fatalf("%s body %d schema: %v", key, i, err)
			}
			for _, name := range propertyNames(schema) {
				if credentialProperty.MatchString(name) {
					t.Errorf("operation %s has a body property %q (body %d of %d) that looks like a credential: "+
						"exclude it (ADR-0004 amendment) or triage the pattern", key, name, i+1, len(stored))
				}
			}
		}
	}
	if bodies < catalog.StoredBodyFloor {
		t.Fatalf("walked %d stored bodies, want the whole history of the operations the server offers (%d)", bodies, catalog.StoredBodyFloor)
	}
}
