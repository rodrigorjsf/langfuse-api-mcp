package main

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// loggedSources returns the "sources" of the "CA sources loaded" startup log line.
func loggedSources(t *testing.T, stderr []byte) []any {
	t.Helper()
	for _, line := range logLines(t, stderr) {
		if line["msg"] == "CA sources loaded" {
			sources, _ := line["sources"].([]any)
			return sources
		}
	}
	t.Fatalf("no \"CA sources loaded\" line in the startup log:\n%s", stderr)
	return nil
}

func TestStartupLoadsEachAmbientVariableAsAnAmbientSource(t *testing.T) {
	t.Parallel()
	for _, variable := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
		t.Run(variable, func(t *testing.T) {
			t.Parallel()
			path := writeCA(t, 1)
			if variable == "SSL_CERT_DIR" {
				path = filepath.Dir(path)
			}

			stderr, err := runExecutable(t, variable+"="+path)
			if err != nil {
				t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
			}

			want := []any{map[string]any{
				"variable": variable, "path": path, "kind": "ambient", "origin": "environment",
				"certificates": float64(1),
			}}
			if got := loggedSources(t, stderr); !reflect.DeepEqual(got, want) {
				t.Fatalf("logged sources = %v, want %v", got, want)
			}
		})
	}
}

// Spec #7 failure policy: "Config-file-provided CA paths count as explicit"
// (#29), including the five ambient variables.
func TestStartupLoadsEachAmbientVariableSetInTheConfigFileAsAnExplicitSource(t *testing.T) {
	t.Parallel()
	for _, variable := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
		t.Run(variable, func(t *testing.T) {
			t.Parallel()
			path := writeCA(t, 1)
			if variable == "SSL_CERT_DIR" {
				path = filepath.Dir(path)
			}

			stderr, err := runExecutableWithConfigFile(t, variable+"="+path+"\n")
			if err != nil {
				t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
			}

			want := []any{map[string]any{
				"variable": variable, "path": path, "kind": "explicit", "origin": "config-file",
				"certificates": float64(1),
			}}
			if got := loggedSources(t, stderr); !reflect.DeepEqual(got, want) {
				t.Fatalf("logged sources = %v, want %v", got, want)
			}
		})
	}
}

func TestStartupLetsAnAmbientVariableInTheEnvironmentOverrideTheConfigFile(t *testing.T) {
	t.Parallel()
	for _, variable := range []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS"} { // one removed at capture, one only read
		t.Run(variable, func(t *testing.T) {
			t.Parallel()
			fromEnv, fromFile := writeCA(t, 1), writeCA(t, 2)

			stderr, err := runExecutableWithConfigFile(t, variable+"="+fromFile+"\n", variable+"="+fromEnv)
			if err != nil {
				t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
			}

			want := []any{map[string]any{
				"variable": variable, "path": fromEnv, "kind": "ambient", "origin": "environment",
				"certificates": float64(1),
			}}
			if got := loggedSources(t, stderr); !reflect.DeepEqual(got, want) {
				t.Fatalf("logged sources = %v, want %v", got, want)
			}
		})
	}
}

func TestStartupKeepsAmbientVariablesInTheConfigFileWhenTheIgnoreFlagIsSet(t *testing.T) {
	t.Parallel()
	path := writeCA(t, 1)

	stderr, err := runExecutableWithConfigFile(t, "NODE_EXTRA_CA_CERTS="+path+"\nLANGFUSE_MCP_IGNORE_AMBIENT_CA=true\n")
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	want := []any{map[string]any{
		"variable": "NODE_EXTRA_CA_CERTS", "path": path, "kind": "explicit", "origin": "config-file",
		"certificates": float64(1),
	}}
	if got := loggedSources(t, stderr); !reflect.DeepEqual(got, want) {
		t.Fatalf("logged sources = %v, want %v", got, want)
	}
}

func TestStartupFailsNamingAMissingAmbientFileSetInTheConfigFile(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "deleted.pem")

	stderr, err := runExecutableWithConfigFile(t, "REQUESTS_CA_BUNDLE="+missing+"\n")

	if err == nil {
		t.Fatalf("executable exited 0 with a missing CA file in the config file; stderr:\n%s", stderr)
	}
	// Match the decoded error field: JSON escapes Windows path separators.
	for _, line := range logLines(t, stderr) {
		if msg, _ := line["error"].(string); strings.Contains(msg, "REQUESTS_CA_BUNDLE="+missing) {
			return
		}
	}
	t.Fatalf("startup error does not name REQUESTS_CA_BUNDLE and %s:\n%s", missing, stderr)
}

func TestStartupWarnsAboutAMissingAmbientFileAndStillSucceeds(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "deleted.pem")

	stderr, err := runExecutable(t, "NODE_EXTRA_CA_CERTS="+missing)
	if err != nil {
		t.Fatalf("executable did not exit 0 with a missing ambient file: %v\nstderr:\n%s", err, stderr)
	}

	for _, line := range logLines(t, stderr) {
		warning, _ := line["warning"].(string)
		if line["level"] == "WARN" && line["variable"] == "NODE_EXTRA_CA_CERTS" && strings.Contains(warning, missing) {
			return
		}
	}
	t.Fatalf("no WARN line naming NODE_EXTRA_CA_CERTS and %s in the startup log:\n%s", missing, stderr)
}

func TestStartupIgnoresAmbientSourcesWhenTheIgnoreFlagIsSet(t *testing.T) {
	t.Parallel()
	path := writeCA(t, 1)

	stderr, err := runExecutable(t, "SSL_CERT_FILE="+path, "LANGFUSE_MCP_IGNORE_AMBIENT_CA=true")
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	want := []any{map[string]any{
		"variable": "SSL_CERT_FILE", "path": path, "kind": "ambient", "origin": "environment",
		"certificates": float64(0), "ignored": true,
	}}
	if got := loggedSources(t, stderr); !reflect.DeepEqual(got, want) {
		t.Fatalf("logged sources = %v, want %v", got, want)
	}
	if bytes.Contains(stderr, []byte(`"level":"WARN"`)) {
		t.Fatalf("an ignored source is logged as a warning:\n%s", stderr)
	}
}

func TestStartupFailsNamingAnInvalidIgnoreAmbientCAValue(t *testing.T) {
	t.Parallel()

	stderr, err := runExecutable(t, "LANGFUSE_MCP_IGNORE_AMBIENT_CA=yes")

	if err == nil {
		t.Fatalf("executable exited 0 with an invalid LANGFUSE_MCP_IGNORE_AMBIENT_CA; stderr:\n%s", stderr)
	}
	if !strings.Contains(string(stderr), "LANGFUSE_MCP_IGNORE_AMBIENT_CA") {
		t.Fatalf("startup error does not name LANGFUSE_MCP_IGNORE_AMBIENT_CA:\n%s", stderr)
	}
}
