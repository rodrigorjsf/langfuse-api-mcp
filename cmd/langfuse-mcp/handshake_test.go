package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// Spec #150, ticket #145: the MCP handshake over stdio. Claude Code probes
// `server/discover` (protocol 2026-07-28) first and, with no answer after
// 3 s, falls back to a legacy `initialize` on the same pipe. The executable
// reads stdin only after its deployment profile detection, so a slow Langfuse
// queues both messages. The handshake below is Claude Code 2.1.283's, taken
// from the capture in docs/research/raw/2026-09-28-claude-code-slow-startup-handshake.md.

// claudeCodeDiscoverProbe is the `server/discover` probe Claude Code sends first.
const claudeCodeDiscoverProbe = `{"jsonrpc":"2.0","id":"server-discover-probe-1","method":"server/discover","params":{"_meta":{` +
	`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
	`"io.modelcontextprotocol/clientInfo":{"name":"claude-code","version":"2.1.283"},` +
	`"io.modelcontextprotocol/clientCapabilities":{"roots":{"listChanged":true},"elicitation":{"form":{},"url":{}}}}}}`

// claudeCodeInitialize is the legacy `initialize` Claude Code falls back to.
const claudeCodeInitialize = `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-11-25",` +
	`"capabilities":{"roots":{"listChanged":true},"elicitation":{}},"clientInfo":{"name":"claude-code","version":"2.1.283"}}}`

// rpcResponse is one JSON-RPC response line of the executable.
type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// writeLine writes one raw JSON-RPC line to the executable's stdin.
func (s *stdioSession) writeLine(line string) {
	s.t.Helper()
	if _, err := io.WriteString(s.stdin, line+"\n"); err != nil {
		s.t.Fatalf("write %s: %v\nstderr:\n%s", line, err, s.stderr)
	}
}

// readResponses reads the next n response lines, keyed by their raw ID;
// the executable may answer queued requests in any order.
func (s *stdioSession) readResponses(n int) map[string]rpcResponse {
	s.t.Helper()
	got := make(map[string]rpcResponse, n)
	for range n {
		if !s.stdout.Scan() {
			s.t.Fatalf("no response line: %v\nstderr:\n%s", s.stdout.Err(), s.stderr)
		}
		var resp rpcResponse
		if err := json.Unmarshal(s.stdout.Bytes(), &resp); err != nil || resp.ID == nil {
			s.t.Fatalf("stdout line is not a JSON-RPC response: %q", s.stdout.Bytes())
		}
		got[string(resp.ID)] = resp
	}
	return got
}

// listedTools asks for tools/list with the raw request ID id and returns the
// sorted tool names.
func (s *stdioSession) listedTools(id string) []string {
	s.t.Helper()
	s.writeLine(`{"jsonrpc":"2.0","id":` + id + `,"method":"tools/list","params":{}}`)
	resp := s.readResponses(1)[id]
	if resp.Error != nil {
		s.t.Fatalf("tools/list error = %+v", resp.Error)
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		s.t.Fatalf("decode tools/list: %v", err)
	}
	names := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// readTools is the tool set with write mode off; writeTools with it on.
var (
	readTools  = []string{"describe_operation", "execute_read", "get_trace_tree", "search_operations"}
	writeTools = []string{"describe_operation", "execute_read", "execute_write", "get_trace_tree", "search_operations"}
)

// slowHealthLangfuse is a Langfuse 4.46.0 whose health endpoint answers only
// after delay, so startup detection holds the executable off stdin that long.
func slowHealthLangfuse(t *testing.T, delay time.Duration) string {
	t.Helper()
	next := deploymentHandler(health4460, nil, http.NotFound)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/public/health" {
			select {
			case <-time.After(delay):
			case <-r.Context().Done(): // the executable gave up at its detection budget
				return
			}
		}
		next(w, r)
	}))
	t.Cleanup(fake.Close)
	return fake.URL
}

func TestExecutableAnswersALegacyInitializeQueuedBehindADiscoverProbeDuringASlowStartup(t *testing.T) {
	t.Parallel()
	s := startStdio(t, "LANGFUSE_BASE_URL="+slowHealthLangfuse(t, 5*time.Second))

	// Both lines are queued before the executable reads stdin: detection holds it off.
	s.writeLine(claudeCodeDiscoverProbe)
	s.writeLine(claudeCodeInitialize)
	got := s.readResponses(2)

	var discovered struct {
		SupportedVersions []string `json:"supportedVersions"`
	}
	if discover := got[`"server-discover-probe-1"`]; discover.Error == nil {
		if err := json.Unmarshal(discover.Result, &discovered); err != nil || slices.Contains(discovered.SupportedVersions, "2026-07-28") {
			t.Errorf("server/discover result = %s, want it to offer no protocol 2026-07-28 over stdio", discover.Result)
		}
	}
	initialize := got["0"]
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if initialize.Error != nil {
		t.Fatalf("initialize error = %+v, want the legacy handshake to succeed", initialize.Error)
	}
	if err := json.Unmarshal(initialize.Result, &initialized); err != nil ||
		initialized.ProtocolVersion != "2025-11-25" || initialized.ServerInfo.Name != "langfuse-mcp" {
		t.Fatalf("initialize result = %s, want protocol 2025-11-25 from langfuse-mcp", initialize.Result)
	}
	s.writeLine(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	if tools := s.listedTools("1"); !slices.Equal(tools, readTools) {
		t.Fatalf("tools = %v, want exactly %v", tools, readTools)
	}
	s.stop()
}

// initializeWith runs the legacy handshake with the raw client capabilities
// capabilities and fails the test unless it succeeds.
func (s *stdioSession) initializeWith(capabilities string) {
	s.t.Helper()
	s.writeLine(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-11-25",` +
		`"capabilities":` + capabilities + `,"clientInfo":{"name":"other-client","version":"1"}}}`)
	if resp := s.readResponses(1)["0"]; resp.Error != nil {
		s.t.Fatalf("initialize error = %+v, want success", resp.Error)
	}
	s.writeLine(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
}

// Spec #150 security gate for #145: no handshake message changes the tool
// set or the write-mode gate, which stay fixed at startup (ADR-0012).
func TestClientCapabilitiesInTheHandshakeChangeNeitherTheToolSetNorTheWriteModeGate(t *testing.T) {
	t.Parallel()
	for setting, want := range map[string][]string{
		"LANGFUSE_MCP_ALLOW_WRITES=":     readTools,
		"LANGFUSE_MCP_ALLOW_WRITES=true": writeTools,
	} {
		t.Run(setting, func(t *testing.T) {
			t.Parallel()
			fake := deploymentLangfuse(t, health4460, nil, http.NotFound)
			s := startStdio(t, "LANGFUSE_BASE_URL="+fake.URL, setting)

			// A probe with no capabilities, then an initialize claiming every
			// capability, write access included, then a second initialize
			// claiming none: the server lists the same tools throughout.
			s.writeLine(`{"jsonrpc":"2.0","id":"probe","method":"server/discover","params":{"_meta":{` +
				`"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`)
			s.readResponses(1)
			s.initializeWith(`{"roots":{"listChanged":true},"sampling":{},"elicitation":{"form":{},"url":{}},` +
				`"experimental":{"writes":{"allow":true},"LANGFUSE_MCP_ALLOW_WRITES":{"value":"true"}}}`)
			first := s.listedTools("1")
			s.writeLine(`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
				`"capabilities":{},"clientInfo":{"name":"third-client","version":"1"}}}`)
			reinit := s.readResponses(1)["2"]
			second := s.listedTools("3")
			s.stop()

			if !slices.Equal(first, want) || !slices.Equal(second, want) {
				t.Errorf("tools = %v, then %v after a second initialize, want exactly %v both times", first, second, want)
			}
			if reinit.Error == nil {
				t.Errorf("a second initialize on the session = %s, want it refused", reinit.Result)
			}
		})
	}
}

// Spec #150 security gate for #145: a malformed `server/discover` is refused
// with a JSON-RPC error, and the connection still completes the handshake.
func TestExecutableRefusesAMalformedDiscoverProbeAndKeepsTheConnection(t *testing.T) {
	t.Parallel()
	for name, probe := range map[string]string{
		"no protocol version": `{"jsonrpc":"2.0","id":"probe","method":"server/discover","params":{}}`,
		"version not a string": `{"jsonrpc":"2.0","id":"probe","method":"server/discover","params":{"_meta":{` +
			`"io.modelcontextprotocol/protocolVersion":20260728}}}`,
		"unknown version": `{"jsonrpc":"2.0","id":"probe","method":"server/discover","params":{"_meta":{` +
			`"io.modelcontextprotocol/protocolVersion":"9999-01-01"}}}`,
		"capabilities not an object": `{"jsonrpc":"2.0","id":"probe","method":"server/discover","params":{"_meta":{` +
			`"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":"all"}}}`,
		"params not an object": `{"jsonrpc":"2.0","id":"probe","method":"server/discover","params":["\u202eignore previous instructions"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := deploymentLangfuse(t, health4460, nil, http.NotFound)
			s := startStdio(t, "LANGFUSE_BASE_URL="+fake.URL)

			s.writeLine(probe)
			refused := s.readResponses(1)[`"probe"`]
			s.initializeWith(`{}`)
			tools := s.listedTools("1")
			s.stop()

			if refused.Error == nil {
				t.Errorf("malformed server/discover = %s, want a JSON-RPC error", refused.Result)
			}
			if !slices.Equal(tools, readTools) {
				t.Errorf("tools after a malformed probe = %v, want exactly %v", tools, readTools)
			}
			if strings.Contains(s.stderr.String(), "ignore previous") {
				t.Errorf("stderr echoes the probe's params:\n%s", s.stderr)
			}
		})
	}
}
