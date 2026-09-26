package server_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam S1 (spec #68): the discovery tools, driven through the in-memory MCP
// client. They never call Langfuse, so the server gets no Langfuse client at
// all: a call to Langfuse would panic and surface as internal_error.

// connectOffline starts the server without a Langfuse client.
func connectOffline(t testing.TB, opts ...server.Option) *mcp.ClientSession {
	t.Helper()
	return connectServer(t, nil, slog.New(slog.DiscardHandler), server.Secrets{Keys: testKeys()}, opts...)
}

// toolNamed returns the listed tool with the given name.
func toolNamed(t *testing.T, cs *mcp.ClientSession, name string) *mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s is not listed", name)
	return nil
}

// callTool calls the named tool with args.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

// assertMatchesOutputSchema fails the test when the result's
// structuredContent does not validate against the tool's outputSchema.
func assertMatchesOutputSchema(t *testing.T, tool *mcp.Tool, res *mcp.CallToolResult) {
	t.Helper()
	raw, err := json.Marshal(tool.OutputSchema)
	if err != nil || tool.OutputSchema == nil {
		t.Fatalf("tool %s declares no usable outputSchema (%v)", tool.Name, err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("outputSchema of %s: %v", tool.Name, err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve outputSchema of %s: %v", tool.Name, err)
	}
	if err := resolved.Validate(res.StructuredContent); err != nil {
		t.Fatalf("structuredContent does not match the outputSchema of %s: %v", tool.Name, err)
	}
}

// operationIndex is the structuredContent of search_operations.
type operationIndex struct {
	Count  int `json:"count"`
	Groups []struct {
		Tag        string `json:"tag"`
		Operations []struct {
			OperationID string `json:"operationId"`
			Description string `json:"description"`
			Tool        string `json:"tool"`
		} `json:"operations"`
	} `json:"groups"`
	Tags []string `json:"tags"`
}

func operationIndexOf(t *testing.T, res *mcp.CallToolResult) operationIndex {
	t.Helper()
	if res.IsError {
		t.Fatalf("search_operations returned a tool error: %s", resultText(t, res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	var idx operationIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		t.Fatalf("structuredContent is not an operation index: %v", err)
	}
	return idx
}

// listed returns "tag/operationId@tool" for every operation of the index.
func (idx operationIndex) listed() []string {
	var out []string
	for _, g := range idx.Groups {
		for _, op := range g.Operations {
			out = append(out, g.Tag+"/"+op.OperationID+"@"+op.Tool)
		}
	}
	return out
}

func TestSearchOperationsIsAnnotatedReadOnlyAndClosedWorld(t *testing.T) {
	t.Parallel()
	tool := toolNamed(t, connectOffline(t), "search_operations")

	a := tool.Annotations
	if a == nil || !a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || !a.IdempotentHint ||
		a.OpenWorldHint == nil || *a.OpenWorldHint || a.Title == "" || tool.Title == "" {
		t.Fatalf("annotations = %+v, want title, readOnly, idempotent, not destructive, closed-world, all explicit", a)
	}
}

func TestSearchOperationsWithoutQueryListsEveryReadOperationGroupedByTag(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t)
	tool := toolNamed(t, cs, "search_operations")

	res := callTool(t, cs, "search_operations", map[string]any{})

	assertMatchesOutputSchema(t, tool, res)
	idx := operationIndexOf(t, res)
	// The union catalog has 68 GET operations and no excluded one among them.
	if idx.Count != 68 || len(idx.listed()) != 68 {
		t.Fatalf("index lists %d operations (count %d), want the 68 read operations", len(idx.listed()), idx.Count)
	}
	// The tags holding a v4-family operation come first (ADR-0012 §5).
	if idx.Groups[0].Tag != "Metrics" || idx.Groups[0].Operations[0].OperationID != "metrics_metrics" ||
		idx.Groups[2].Tag != "AnnotationQueues" || idx.Groups[2].Operations[0].OperationID != "annotationQueues_getQueue" {
		t.Errorf("groups = %+v, want Metrics starting with metrics_metrics, Observations, then AnnotationQueues starting with annotationQueues_getQueue",
			idx.Groups[:3])
	}
	text := resultText(t, res)
	for _, want := range []string{
		"Health\n- health_health — Check health of API and database\n",
		"Prompts\n- prompts_get — Get a prompt\n- prompts_list — Get a list of prompt names with versions and labels\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "prompts_create") || strings.Contains(text, "execute_write") {
		t.Errorf("text lists a write operation or the write tool while write mode is off:\n%s", text)
	}
}

// ADR-0012 §5, spec #68 story 7: the v4 family ranks before the legacy family.
func TestSearchOperationsRanksTheV4FamilyBeforeTheLegacyFamily(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t)

	idx := operationIndexOf(t, callTool(t, cs, "search_operations", map[string]any{"query": "metrics"}))

	got := idx.listed()
	want := []string{"Metrics/metrics_metrics@execute_read", "Metrics/metrics_daily@execute_read", "LegacyMetricsV1/legacy_metricsV1_metrics@execute_read"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("index = %v, want %v", got, want)
	}
}

// Spec #68 story 12, ADR-0002, #79: listing every read operation of the
// unresolved union catalog (68) stays within 7 KiB of text (6,991 bytes,
// ~1.8k tokens, when measured; ~9 KB while legacy lines carried the whole
// deprecation notice). A catalog regeneration that crosses the budget
// updates ADR-0002 and spec #68 with the new figure.
func TestSearchOperationsWithoutQueryListsTheReadIndexWithinItsBudget(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t)

	text := resultText(t, callTool(t, cs, "search_operations", map[string]any{}))

	if len(text) > 7*1024 {
		t.Errorf("read index text is %d bytes, want at most 7 KiB", len(text))
	}
}

// #79: a legacy read's line in the index says what it does and names its
// replacement; no line carries the deprecation notice or its link.
func TestSearchOperationsListsALegacyReadByWhatItDoesAndItsReplacement(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t)

	text := resultText(t, callTool(t, cs, "search_operations", map[string]any{"query": "trace"}))

	if want := "- trace_list — Get list of traces (legacy: prefer observations_getMany when it is available)\n"; !strings.Contains(text, want) ||
		strings.Contains(text, "Deprecated") || strings.Contains(text, "https://") {
		t.Errorf("text does not list trace_list by what it does and its replacement alone:\n%s", text)
	}
}

func TestSearchOperationsWithQueryKeepsOnlyTheOperationsMatchingEveryTerm(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t)

	idx := operationIndexOf(t, callTool(t, cs, "search_operations", map[string]any{"query": "Prompt GET"}))

	got, want := idx.listed(), []string{"Prompts/prompts_get@execute_read", "Prompts/prompts_list@execute_read"}
	if strings.Join(got, ",") != strings.Join(want, ",") || idx.Count != 2 {
		t.Fatalf("index = %v (count %d), want %v", got, idx.Count, want)
	}
}

func TestSearchOperationsThatMatchesNothingNamesTheTagsWithoutEchoingTheQuery(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t)
	tool := toolNamed(t, cs, "search_operations")
	const query = "zqxj-nothing-matches"

	res := callTool(t, cs, "search_operations", map[string]any{"query": query})

	assertMatchesOutputSchema(t, tool, res)
	idx := operationIndexOf(t, res)
	if idx.Count != 0 || len(idx.Groups) != 0 {
		t.Fatalf("index = %+v, want no operation", idx)
	}
	text := resultText(t, res)
	for _, want := range []string{"No operation matches", "Trace", "Prompts", "Health", "without query"} {
		if !strings.Contains(text, want) {
			t.Errorf("text does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "zqxj") || strings.Contains(strings.Join(idx.Tags, ","), "zqxj") {
		t.Errorf("result echoes the query:\n%s", text)
	}
	if strings.Contains(strings.Join(idx.Tags, ","), "PromptVersion") {
		t.Errorf("tags = %v, want only tags of listed operations (PromptVersion has only a write operation)", idx.Tags)
	}
}

func TestSearchOperationsInWriteModeListsWriteOperationsNamingTheToolThatRunsThem(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t, server.WithWriteMode())

	res := callTool(t, cs, "search_operations", map[string]any{"query": "prompts"})

	got := strings.Join(operationIndexOf(t, res).listed(), ",")
	want := "Prompts/prompts_create@execute_write,Prompts/prompts_delete@execute_write," +
		"Prompts/prompts_get@execute_read,Prompts/prompts_list@execute_read"
	if got != want {
		t.Fatalf("index = %s, want %s", got, want)
	}
	text := resultText(t, res)
	for _, want := range []string{
		"- prompts_create — Create a new version for the prompt with the given `name` (execute_write)\n",
		"- prompts_get — Get a prompt (execute_read)\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text does not contain %q:\n%s", want, text)
		}
	}
}

func TestSearchOperationsRefusesAnInvalidQueryWithoutEchoingIt(t *testing.T) {
	t.Parallel()
	tests := map[string]map[string]any{
		"a query longer than 128 runes": {"query": strings.Repeat("é", 129)},
		"a control character":           {"query": "trace\u0007list"},
		"a line feed":                   {"query": "trace\nlist"},
		"an invisible character":        {"query": "trace\u200blist"},
		"a bidi override":               {"query": "trace\u202elist"},
		"a tag character":               {"query": "trace\U000E0041list"},
		"a query that is not a string":  {"query": 42},
		"an unknown argument":           {"query": "trace", "operationId": "trace_list"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cs := connectOffline(t)

			res := callTool(t, cs, "search_operations", args)

			got := toolErrorOf(t, res).Error
			if got.Code != "invalid_argument" || got.Hint == "" {
				t.Fatalf("error = %+v, want invalid_argument with a hint", got)
			}
			if text := resultText(t, res); strings.Contains(text, "trace") || strings.Contains(text, "éé") {
				t.Errorf("tool error echoes the query:\n%s", text)
			}
		})
	}
}

func TestSearchOperationsAcceptsAQueryOfExactly128Runes(t *testing.T) {
	t.Parallel()
	cs := connectOffline(t)

	res := callTool(t, cs, "search_operations", map[string]any{"query": strings.Repeat("é", 128)})

	if res.IsError {
		t.Fatalf("a 128-rune query was refused: %s", resultText(t, res))
	}
}

func TestEveryDiscoveryCallLogsOneAuditLineWithoutTheQueryOrAPayload(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		tool     string
		args     map[string]any
		wantCode string
	}{
		"a search":         {tool: "search_operations", args: map[string]any{"query": "promptsecretquery"}},
		"a refused search": {tool: "search_operations", args: map[string]any{"query": "promptsecretquery\u200b"}, wantCode: "invalid_argument"},
		"a description":    {tool: "describe_operation", args: map[string]any{"operationId": "prompts_get"}},
		"an unknown operation description": {tool: "describe_operation", args: map[string]any{"operationId": "no_such_op"},
			wantCode: "operation_not_found"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var logs syncBuffer
			cs := connectServer(t, nil, slog.New(slog.NewJSONHandler(&logs, nil)), server.Secrets{Keys: testKeys()})

			callTool(t, cs, tc.tool, tc.args)

			lines := auditLines(t, &logs)
			if len(lines) != 1 || lines[0]["tool"] != tc.tool || lines[0]["code"] != tc.wantCode {
				t.Fatalf("audit lines = %v, want one for %s with code %q", lines, tc.tool, tc.wantCode)
			}
			if strings.Contains(logs.String(), "promptsecretquery") {
				t.Errorf("the audit line holds the query:\n%s", logs.String())
			}
		})
	}
}

// go.md: every parser of untrusted input is fuzzed. Whatever the query, the
// discovery tools answer with a result or an invalid_argument tool error —
// never an internal error — and never repeat a refused query.
func FuzzDiscoveryArguments(f *testing.F) {
	for _, seed := range []string{"", "prompt get", "trace\u200blist", "\u202e", strings.Repeat("é", 129), "a\x00b", "sk-lf-x"} {
		f.Add(seed)
	}
	cs := connectOffline(f)
	f.Fuzz(func(t *testing.T, s string) {
		for _, call := range []struct{ tool, arg string }{
			{"search_operations", "query"}, {"describe_operation", "operationId"},
		} {
			res := callTool(t, cs, call.tool, map[string]any{call.arg: s})
			if !res.IsError {
				continue
			}
			got := toolErrorOf(t, res).Error
			if got.Code != "invalid_argument" && got.Code != "operation_not_found" {
				t.Fatalf("%s(%q) = %+v, want a result, invalid_argument or operation_not_found", call.tool, s, got)
			}
			if call.tool == "search_operations" && len(s) >= 8 && strings.Contains(resultText(t, res), s) {
				t.Fatalf("search_operations echoes the refused query %q", s)
			}
		}
	})
}
