package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Seam S2 (spec #17): the executable over stdio, driven by raw JSON-RPC lines
// exactly as an MCP host sends them, against an httptest Langfuse.

const (
	stdioPublicKey = "pk-lf-stdio-public"
	stdioSecretKey = "sk-lf-stdio-secret" //nolint:gosec // G101: a fake key for the fake Langfuse
)

// stdioSession is a running executable talking MCP over its stdin/stdout.
type stdioSession struct {
	t      *testing.T
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	stderr *bytes.Buffer
	cmd    *exec.Cmd
	nextID int
}

// startStdio starts the executable as a child with the extra environment
// entries (later entries win) and returns the session.
func startStdio(t *testing.T, env ...string) *stdioSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	configEnv, _ := userConfigLocation(t)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^$") //nolint:gosec // G204: exe is this test binary, not external input
	cmd.Env = append(append(append(os.Environ(), runMainEnv+"=1"), hermeticEnv...), connectionEnv...)
	cmd.Env = append(append(cmd.Env, configEnv...), env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	s := &stdioSession{t: t, stdin: stdin, stdout: bufio.NewScanner(stdout), stderr: &bytes.Buffer{}, cmd: cmd}
	s.stdout.Buffer(make([]byte, 0, 64<<10), 8<<20)
	cmd.Stderr = s.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start executable: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) // no-op once the child exited
	return s
}

// send writes one JSON-RPC message; a request (with an ID) returns its
// response's result, failing the test on a JSON-RPC error.
func (s *stdioSession) send(method string, params any, request bool) json.RawMessage {
	s.t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if request {
		s.nextID++
		msg["id"] = s.nextID
	}
	line, err := json.Marshal(msg)
	if err != nil {
		s.t.Fatalf("encode %s: %v", method, err)
	}
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		s.t.Fatalf("write %s: %v\nstderr:\n%s", method, err, s.stderr)
	}
	if !request {
		return nil
	}
	if !s.stdout.Scan() {
		s.t.Fatalf("no response to %s: %v\nstderr:\n%s", method, s.stdout.Err(), s.stderr)
	}
	// Every stdout line must be a JSON-RPC message: stdout carries only the protocol.
	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(s.stdout.Bytes(), &resp); err != nil || resp.JSONRPC != "2.0" {
		s.t.Fatalf("stdout line is not a JSON-RPC message: %q", s.stdout.Bytes())
	}
	if resp.ID != s.nextID || resp.Error != nil {
		s.t.Fatalf("response to %s = %s, want the result of request %d", method, s.stdout.Bytes(), s.nextID)
	}
	return resp.Result
}

func TestExecutableServesExecuteReadOverStdio(t *testing.T) {
	t.Parallel()
	gotAuth := make(chan string, 1)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, _ := r.BasicAuth()
		gotAuth <- r.Method + " " + r.URL.Path + " " + user + ":" + password
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"trace-1","name":"checkout"}`) // a failed write fails the call below
	}))
	t.Cleanup(fake.Close)
	s := startStdio(t, "LANGFUSE_BASE_URL="+fake.URL)

	s.send("initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "s2-test", "version": "0"},
	}, true)
	s.send("notifications/initialized", map[string]any{}, false)

	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(s.send("tools/list", map[string]any{}, true), &list); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "execute_read" {
		t.Fatalf("tools = %+v, want exactly execute_read", list.Tools)
	}

	var call struct {
		IsError           bool           `json:"isError"`
		StructuredContent map[string]any `json:"structuredContent"`
	}
	if err := json.Unmarshal(s.send("tools/call", map[string]any{
		"name": "execute_read", "arguments": map[string]any{
			"operationId": "trace_get", "parameters": map[string]any{"traceId": "trace-1"},
		},
	}, true), &call); err != nil {
		t.Fatalf("decode tools/call: %v", err)
	}
	if call.IsError || call.StructuredContent["operationId"] != "trace_get" {
		t.Fatalf("tools/call result = %+v, want the enveloped trace", call)
	}
	if got, want := <-gotAuth, "GET /api/public/traces/trace-1 "+stdioPublicKey+":"+stdioSecretKey; got != want {
		t.Fatalf("Langfuse received %q, want %q", got, want)
	}

	// Closing stdin ends the session: the executable exits cleanly and wrote
	// nothing else to stdout.
	_ = s.stdin.Close() // the exit status below is what matters
	if s.stdout.Scan() {
		t.Fatalf("unexpected stdout line after the last response: %q", s.stdout.Bytes())
	}
	if err := s.cmd.Wait(); err != nil {
		t.Fatalf("executable did not exit 0 after stdin closed: %v\nstderr:\n%s", err, s.stderr)
	}
	if strings.Contains(s.stderr.String(), stdioSecretKey) {
		t.Fatalf("stderr leaks the secret key:\n%s", s.stderr)
	}
}

func TestStartupFailsNamingTheMissingOrInvalidConnectionVariable(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		env  []string
		want string
	}{
		"host":         {env: []string{"LANGFUSE_BASE_URL=", "LANGFUSE_HOST="}, want: "LANGFUSE_BASE_URL"},
		"public key":   {env: []string{"LANGFUSE_PUBLIC_KEY="}, want: "LANGFUSE_PUBLIC_KEY"},
		"secret key":   {env: []string{"LANGFUSE_SECRET_KEY="}, want: "LANGFUSE_SECRET_KEY"},
		"invalid host": {env: []string{"LANGFUSE_BASE_URL=cloud.langfuse.com"}, want: "LANGFUSE_BASE_URL"},
		"plain http to a host that is not loopback": {
			env: []string{"LANGFUSE_BASE_URL=http://langfuse.internal.example.com"}, want: "LANGFUSE_BASE_URL: want an https URL",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configEnv, _ := userConfigLocation(t)

			stdout, stderr, err := runChildOutput(t, append(configEnv, tc.env...))

			if err == nil {
				t.Fatalf("executable exited 0 without %s; stderr:\n%s", tc.want, stderr)
			}
			if msg := startupError(t, stderr); !strings.Contains(msg, tc.want) {
				t.Fatalf("startup error %q does not name %s", msg, tc.want)
			}
			if len(stdout) != 0 {
				t.Fatalf("a failed startup wrote to stdout: %q", stdout)
			}
		})
	}
}
