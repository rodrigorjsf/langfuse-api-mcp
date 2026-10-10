package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Ticket #176 (spec #172): LANGFUSE_MCP_MAX_RESULT_BYTES reaches the server,
// and the startup log states the effective cap.

func TestStartupLogStatesTheEffectiveResultSizeCap(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		env        []string
		configFile string
		wantBytes  float64
		wantSource string
	}{
		"unset":            {nil, "", 65536, "default"},
		"environment":      {[]string{"LANGFUSE_MCP_MAX_RESULT_BYTES=200000"}, "", 200000, "environment"},
		"config file":      {nil, "LANGFUSE_MCP_MAX_RESULT_BYTES=300000\n", 300000, "config-file"},
		"environment wins": {[]string{"LANGFUSE_MCP_MAX_RESULT_BYTES=100000"}, "LANGFUSE_MCP_MAX_RESULT_BYTES=300000\n", 100000, "environment"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := append(tc.env, noLangfuseProxy)

			var stderr []byte
			var err error
			if tc.configFile == "" {
				stderr, err = runExecutable(t, env...)
			} else {
				stderr, err = runExecutableWithConfigFile(t, tc.configFile, env...)
			}
			if err != nil {
				t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
			}

			for _, line := range logLines(t, stderr) {
				if line["msg"] != "result size cap" {
					continue
				}
				if line["bytes"] != tc.wantBytes || line["source"] != tc.wantSource {
					t.Fatalf("result size cap line = %v, want bytes %v, source %q", line, tc.wantBytes, tc.wantSource)
				}
				return
			}
			t.Fatalf("no \"result size cap\" line in the startup log:\n%s", stderr)
		})
	}
}

// Dangerous parameters, on the built executable: an invalid cap stops startup
// with one error naming the variable and its source, never the value.
func TestStartupFailsNamingAnInvalidResultSizeCapWithoutQuotingIt(t *testing.T) {
	t.Parallel()
	const keyLike = "sk-lf-6d5c4b3a-2f1e-4d0c-8b9a-7f6e5d4c3b2a"
	for _, value := range []string{keyLike, "-65536", "64KiB", "1048577", "16383", "0", "65536.5"} {
		t.Run("environment/"+value, func(t *testing.T) {
			t.Parallel()

			stderr, err := runExecutable(t, "LANGFUSE_MCP_MAX_RESULT_BYTES="+value, noLangfuseProxy)

			if err == nil {
				t.Fatalf("executable exited 0 with LANGFUSE_MCP_MAX_RESULT_BYTES=%s; stderr:\n%s", value, stderr)
			}
			msg := startupError(t, stderr)
			if !strings.Contains(msg, "LANGFUSE_MCP_MAX_RESULT_BYTES") || !strings.Contains(msg, "environment") {
				t.Fatalf("startup error %q does not name the variable and the environment", msg)
			}
			if len(value) >= 5 && strings.Contains(string(stderr), value) {
				t.Fatalf("stderr quotes the value:\n%s", stderr)
			}
		})
		t.Run("config file/"+value, func(t *testing.T) {
			t.Parallel()

			stderr, err := runExecutableWithConfigFile(t, "LANGFUSE_MCP_MAX_RESULT_BYTES="+value+"\n", noLangfuseProxy)

			if err == nil {
				t.Fatalf("executable exited 0 with LANGFUSE_MCP_MAX_RESULT_BYTES=%s in the config file; stderr:\n%s", value, stderr)
			}
			msg := startupError(t, stderr)
			if !strings.Contains(msg, "LANGFUSE_MCP_MAX_RESULT_BYTES") || !strings.Contains(msg, "config.env") {
				t.Fatalf("startup error %q does not name the variable and the config file", msg)
			}
			if len(value) >= 5 && strings.Contains(string(stderr), value) {
				t.Fatalf("stderr quotes the value:\n%s", stderr)
			}
		})
	}
}

// The configured cap reaches the tool results: the same 40 KB page is whole
// at the default cap and cut at a 16384 cap.
func TestExecutableCutsAToolResultAtTheConfiguredResultSizeCap(t *testing.T) {
	t.Parallel()
	rows := make([]string, 0, 40)
	for i := range 40 {
		rows = append(rows, fmt.Sprintf(`{"id":"r%03d","input":"%s"}`, i, strings.Repeat("x", 1000)))
	}
	page := `{"data":[` + strings.Join(rows, ",") + `],"meta":{"cursor":"next"}}`
	for name, tc := range map[string]struct {
		env           []string
		wantLimit     int
		wantTruncated bool
	}{
		"default cap":   {nil, 65536, false},
		"cap of 16384":  {[]string{"LANGFUSE_MCP_MAX_RESULT_BYTES=16384"}, 16384, true},
		"cap of 100000": {[]string{"LANGFUSE_MCP_MAX_RESULT_BYTES=100000"}, 100000, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := deploymentLangfuse(t, health4460, nil, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, page) // a failed write fails the call below
			})
			s := startStdio(t, append(tc.env, "LANGFUSE_BASE_URL="+fake.URL)...)
			s.initialize()

			var call toolCall
			if err := json.Unmarshal(s.send("tools/call", map[string]any{
				"name": "execute_read", "arguments": map[string]any{"operationId": "observations_getMany"},
			}, true), &call); err != nil {
				t.Fatalf("decode tools/call: %v", err)
			}
			s.stop()

			if len(call.Content) != 1 || call.IsError {
				t.Fatalf("tools/call result = %+v, want one enveloped result", call)
			}
			truncated, _ := call.StructuredContent["truncated"].(bool)
			if truncated != tc.wantTruncated || len(call.Content[0].Text) > tc.wantLimit {
				t.Fatalf("result of %d bytes, truncated %v; want truncated %v within %d bytes",
					len(call.Content[0].Text), truncated, tc.wantTruncated, tc.wantLimit)
			}
		})
	}
}
