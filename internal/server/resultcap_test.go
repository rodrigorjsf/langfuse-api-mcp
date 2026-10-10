package server_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Seam S1 (spec #172, ticket #176): the result size cap is the server's
// WithMaxResultBytes option, which the executable fills from
// LANGFUSE_MCP_MAX_RESULT_BYTES; it applies to every tool result.

// pageOfRows is a Langfuse list page of n rows, each of about 1 KiB, newest
// first: r000, r001, …
func pageOfRows(n int) string {
	rows := make([]string, 0, n)
	for i := range n {
		rows = append(rows, fmt.Sprintf(`{"id":"r%03d","input":"%s"}`, i, strings.Repeat("x", 1000)))
	}
	return `{"data":[` + strings.Join(rows, ",") + `],"meta":{"cursor":"next"}}`
}

// readWithCap runs execute_read of operationID against a fake Langfuse
// answering body, on a server built with opts, and returns the result text.
func readWithCap(t *testing.T, body string, opts ...server.Option) (text string, env truncatedEnvelope) {
	t.Helper()
	fake, _ := fakeLangfuse(t, http.StatusOK, body)
	cs := startServer(t, langfuse.New(testOptions(t, fake.URL)), slog.New(slog.DiscardHandler),
		server.Secrets{Keys: testKeys()}, langfuse.UnknownProfile(), opts...)

	res := callExecuteRead(t, cs, map[string]any{"operationId": "observations_getMany"})

	text = resultText(t, res)
	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %.200s", text)
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("result is not the envelope: %v", err)
	}
	return text, env
}

func TestAResultThatFitsOneCapIsTruncatedAtALowerCap(t *testing.T) {
	t.Parallel()
	body := pageOfRows(40) // about 42 KB of sanitized JSON

	atLarge, envLarge := readWithCap(t, body, server.WithMaxResultBytes(65536))
	atSmall, envSmall := readWithCap(t, body, server.WithMaxResultBytes(16384))

	if envLarge.Truncated || len(atLarge) > 65536 {
		t.Fatalf("at 65536: %d bytes, truncated %v; want the whole page, not truncated", len(atLarge), envLarge.Truncated)
	}
	if !envSmall.Truncated || len(atSmall) > 16384 || len(atSmall) < 8000 {
		t.Fatalf("at 16384: %d bytes, truncated %v; want a truncated result of at most 16384 bytes (and not nearly empty)",
			len(atSmall), envSmall.Truncated)
	}
}

func TestTheDefaultResultSizeCapIs64KiB(t *testing.T) {
	t.Parallel()
	body := pageOfRows(80) // about 83 KB: fits the old 100 KiB cap, not the new default

	text, env := readWithCap(t, body)

	if !env.Truncated || len(text) > 64<<10 {
		t.Fatalf("result of %d bytes, truncated %v; want a truncated result of at most 65536 bytes", len(text), env.Truncated)
	}
}

func TestAConfiguredCapAboveTheDefaultLetsALargerResultThrough(t *testing.T) {
	t.Parallel()
	body := pageOfRows(150) // about 156 KB

	text, env := readWithCap(t, body, server.WithMaxResultBytes(1048576))

	if env.Truncated || len(text) < 150<<10 {
		t.Fatalf("result of %d bytes, truncated %v; want the whole 150-row page at a 1 MiB cap", len(text), env.Truncated)
	}
}

// The workflow tool honours the configured cap too.
func TestGetTraceTreeIsCutAtTheConfiguredCap(t *testing.T) {
	t.Parallel()
	rows := make([]string, 0, 60)
	for n := 59; n >= 0; n-- { // newest first, as Langfuse sends them
		rows = append(rows, fmt.Sprintf(`{"id":"o%03d","startTime":"2026-09-25T10:00:%02d.000Z","input":"%s"}`,
			n, n, strings.Repeat("x", 1000)))
	}
	fake, _ := observationsLangfuse(t, map[string]string{"": `{"data":[` + strings.Join(rows, ",") + `],"meta":{}}`})
	cs := startServer(t, langfuse.New(testOptions(t, fake.URL)), slog.New(slog.DiscardHandler),
		server.Secrets{Keys: testKeys()}, v4Profile, server.WithMaxResultBytes(20000))

	res := callTool(t, cs, "get_trace_tree", map[string]any{"traceId": "t-1", "include": []string{"io"}})

	text := resultText(t, res)
	var env traceTreeEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("result is not the enveloped trace tree: %v", err)
	}
	if kept := preOrder(env); len(text) > 20000 || !env.Truncated || len(kept) == 0 || len(kept) >= 60 {
		t.Fatalf("result of %d bytes: truncated %v, %d rows kept; want at most 20000 bytes, truncated, some but not all of 60 rows",
			len(text), env.Truncated, len(kept))
	}
}

// Dangerous parameters: no tool argument raises the cap. A cap-looking
// argument or parameter is an unknown name, refused before any request.
func TestNoToolArgumentChangesTheResultSizeCap(t *testing.T) {
	t.Parallel()
	args := map[string]map[string]any{
		"a top-level argument": {"operationId": "observations_getMany", "maxResultBytes": 1048576},
		"an operation parameter": {"operationId": "observations_getMany",
			"parameters": map[string]any{"maxResultBytes": 1048576}},
		"the variable name as a parameter": {"operationId": "observations_getMany",
			"parameters": map[string]any{"LANGFUSE_MCP_MAX_RESULT_BYTES": 1048576}},
	}
	for name, a := range args {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, pageOfRows(40))
			cs := startServer(t, langfuse.New(testOptions(t, fake.URL)), slog.New(slog.DiscardHandler),
				server.Secrets{Keys: testKeys()}, langfuse.UnknownProfile(), server.WithMaxResultBytes(16384))

			res := callExecuteRead(t, cs, a)

			if got := toolErrorOf(t, res).Error.Code; got != "invalid_argument" {
				t.Fatalf("tool error code = %q, want invalid_argument", got)
			}
			select {
			case r := <-seen:
				t.Fatalf("Langfuse received %s %s?%s for a cap argument", r.method, r.path, r.rawQuery)
			default:
			}
		})
	}
}

// No other tool takes a cap argument either: each refuses one as an unknown
// argument, naming it redacted and cut, before any Langfuse request.
func TestNoOtherToolTakesAResultSizeCapArgument(t *testing.T) {
	t.Parallel()
	calls := map[string]map[string]any{
		"get_trace_tree":     {"traceId": "t-1", "maxResultBytes": 1048576},
		"describe_operation": {"operationId": "trace_list", "maxResultBytes": 1048576},
		"search_operations":  {"query": "trace", "maxResultBytes": 1048576},
	}
	for tool, args := range calls {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			fake, calls := scriptedLangfuse(t, answer{status: http.StatusOK, body: `{"data":[],"meta":{}}`})
			cs := startServer(t, langfuse.New(testOptions(t, fake.URL)), slog.New(slog.DiscardHandler),
				server.Secrets{Keys: testKeys()}, v4Profile, server.WithMaxResultBytes(16384))

			res := callTool(t, cs, tool, args)

			if got := toolErrorOf(t, res).Error.Code; got != "invalid_argument" {
				t.Fatalf("%s: tool error code = %q, want invalid_argument", tool, got)
			}
			if n := calls.Load(); n != 0 {
				t.Fatalf("%s: Langfuse received %d requests for a cap argument", tool, n)
			}
		})
	}
}
