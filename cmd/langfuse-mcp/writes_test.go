package main

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// Spec #109, ticket #110: LANGFUSE_MCP_ALLOW_WRITES reaches the server, and
// the startup log states whether write mode is on.

func TestStartupLogStatesWhetherWriteModeIsOn(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		setting    string
		wantOn     bool
		wantSource string
	}{
		"unset":        {"LANGFUSE_MCP_ALLOW_WRITES=", false, "default"},
		"set to true":  {"LANGFUSE_MCP_ALLOW_WRITES=true", true, "environment"},
		"set to false": {"LANGFUSE_MCP_ALLOW_WRITES=false", false, "environment"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stderr, err := runExecutable(t, tc.setting, noLangfuseProxy)
			if err != nil {
				t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
			}

			for _, line := range logLines(t, stderr) {
				if line["msg"] != "write mode" {
					continue
				}
				if line["on"] != tc.wantOn || line["source"] != tc.wantSource {
					t.Fatalf("write mode line = %v, want on %v, source %q", line, tc.wantOn, tc.wantSource)
				}
				return
			}
			t.Fatalf("no \"write mode\" line in the startup log:\n%s", stderr)
		})
	}
}

func TestStartupFailsNamingAnInvalidWriteModeWithoutQuotingIt(t *testing.T) {
	t.Parallel()

	stderr, err := runExecutable(t, "LANGFUSE_MCP_ALLOW_WRITES=sk-lf-yes-please")

	if err == nil {
		t.Fatalf("executable exited 0 with an invalid LANGFUSE_MCP_ALLOW_WRITES; stderr:\n%s", stderr)
	}
	if !strings.Contains(string(stderr), "LANGFUSE_MCP_ALLOW_WRITES") || strings.Contains(string(stderr), "sk-lf-yes-please") {
		t.Fatalf("startup error, want it to name LANGFUSE_MCP_ALLOW_WRITES without the value:\n%s", stderr)
	}
}

func TestExecutableListsExecuteWriteOnlyInWriteMode(t *testing.T) {
	t.Parallel()
	for setting, want := range map[string]bool{
		"LANGFUSE_MCP_ALLOW_WRITES=true": true,
		"LANGFUSE_MCP_ALLOW_WRITES=":     false,
	} {
		t.Run(setting, func(t *testing.T) {
			t.Parallel()
			fake := deploymentLangfuse(t, `{"status":"OK","version":"4.46.0"}`, nil, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{}`) // no call reaches it in this test
			})
			s := startStdio(t, "LANGFUSE_BASE_URL="+fake.URL, setting)
			s.initialize()

			var list struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			}
			if err := json.Unmarshal(s.send("tools/list", map[string]any{}, true), &list); err != nil {
				t.Fatalf("decode tools/list: %v", err)
			}
			listed := slices.ContainsFunc(list.Tools, func(tool struct {
				Name string `json:"name"`
			}) bool {
				return tool.Name == "execute_write"
			})
			if listed != want {
				t.Fatalf("execute_write listed = %v with %s, want %v", listed, setting, want)
			}
			s.stop(func() {})
		})
	}
}
