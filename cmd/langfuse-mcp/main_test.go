// Package main, not main_test: TestMain must call the unexported main() in the child process.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// runMainEnv marks a child process started by this test binary: instead of
// running the tests, the child runs the real main(), so the test observes the
// executable's behavior at the process seam (exit code), not internals.
const runMainEnv = "LANGFUSE_MCP_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	// probeEnv is checked first: every child also carries runMainEnv=1 (runChildOutput).
	if targets := os.Getenv(probeEnv); targets != "" {
		os.Exit(probe(targets))
	}
	if os.Getenv(runMainEnv) == "1" {
		main()
		os.Exit(0) // main returned normally: that is a clean exit
	}
	os.Exit(m.Run())
}

// runExecutable runs the real main() in a child process with the extra
// environment entries and returns its stderr and exit error. The child's OS
// user config location is an empty temp directory, so no config file exists
// and the developer's own config file never leaks into a test.
func runExecutable(t *testing.T, env ...string) ([]byte, error) {
	t.Helper()
	configEnv, _ := userConfigLocation(t)
	return runChild(t, append(configEnv, env...))
}

// runExecutableWithConfigFile is runExecutable with a config file holding
// content at the documented location for the running OS.
func runExecutableWithConfigFile(t *testing.T, content string, env ...string) ([]byte, error) {
	t.Helper()
	configEnv, path := userConfigLocation(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return runChild(t, append(configEnv, env...))
}

// userConfigLocation points the child's OS user config location at a fresh
// temp directory. It returns the environment entries doing so and the path
// where README documents the config file for the running OS.
func userConfigLocation(t *testing.T) (env []string, configFile string) {
	t.Helper()
	home := t.TempDir()
	switch runtime.GOOS {
	case "windows": // %AppData%\langfuse-mcp\config.env
		appData := filepath.Join(home, "AppData", "Roaming")
		return []string{"AppData=" + appData}, filepath.Join(appData, "langfuse-mcp", "config.env")
	case "darwin": // ~/Library/Application Support/langfuse-mcp/config.env
		return []string{"HOME=" + home}, filepath.Join(home, "Library", "Application Support", "langfuse-mcp", "config.env")
	default: // $XDG_CONFIG_HOME/langfuse-mcp/config.env
		xdg := filepath.Join(home, "xdg-config")
		return []string{"HOME=" + home, "XDG_CONFIG_HOME=" + xdg}, filepath.Join(xdg, "langfuse-mcp", "config.env")
	}
}

// runChild runs the real main() in a child process with the extra environment
// entries and returns its stderr and exit error.
func runChild(t *testing.T, env []string) ([]byte, error) {
	t.Helper()
	_, stderr, err := runChildOutput(t, env)
	return stderr, err
}

// runChildOutput runs this test binary as a child with the extra environment
// entries (later entries win) and returns its stdout, stderr and exit error.
func runChildOutput(t *testing.T, env []string) (stdout, stderr []byte, err error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^$") //nolint:gosec // G204: exe is this test binary, not external input
	cmd.Env = append(append(append(os.Environ(), runMainEnv+"=1"), hermeticEnv...), connectionEnv...)
	cmd.Env = append(cmd.Env, env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// hermeticEnv clears the CA and proxy variables the developer's or runner's
// own environment may hold, so each test sets exactly the sources it asserts on.
var hermeticEnv = []string{
	"LANGFUSE_CA_CERT=", "LANGFUSE_CA_CERTS_PATH=", "LANGFUSE_MCP_IGNORE_AMBIENT_CA=",
	"SSL_CERT_FILE=", "SSL_CERT_DIR=", "NODE_EXTRA_CA_CERTS=", "REQUESTS_CA_BUNDLE=", "CURL_CA_BUNDLE=",
	"HTTPS_PROXY=", "https_proxy=", "HTTP_PROXY=", "http_proxy=", "NO_PROXY=", "no_proxy=",
}

// connectionEnv is a valid Langfuse connection, so every child starts unless a
// test overrides it. Nothing listens on the discard port: tests that call
// Langfuse point LANGFUSE_BASE_URL at their own httptest server.
var connectionEnv = []string{
	"LANGFUSE_BASE_URL=http://127.0.0.1:9", "LANGFUSE_HOST=",
	"LANGFUSE_PUBLIC_KEY=" + stdioPublicKey, "LANGFUSE_SECRET_KEY=" + stdioSecretKey,
}

// logLines decodes the JSON log lines the executable wrote to stderr.
func logLines(t *testing.T, stderr []byte) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range bytes.Lines(stderr) {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("stderr line is not a JSON log entry: %q", line)
		}
		lines = append(lines, entry)
	}
	return lines
}

// writeCA writes a PEM file holding n freshly generated self-signed CA certificates.
func writeCA(t *testing.T, n int) string {
	t.Helper()
	var bundle []byte
	for i := range n {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(int64(i + 1)),
			Subject:               pkix.Name{CommonName: fmt.Sprintf("test CA %d", i)},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(time.Hour),
			IsCA:                  true,
			BasicConstraintsValid: true,
			KeyUsage:              x509.KeyUsageCertSign,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			t.Fatalf("create certificate: %v", err)
		}
		bundle = append(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	path := filepath.Join(t.TempDir(), "corp-root.pem")
	if err := os.WriteFile(path, bundle, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	return path
}

func TestExecutableStartsAndExitsCleanly(t *testing.T) {
	t.Parallel()

	stderr, err := runExecutable(t, "LANGFUSE_CA_CERT=", "LANGFUSE_CA_CERTS_PATH=")
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}
}

func TestStartupFailsNamingTheVariableAndPathOfAMissingExplicitCAFile(t *testing.T) {
	t.Parallel()

	// A CA path from the config file is an explicit source, exactly like the variable.
	tests := map[string]func(t *testing.T, missing string) ([]byte, error){
		"environment": func(t *testing.T, missing string) ([]byte, error) {
			return runExecutable(t, "LANGFUSE_CA_CERT="+missing, "LANGFUSE_CA_CERTS_PATH=")
		},
		"config file": func(t *testing.T, missing string) ([]byte, error) {
			return runExecutableWithConfigFile(t, "LANGFUSE_CA_CERT="+missing+"\n",
				"LANGFUSE_CA_CERT=", "LANGFUSE_CA_CERTS_PATH=")
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			missing := filepath.Join(t.TempDir(), "missing.pem")

			stderr, err := run(t, missing)

			if err == nil {
				t.Fatalf("executable exited 0 with a missing explicit CA file; stderr:\n%s", stderr)
			}
			msg := startupError(t, stderr)
			if !strings.Contains(msg, "LANGFUSE_CA_CERT") || !strings.Contains(msg, missing) {
				t.Fatalf("startup error %q does not name LANGFUSE_CA_CERT and %s", msg, missing)
			}
		})
	}
}

// startupError returns the error of the single log line a failed startup writes.
func startupError(t *testing.T, stderr []byte) string {
	t.Helper()
	lines := logLines(t, stderr)
	if len(lines) != 1 {
		t.Fatalf("want exactly one startup error line, got %d:\n%s", len(lines), stderr)
	}
	msg, _ := lines[0]["error"].(string)
	return msg
}

func TestStartupRefusesAConfigFileHoldingALangfuseKey(t *testing.T) {
	t.Parallel()

	stderr, err := runExecutableWithConfigFile(t, "LANGFUSE_SECRET_KEY=sk-lf-do-not-log\n")

	if err == nil {
		t.Fatalf("executable exited 0 with a key in the config file; stderr:\n%s", stderr)
	}
	if bytes.Contains(stderr, []byte("sk-lf-do-not-log")) {
		t.Fatalf("startup log leaks the key value:\n%s", stderr)
	}
	if msg := startupError(t, stderr); !strings.Contains(msg, "LANGFUSE_SECRET_KEY") {
		t.Fatalf("startup error %q does not name LANGFUSE_SECRET_KEY", msg)
	}
}

func TestStartupFailsNamingTheLineNumberOfAMalformedConfigFileLine(t *testing.T) {
	t.Parallel()

	stderr, err := runExecutableWithConfigFile(t, "# corporate settings\nnot a setting\n")

	if err == nil {
		t.Fatalf("executable exited 0 with a malformed config file; stderr:\n%s", stderr)
	}
	if msg := startupError(t, stderr); !strings.Contains(msg, "config.env line 2") {
		t.Fatalf("startup error %q does not name config.env line 2", msg)
	}
}

func TestStartupLogListsTheCASourcesWithTheirCertificateCounts(t *testing.T) {
	t.Parallel()
	path := writeCA(t, 2)

	stderr, err := runExecutable(t, "LANGFUSE_CA_CERT="+path, "LANGFUSE_CA_CERTS_PATH=")
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	if bytes.Contains(stderr, []byte("CERTIFICATE")) {
		t.Fatalf("startup log contains certificate content:\n%s", stderr)
	}
	for _, line := range logLines(t, stderr) {
		if line["msg"] != "CA sources loaded" {
			continue
		}
		want := []any{map[string]any{
			"variable": "LANGFUSE_CA_CERT", "path": path, "kind": "explicit", "origin": "environment",
			"certificates": float64(2),
		}}
		if !reflect.DeepEqual(line["sources"], want) {
			t.Fatalf("logged sources = %v, want %v", line["sources"], want)
		}
		return
	}
	t.Fatalf("no \"CA sources loaded\" line in the startup log:\n%s", stderr)
}

func TestStartupLoadsCASourcesNamedInTheConfigFileAsExplicitSources(t *testing.T) {
	t.Parallel()
	path := writeCA(t, 2)
	dir := filepath.Dir(writeCA(t, 1))

	stderr, err := runExecutableWithConfigFile(t,
		"LANGFUSE_CA_CERT="+path+"\nLANGFUSE_CA_CERTS_PATH="+dir+"\n",
		"LANGFUSE_CA_CERT=", "LANGFUSE_CA_CERTS_PATH=")
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	want := []any{
		map[string]any{
			"variable": "LANGFUSE_CA_CERT", "path": path, "kind": "explicit", "origin": "config-file",
			"certificates": float64(2),
		},
		map[string]any{
			"variable": "LANGFUSE_CA_CERTS_PATH", "path": dir, "kind": "explicit", "origin": "config-file",
			"certificates": float64(1),
		},
	}
	if got := loggedSources(t, stderr); !reflect.DeepEqual(got, want) {
		t.Fatalf("logged sources = %v, want %v", got, want)
	}
}

func TestStartupWarnsNamingTheFileLineAndKeyOfAnUnknownConfigFileKey(t *testing.T) {
	t.Parallel()

	stderr, err := runExecutableWithConfigFile(t, "LANGFUSE_CA_CRT=/x.pem\n", "LANGFUSE_CA_CERT=", "LANGFUSE_CA_CERTS_PATH=")
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	for _, line := range logLines(t, stderr) {
		if line["level"] != "WARN" || line["key"] != "LANGFUSE_CA_CRT" {
			continue
		}
		file, ok := line["file"].(string)
		if !ok || !strings.HasSuffix(file, filepath.Join("langfuse-mcp", "config.env")) || line["line"] != float64(1) {
			t.Fatalf("warning %v does not name the config file and line 1", line)
		}
		return
	}
	t.Fatalf("no warning naming LANGFUSE_CA_CRT in the startup log:\n%s", stderr)
}

func TestStartupWarningAboutAnUnknownConfigFileKeyNeverHoldsItsValue(t *testing.T) {
	t.Parallel()

	stderr, err := runExecutableWithConfigFile(t, "LANGFUSE_SECRT_KEY=sk-lf-do-not-log\n")
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	if bytes.Contains(stderr, []byte("sk-lf-do-not-log")) {
		t.Fatalf("startup log quotes the config file value:\n%s", stderr)
	}
}

func TestStartupFailsNamingAnInvalidRequestLimit(t *testing.T) {
	t.Parallel()
	for _, setting := range []string{"LANGFUSE_MCP_RATE_LIMIT=0", "LANGFUSE_MCP_MAX_CONCURRENCY=-1"} {
		t.Run(setting, func(t *testing.T) {
			t.Parallel()
			variable, _, _ := strings.Cut(setting, "=")

			stderr, err := runExecutable(t, setting)

			if err == nil {
				t.Fatalf("executable exited 0 with %s; stderr:\n%s", setting, stderr)
			}
			if !strings.Contains(string(stderr), variable) {
				t.Fatalf("startup error does not name %s:\n%s", variable, stderr)
			}
		})
	}
}

// Issue #47: the startup log states the effective rate limit and why it has
// that value.
func TestStartupLogStatesTheEffectiveRateLimitAndItsSource(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		env        []string
		perMinute  float64
		wantSource string
	}{
		"Cloud host":       {[]string{"LANGFUSE_BASE_URL=https://cloud.langfuse.com", "LANGFUSE_MCP_RATE_LIMIT="}, 30, "default-cloud"},
		"self-hosted host": {[]string{"LANGFUSE_BASE_URL=https://langfuse.corp.example", "LANGFUSE_MCP_RATE_LIMIT="}, 1000, "default-self-hosted"},
		"explicit value":   {[]string{"LANGFUSE_BASE_URL=https://cloud.langfuse.com", "LANGFUSE_MCP_RATE_LIMIT=250"}, 250, "explicit"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stderr, err := runExecutable(t, tc.env...)
			if err != nil {
				t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
			}

			for _, line := range logLines(t, stderr) {
				if line["msg"] != "rate limit" {
					continue
				}
				if line["perMinute"] != tc.perMinute || line["source"] != tc.wantSource {
					t.Fatalf("rate limit line = %v, want perMinute %v, source %q", line, tc.perMinute, tc.wantSource)
				}
				return
			}
			t.Fatalf("no \"rate limit\" line in the startup log:\n%s", stderr)
		})
	}
}

// The startup log, rate-limit line included, never holds the Langfuse keys.
func TestStartupLogNeverHoldsTheLangfuseKeys(t *testing.T) {
	t.Parallel()

	stderr, err := runExecutable(t, "LANGFUSE_BASE_URL=https://cloud.langfuse.com")
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	if bytes.Contains(stderr, []byte(stdioSecretKey)) || bytes.Contains(stderr, []byte(stdioPublicKey)) {
		t.Fatalf("startup log contains a Langfuse key:\n%s", stderr)
	}
}
