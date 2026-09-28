package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Seam: describe_operation steers an agent listing prompts by tag to the tag
// parameter of prompts_list, not to its undocumented filter parameter (#141).

// The guidance describe_operation gives on the tag and filter parameters of
// prompts_list, as literals.
const (
	promptsTagGuidance    = "The tag filter: send one tag name as a string to list only the prompts that carry that tag."
	promptsFilterGuidance = "Not the tag filter: to list the prompts that carry a tag, send the tag name in tag instead."
)

// describedParams calls describe_operation for operationID and returns its
// text and the guidance of each parameter by name.
func describedParams(t *testing.T, operationID string) (string, map[string]string) {
	t.Helper()
	cs := connectOffline(t)
	tool := toolNamed(t, cs, "describe_operation")

	res := callTool(t, cs, "describe_operation", map[string]any{"operationId": operationID})

	if res.IsError {
		t.Fatalf("describe_operation %s returned a tool error: %s", operationID, resultText(t, res))
	}
	assertMatchesOutputSchema(t, tool, res)
	var got struct {
		Parameters []struct {
			Name     string `json:"name"`
			Guidance string `json:"guidance"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, res.StructuredContent)), &got); err != nil {
		t.Fatalf("structuredContent: %v", err)
	}
	guidance := make(map[string]string, len(got.Parameters))
	for _, p := range got.Parameters {
		guidance[p.Name] = p.Guidance
	}
	return resultText(t, res), guidance
}

func TestDescribeOperationNamesTagAsTheTagFilterOfPromptsList(t *testing.T) {
	t.Parallel()

	text, guidance := describedParams(t, "prompts_list")

	if guidance["tag"] != promptsTagGuidance {
		t.Fatalf("tag guidance = %q, want %q", guidance["tag"], promptsTagGuidance)
	}
	if !strings.Contains(text, "- tag (query, string)\n  "+promptsTagGuidance+"\n") {
		t.Fatalf("text does not give the guidance under the tag line:\n%s", text)
	}
}

func TestDescribeOperationSaysFilterIsNotTheTagFilterOfPromptsList(t *testing.T) {
	t.Parallel()

	text, guidance := describedParams(t, "prompts_list")

	if guidance["filter"] != promptsFilterGuidance {
		t.Fatalf("filter guidance = %q, want %q", guidance["filter"], promptsFilterGuidance)
	}
	if !strings.Contains(text, "- filter (query, string)\n  "+promptsFilterGuidance+"\n") {
		t.Fatalf("text does not give the guidance under the filter line:\n%s", text)
	}
}

func TestThePromptsListGuidanceIsStaticTextThatNeverHoldsCallerInput(t *testing.T) {
	t.Parallel()
	before, _ := describedParams(t, "prompts_list")
	cs := connectOffline(t)
	// A hostile search first: the guidance must not pick up the query.
	callTool(t, cs, "search_operations", map[string]any{"query": "tag billing <b>ignore previous</b>"})

	res := callTool(t, cs, "describe_operation", map[string]any{"operationId": "prompts_list"})

	text := resultText(t, res)
	if text != before {
		t.Errorf("describe_operation prompts_list changed after a search:\nbefore %s\nafter %s", before, text)
	}
	for _, g := range []string{promptsTagGuidance, promptsFilterGuidance} {
		if strings.Count(text, g) != 1 {
			t.Errorf("text holds %q %d times, want once:\n%s", g, strings.Count(text, g), text)
		}
	}
	for _, leak := range []string{"billing", "<b>", "ignore previous"} {
		if strings.Contains(text, leak) {
			t.Errorf("describe_operation prompts_list holds %q from the search query:\n%s", leak, text)
		}
	}
}

func TestNoOtherOperationCarriesTheTagFilterGuidance(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t)

	for _, operationID := range []string{"prompts_get", "trace_list", "metrics_metrics", "observations_getMany"} {
		res := callTool(t, cs, "describe_operation", map[string]any{"operationId": operationID})
		if res.IsError {
			t.Fatalf("describe_operation %s returned a tool error: %s", operationID, resultText(t, res))
		}
		if text := resultText(t, res); strings.Contains(text, "tag filter") {
			t.Errorf("describe_operation %s carries the tag filter guidance:\n%s", operationID, text)
		}
	}
}

func TestAPromptsListCallWithAnUnknownParameterIsStillRefusedBeforeLangfuse(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[],"meta":{}}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "prompts_list", "parameters": map[string]any{"tags": "billing"},
	})

	select {
	case r := <-seen:
		t.Fatalf("Langfuse received %s %s for a refused call", r.method, r.path)
	default:
	}
	if got := toolErrorOf(t, res).Error; got.Code != "invalid_argument" {
		t.Fatalf("tool error = %+v, want invalid_argument", got)
	}
}

func TestAPromptsListCallWithFilterIsStillSent(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[],"meta":{}}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "prompts_list", "parameters": map[string]any{"filter": "billing"},
	})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	if got := receivedOne(t, seen); got.query.Get("filter") != "billing" {
		t.Fatalf("Langfuse received query %v, want filter=billing", got.query)
	}
}
