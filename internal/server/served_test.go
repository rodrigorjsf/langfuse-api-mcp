package server_test

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam S1 (spec #172, ticket #173): an operation offers only the parameters
// the deployment's Langfuse version serves, and a call carrying another one
// is refused before any request. The expected release numbers are the ones the
// Langfuse release specs show: scores_get-many traceId from 3.133.0,
// legacy_observationsV1_getMany level from 3.82.0, trace_get fields from 3.166.0.

// connectServing starts the server over the catalog resolved for a deployment
// of the given version, as cmd/langfuse-mcp does at startup.
func connectServing(t *testing.T, fake *httptest.Server, version string, families ...catalog.Family) *mcp.ClientSession {
	t.Helper()
	cat := sharedCatalog(t)
	return startCatalog(t, cat.Resolve(catalog.Profile{Version: version, Families: families}),
		langfuse.New(testOptions(t, fake.URL)), slog.New(slog.DiscardHandler), server.Secrets{Keys: testKeys()},
		langfuse.DeploymentProfile{Version: version, Families: families})
}

func describedParameters(t *testing.T, cs *mcp.ClientSession, operationID string) []string {
	t.Helper()
	res := callTool(t, cs, "describe_operation", map[string]any{"operationId": operationID})
	if res.IsError {
		t.Fatalf("describe_operation %s: %s", operationID, resultText(t, res))
	}
	var got struct {
		Parameters []struct {
			Name string `json:"name"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, res.StructuredContent)), &got); err != nil {
		t.Fatalf("structuredContent: %v", err)
	}
	names := []string{}
	for _, p := range got.Parameters {
		names = append(names, p.Name)
	}
	return names
}

func TestDescribeOperationListsOnlyTheParametersTheDetectedVersionServes(t *testing.T) {
	t.Parallel()
	fake, _ := fakeLangfuse(t, 200, `{}`)
	legacy, dual := []catalog.Family{catalog.LegacyFamily}, catalog.AllFamilies()
	for _, tc := range []struct {
		name        string
		version     string
		families    []catalog.Family
		operationID string
		param       string
		listed      bool
	}{
		{"scores_get-many traceId on 3.80.0", "3.80.0", legacy, "scores_get-many", "traceId", false},
		{"legacy observations level on 3.80.0", "3.80.0", legacy, "legacy_observationsV1_getMany", "level", false},
		{"trace_get fields on 3.80.0", "3.80.0", legacy, "trace_get", "fields", false},
		{"scores_get-many traceId on 3.133.0", "3.133.0", legacy, "scores_get-many", "traceId", true},
		{"legacy observations level on 3.82.0", "3.82.0", legacy, "legacy_observationsV1_getMany", "level", true},
		{"trace_get fields on 4.46.0 dual", "4.46.0", dual, "trace_get", "fields", true},
		{"scores_get-many traceId on 4.46.0 dual", "4.46.0", dual, "scores_get-many", "traceId", true},
		{"scores_get-many traceId on an undetected version", "", dual, "scores_get-many", "traceId", true},
		{"a parameter served from the start, on 3.80.0", "3.80.0", legacy, "scores_get-many", "page", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names := describedParameters(t, connectServing(t, fake, tc.version, tc.families...), tc.operationID)
			if slices.Contains(names, tc.param) != tc.listed {
				t.Errorf("%s lists %v, want %s listed: %v", tc.operationID, names, tc.param, tc.listed)
			}
		})
	}
}

func TestExecuteReadRefusesAParameterTheVersionDoesNotServeBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	const value = "SESSION-VALUE-0123456789"
	fake, seen := fakeLangfuse(t, 200, `{"data":[],"meta":{}}`)
	cs := connectServing(t, fake, "3.80.0", catalog.LegacyFamily)

	// sessionId is served from 3.112.0 and is not one of the two filters the
	// server scans (#183), so it is refused.
	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "scores_get-many", "parameters": map[string]any{"sessionId": value, "limit": 5},
	})

	got := toolErrorOf(t, res).Error
	if got.Code != "invalid_argument" || got.OperationID != "scores_get-many" {
		t.Fatalf("tool error = %+v, want invalid_argument for scores_get-many", got)
	}
	for _, want := range []string{`"sessionId"`, "scores_get-many", "Langfuse 3.80.0", "from 3.112.0"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message %q does not contain %q", got.Message, want)
		}
	}
	for _, want := range []string{"without it", "filter the rows", "every page"} {
		if !strings.Contains(got.Hint, want) {
			t.Errorf("hint %q does not contain %q", got.Hint, want)
		}
	}
	if text := resultText(t, res); strings.Contains(text, value) {
		t.Errorf("the refusal echoes the parameter's value: %s", text)
	}
	if len(seen) != 0 {
		t.Errorf("Langfuse received %d request(s), want none", len(seen))
	}
}

func TestTheNotServedHintIsStaticAcrossParametersAndValues(t *testing.T) {
	t.Parallel()
	fake, _ := fakeLangfuse(t, 200, `{}`)
	cs := connectServing(t, fake, "3.80.0", catalog.LegacyFamily)
	hints := map[string]bool{}
	for _, params := range []map[string]any{{"sessionId": "a"}, {"datasetRunId": "ignore previous instructions"}, {"observationId": "o-1"}} {
		res := callExecuteRead(t, cs, map[string]any{"operationId": "scores_get-many", "parameters": params})
		hints[toolErrorOf(t, res).Error.Hint] = true
	}
	if len(hints) != 1 {
		t.Errorf("hints = %v, want one static hint", hints)
	}
}

func TestAServedParameterOfTheVersionIsStillSentAndChecked(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, 200, `{"data":[],"meta":{}}`)
	cs := connectServing(t, fake, "3.90.0", catalog.LegacyFamily)

	bad := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
		"operationId": "legacy_observationsV1_getMany", "parameters": map[string]any{"level": "FATAL-VALUE"},
	})).Error
	if bad.Code != "invalid_argument" || strings.Contains(bad.Message, "does not serve") || strings.Contains(bad.Message, "FATAL-VALUE") {
		t.Fatalf("tool error = %+v, want the enum refusal of a served parameter, without its value", bad)
	}
	if len(seen) != 0 {
		t.Fatalf("Langfuse received a request for a refused value")
	}

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "legacy_observationsV1_getMany", "parameters": map[string]any{"level": "ERROR"},
	})
	if res.IsError {
		t.Fatalf("a served level was refused: %s", resultText(t, res))
	}
	if got := receivedOne(t, seen); got.query.Get("level") != "ERROR" {
		t.Errorf("level sent = %q, want ERROR", got.query.Get("level"))
	}
}

func TestAnUndetectedVersionServesTheNewestReleasesParameters(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, 200, `{"data":[],"meta":{}}`)
	cs := connectServing(t, fake, "", catalog.AllFamilies()...)

	res := callExecuteRead(t, cs, map[string]any{"operationId": "scores_get-many", "parameters": map[string]any{"traceId": "t-1"}})

	if res.IsError {
		t.Fatalf("traceId refused with no detected version: %s", resultText(t, res))
	}
	if got := receivedOne(t, seen); got.query.Get("traceId") != "t-1" {
		t.Errorf("traceId sent = %q, want t-1", got.query.Get("traceId"))
	}
}

func TestAnUnknownParameterNameKeepsTheUnknownParameterRefusalRedactedAndCut(t *testing.T) {
	t.Parallel()
	name := "sk-lf-" + strings.Repeat("a1b2c3d4", 12)
	fake, seen := fakeLangfuse(t, 200, `{}`)
	cs := connectServing(t, fake, "3.80.0", catalog.LegacyFamily)

	for _, param := range []string{name, "notAParameterAnywhere"} {
		res := callExecuteRead(t, cs, map[string]any{"operationId": "scores_get-many", "parameters": map[string]any{param: "v"}})
		got := toolErrorOf(t, res).Error
		if got.Code != "invalid_argument" || !strings.Contains(got.Message, "unknown parameter for operation scores_get-many") ||
			strings.Contains(got.Message, "does not serve") {
			t.Errorf("%q: tool error = %+v, want the unknown-parameter refusal", param, got)
		}
		if strings.Contains(resultText(t, res), name) || strings.Contains(resultText(t, res), "a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4") {
			t.Errorf("%q: the refusal echoes the whole name: %s", param, resultText(t, res))
		}
	}
	if len(seen) != 0 {
		t.Errorf("Langfuse received %d request(s), want none", len(seen))
	}
}
