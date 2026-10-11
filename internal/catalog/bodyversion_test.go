package catalog_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

// bodiesBeforeTheHistory is the SHA-256 of the body schema each operation that
// takes a body had in the catalog before #202 kept the body of every release,
// as the catalog resolved it for a version not detected (the newest release's):
// computed with the code and the catalog of commit 236aaa4, so the newest body
// is shown to be unchanged by a value the new code did not produce. Every body
// of those operations was required.
var bodiesBeforeTheHistory = map[string]string{
	"annotationQueues_createQueue":           "219999c83bc0d2ad26c6ab0f0b188a3b8de2c26b2194918815fed862090178c2",
	"annotationQueues_createQueueAssignment": "60a346e31b4b44000c35297a212dd0b0e72d903e2838f688eed80d0a0054dbea",
	"annotationQueues_createQueueItem":       "8620a59b0c822fc10c1c0d6aebd27de0e5e77b813ea36af455cb37f6d81b2ac6",
	"annotationQueues_deleteQueueAssignment": "60a346e31b4b44000c35297a212dd0b0e72d903e2838f688eed80d0a0054dbea",
	"annotationQueues_updateQueueItem":       "86b29c5f50108fd8ec49fd0448eed1fa8557e979716854c4cf73b0eb3cdb2935",
	"comments_create":                        "cef61d0b9c9bd03145336a5164e33a22f4186239f464f18232e4cd899b6be76e",
	"datasetItems_create":                    "b69e7b941edf42f96d08b72f4413bd3da0e4de72046ac5835bf6ca795eaf3e15",
	"datasetRunItems_create":                 "85f56f75f3d40833fd83d5694cf90b6402d6e9642a0961d8fd6436a5d91e8a69",
	"datasets_create":                        "a695a3899fc66cec9fc3a4b72e92314208cb8a23db89c65ceb97c5bbd1ccda1b",
	"evaluationRules_create":                 "4f0ceeb41eea288545a4b6e505ea27715c66ffeac0e39fd5577777c0d5b243d0",
	"evaluationRules_update":                 "4affc9bbcdb4ea915c2f3c12ff60abb9d5aa1096ca2999d64d0c304c21446faa",
	"evaluators_create":                      "3128d557523bd6da78af29dd099c663e1ac9b3222d73536bc355de90e8fc3657",
	"evaluators_update":                      "59919dae783a1798d521de275eca661cb452fef5dd7eb6008b11671934fa3056",
	"feedback_submit":                        "c5b4316a6f3d9b90e7e6c8e3f4ae97aa42d5f7031608d8a7ce69389067bab7fe",
	"models_create":                          "5480851bfff342a524bc757aff2f5d352305d58391b3361e534fed7f4405204c",
	"models_upsert":                          "5480851bfff342a524bc757aff2f5d352305d58391b3361e534fed7f4405204c",
	"promptVersion_update":                   "bd8693228ce6022155524f5f5e023f6f59eed2a376042f8de2ec608246281485",
	"prompts_create":                         "08bdde6b0d9daf55138f0fbfcb311909ee4d8f84f2623ea1c1f34671ce32acd9",
	"scoreConfigs_create":                    "132d20defa037e6de451ae0ae180d194385e8310767af641ec8f4b2da0775d53",
	"scoreConfigs_update":                    "d917e2ce99e1aeaeca13ea4a316610e878304e6a2b6f87cd370186375ebfdc68",
	"scores_create":                          "f87c4f1173592bb2aa37a1da9c800f8e73894d9045f7bbc2f0794b4309c92a8d",
	"trace_deleteMultiple":                   "a876e15d343e878257c002c377f1abe3bc6260fbb2730f3e0dc4755005997931",
	"unstable_dashboardWidgets_create":       "4ef8f58b3b7d67655643b075e8afd6090e8b75a6ab52acd643275a8e1f98ba9f",
	"unstable_dashboardWidgets_update":       "166635238cafd63efcd611c47a5c365e790498f93b6d7e495c92cf1d3d06ca03",
	"unstable_dashboards_addPlacement":       "47eb46bf2ee78e0eb976981deb3c6f7f2b74d9c680c85b9218e72131e93331cb",
	"unstable_dashboards_create":             "fcab76708b563c5112d6e90037478fec1b15db88adf0e40e37643e25799b2a9d",
	"unstable_dashboards_update":             "42a79f7d8d96d334556bac42855942e72324109db7613e25b23582b66146e131",
	"unstable_dashboards_updatePlacement":    "3ba33684013fba3acb53416e6638ae8d8ee4cab04ddba41c8208bfc3bd179e17",
	"unstable_evaluationRules_create":        "7a2277f8871c627a7dcca72fd17ef525a3063241c076eb355a41995e97779ff7",
	"unstable_evaluationRules_update":        "7f528cfde974b911039cc4a5e39be581ab45fa10d90d73476a3bd11727f1254d",
	"unstable_evaluators_create":             "b4034f1300f76f9799da758a588ebfcb2156413bcfedee66f9ab4939f853ac3d",
	"unstable_skills_createVersion":          "d9bffc96c94cbb6504493f9c1f1bb1c21410c853248c2bec2b31310e9cadde2b",
	"unstable_skills_setLabels":              "333b3e0984acc93bca922d6caee6fa2b14c4f8f692a4dc1e74dd44a4e06d008b",
	"unstable_skills_update":                 "80b1bf9df07af5cb0712f270435360f783fab495ad6347282d45c0f5ac21af4c",
}

func TestForTheNewestVersionEveryOperationHasTheBodyItHadBeforeTheHistory(t *testing.T) {
	t.Parallel()
	// unstable_evaluationRules_create, unstable_evaluationRules_update and
	// unstable_evaluators_create left the spec in 4.31.0: only a version not
	// detected, which keeps every range, still offers them.
	left := []string{"unstable_evaluationRules_create", "unstable_evaluationRules_update", "unstable_evaluators_create"}
	for _, version := range []string{"", "4.50.0", "9.9.9"} {
		cat := mustLoad(t).Resolve(catalog.Profile{Version: version, Families: catalog.AllFamilies()})
		got := map[string]string{}
		for _, op := range cat.Operations() {
			if op.Body != nil {
				if !op.Body.Required {
					t.Errorf("at %q: the body of %s is not required", version, op.ID)
				}
				got[op.ID] = fmt.Sprintf("%x", sha256.Sum256(op.Body.Schema))
			}
		}
		for id, want := range bodiesBeforeTheHistory {
			if _, offered := got[id]; !offered && version != "" && slices.Contains(left, id) {
				continue
			}
			if got[id] != want {
				t.Errorf("at %q: the body schema of %s has SHA-256 %q, want %q", version, id, got[id], want)
			}
		}
		for id := range got {
			if _, known := bodiesBeforeTheHistory[id]; !known {
				t.Errorf("at %q: %s has a body now and had none before", version, id)
			}
		}
	}
}

// Every write operation of every deployment-profile fixture has the one body
// the release specs give it (and none where they give it none): the 34
// operations of bodiesBeforeTheHistory take one in every release they are in.
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
			_, takesOne := bodiesBeforeTheHistory[op.ID]
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
// distinct body once, however many releases and times it came back (literals:
// the generator's output for v3.0.0 to v4.50.0).
func TestTheCatalogKeepsEveryDistinctBodyTheReleaseSpecsGaveAnOperation(t *testing.T) {
	t.Parallel()
	got := mustLoad(t).StoredBodies()
	for key, want := range map[string]int{
		"POST /api/public/scores":                                    11, // sessionId, datasetRunId, queueId, source ...
		"POST /api/public/models":                                    5,  // unit required, optional, required; 3.28.2 comes back
		"POST /api/public/v2/prompts":                                4,
		"POST /api/public/comments":                                  2,
		"POST /api/public/v2/datasets":                               3,
		"POST /api/public/score-configs":                             2,
		"POST /api/public/unstable/evaluation-rules":                 6,
		"POST /api/public/v2/evaluation-rules":                       1,
		"DELETE /api/public/annotation-queues/{queueId}/assignments": 1,
	} {
		if len(got[key]) != want {
			t.Errorf("%s: %d stored bodies, want %d", key, len(got[key]), want)
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
	if bodies < 76 {
		t.Fatalf("walked %d stored bodies, want the whole history of the operations the server offers (76)", bodies)
	}
}
