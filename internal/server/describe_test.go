package server_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam S1 (spec #68): describe_operation returns the operation description
// from the catalog, without calling Langfuse.

// asJSON decodes a JSON literal for comparison with a structuredContent.
func asJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("bad JSON literal: %v", err)
	}
	return v
}

func TestDescribeOperationIsAnnotatedReadOnlyAndClosedWorld(t *testing.T) {
	t.Parallel()
	tool := toolNamed(t, connectOffline(t), "describe_operation")

	a := tool.Annotations
	if a == nil || !a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || !a.IdempotentHint ||
		a.OpenWorldHint == nil || *a.OpenWorldHint || a.Title == "" || tool.Title == "" {
		t.Fatalf("annotations = %+v, want title, readOnly, idempotent, not destructive, closed-world, all explicit", a)
	}
}

func TestDescribeOperationReturnsEveryParameterWithItsLocationTypeRequiredFlagEnumAndBounds(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		operationID string
		structured  string
		text        []string
	}{
		"a path parameter, an enum and a bounded limit": {
			operationID: "annotationQueues_listQueueItems",
			structured: `{"operationId":"annotationQueues_listQueueItems","tag":"AnnotationQueues",
				"description":"Get items for a specific annotation queue","method":"GET","tool":"execute_read",
				"parameters":[
					{"name":"queueId","in":"path","type":"string","required":true},
					{"name":"status","in":"query","type":"string","required":false,"enum":["PENDING","COMPLETED"]},
					{"name":"page","in":"query","type":"integer","required":false},
					{"name":"limit","in":"query","type":"integer","required":false,"minimum":1,"maximum":100,"default":50}]}`,
			text: []string{
				"annotationQueues_listQueueItems — Get items for a specific annotation queue\n",
				"- queueId (path, string, required)\n",
				"- status (query, string, one of: PENDING, COMPLETED)\n",
				"- page (query, integer)\n",
				"- limit (query, integer, from 1 to 100, default 50)\n",
			},
		},
		"a repeated query parameter and a date-time": {
			operationID: "sessions_list",
			structured: `{"operationId":"sessions_list","tag":"Sessions","method":"GET","tool":"execute_read",
				"parameters":[
					{"name":"page","in":"query","type":"integer","required":false},
					{"name":"limit","in":"query","type":"integer","required":false,"minimum":1,"maximum":100,"default":50},
					{"name":"fromTimestamp","in":"query","type":"string","format":"date-time","required":false},
					{"name":"toTimestamp","in":"query","type":"string","format":"date-time","required":false},
					{"name":"environment","in":"query","type":"string","required":false,"repeated":true}]}`,
			text: []string{
				"- fromTimestamp (query, string, date-time)\n",
				"- environment (query, string, repeatable: a list sends one value each)\n",
			},
		},
		"no parameter": {
			operationID: "health_health",
			structured: `{"operationId":"health_health","tag":"Health","description":"Check health of API and database",
				"method":"GET","tool":"execute_read","parameters":[]}`,
			text: []string{"Parameters: none"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cs := connectOffline(t)
			tool := toolNamed(t, cs, "describe_operation")

			res := callTool(t, cs, "describe_operation", map[string]any{"operationId": tc.operationID})

			if res.IsError {
				t.Fatalf("describe_operation returned a tool error: %s", resultText(t, res))
			}
			assertMatchesOutputSchema(t, tool, res)
			got := asJSON(t, mustJSON(t, res.StructuredContent))
			want := asJSON(t, tc.structured)
			if w := want.(map[string]any); w["description"] == nil {
				// a long deprecation notice; its content is proven by the catalog tests
				w["description"] = got.(map[string]any)["description"]
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("structuredContent =\n%s\nwant\n%s", mustJSON(t, got), mustJSON(t, want))
			}
			text := resultText(t, res)
			for _, line := range tc.text {
				if !strings.Contains(text, line) {
					t.Errorf("text does not contain %q:\n%s", line, text)
				}
			}
		})
	}
}

// mustJSON encodes v as JSON.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func TestDescribeOperationAnswersAnUnknownExcludedOrHiddenOperationWithOperationNotFound(t *testing.T) {
	t.Parallel()
	for name, id := range map[string]string{
		"unknown":                           "trace_lst",
		"excluded (ADR-0004)":               "ingestion_batch",
		"a write operation, write mode off": "prompts_create",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := toolErrorOf(t, callTool(t, connectOffline(t), "describe_operation", map[string]any{"operationId": id})).Error

			if got.Code != "operation_not_found" || got.OperationID != id || !strings.Contains(got.Hint, "search_operations") {
				t.Fatalf("error = %+v, want operation_not_found for %s with a hint naming search_operations", got, id)
			}
		})
	}
}

func TestDescribeOperationInWriteModeDescribesAWriteOperation(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t, server.WithWriteMode())

	res := callTool(t, cs, "describe_operation", map[string]any{"operationId": "prompts_delete"})

	if res.IsError {
		t.Fatalf("describe_operation returned a tool error: %s", resultText(t, res))
	}
	var got struct {
		Method, Tool string
	}
	if err := json.Unmarshal([]byte(mustJSON(t, res.StructuredContent)), &got); err != nil ||
		got.Method != "DELETE" || got.Tool != "execute_write" {
		t.Fatalf("structuredContent = %s, want method DELETE run by execute_write", mustJSON(t, res.StructuredContent))
	}
}

func TestDescribeOperationBoundsAndRedactsTheEchoedOperationID(t *testing.T) {
	t.Parallel()
	for name, id := range map[string]string{
		"a long ID": strings.Repeat("a", 128),
		"a secret":  testSecretKey,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := callTool(t, connectOffline(t), "describe_operation", map[string]any{"operationId": id})

			got := toolErrorOf(t, res).Error
			text := resultText(t, res)
			if got.Code != "operation_not_found" || strings.Contains(text, id) || len(got.OperationID) > 70 {
				t.Fatalf("error = %+v, want operation_not_found echoing the ID only bounded and redacted", got)
			}
		})
	}
}

func TestDescribeOperationRefusesInvalidArgumentsBeforeAnyWork(t *testing.T) {
	t.Parallel()
	tests := map[string]map[string]any{
		"a missing operationId":            {},
		"an empty operationId":             {"operationId": ""},
		"an operationId longer than 128":   {"operationId": strings.Repeat("a", 129)},
		"a control character":              {"operationId": "trace_list\u0000"},
		"an invisible character":           {"operationId": "trace\u200b_list"},
		"an operationId that is no string": {"operationId": 7},
		"an unknown argument":              {"operationId": "trace_list", "parameters": map[string]any{}},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := toolErrorOf(t, callTool(t, connectOffline(t), "describe_operation", args)).Error

			if got.Code != "invalid_argument" || got.Hint == "" {
				t.Fatalf("error = %+v, want invalid_argument with a hint", got)
			}
		})
	}
}

// ADR-0002 amendment: a refused parameter lists the operation's valid
// parameter names and names describe_operation, so one call fixes it.
func TestExecuteReadRefusingAParameterListsTheValidNamesAndNamesDescribeOperation(t *testing.T) {
	t.Parallel()
	const validNames = "queueId, status, page, limit"
	tests := map[string]struct {
		params   map[string]any
		unechoed string
	}{
		"an unknown parameter":        {params: map[string]any{"queueId": "q-1", "bogusParam": "x"}},
		"a value outside the enum":    {params: map[string]any{"queueId": "q-1", "status": "BOGUSVALUE"}, unechoed: "BOGUSVALUE"},
		"a value of the wrong type":   {params: map[string]any{"queueId": "q-1", "page": "BOGUSVALUE"}, unechoed: "BOGUSVALUE"},
		"a limit outside its bounds":  {params: map[string]any{"queueId": "q-1", "limit": 987654}, unechoed: "987654"},
		"a missing required path one": {params: map[string]any{}},
		"an unsafe path value":        {params: map[string]any{"queueId": "../BOGUSVALUE"}, unechoed: "BOGUSVALUE"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
			res := callExecuteRead(t, connect(t, fake), map[string]any{
				"operationId": "annotationQueues_listQueueItems", "parameters": tc.params,
			})

			got := toolErrorOf(t, res).Error
			if got.Code != "invalid_argument" || !strings.Contains(got.Hint, validNames) ||
				!strings.Contains(got.Hint, "describe_operation") || strings.Contains(got.Hint, "http") {
				t.Fatalf("error = %+v, want invalid_argument whose hint lists %q, names describe_operation and links no web page (#36)",
					got, validNames)
			}
			if tc.unechoed != "" && strings.Contains(resultText(t, res), tc.unechoed) {
				t.Errorf("tool error echoes the parameter value %q:\n%s", tc.unechoed, resultText(t, res))
			}
			assertNoRequest(t, seen)
		})
	}
}
