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
	"strings"
	"testing"
	"time"
)

// runMainEnv marks a child process started by this test binary: instead of
// running the tests, the child runs the real main(), so the test observes the
// executable's behavior at the process seam (exit code), not internals.
const runMainEnv = "LANGFUSE_MCP_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		os.Exit(0) // main returned normally: that is a clean exit
	}
	os.Exit(m.Run())
}

// runExecutable runs the real main() in a child process with the extra
// environment entries and returns its stderr and exit error.
func runExecutable(t *testing.T, env ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^$") //nolint:gosec // G204: exe is this test binary, not external input
	cmd.Env = append(append(os.Environ(), runMainEnv+"=1"), hermeticEnv...)
	cmd.Env = append(cmd.Env, env...) // later entries win
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stderr.Bytes(), err
}

// hermeticEnv clears the CA-related variables the developer's or runner's own
// environment may hold, so each test sets exactly the sources it asserts on.
var hermeticEnv = []string{
	"LANGFUSE_CA_CERT=", "LANGFUSE_CA_CERTS_PATH=", "LANGFUSE_MCP_IGNORE_AMBIENT_CA=",
	"SSL_CERT_FILE=", "SSL_CERT_DIR=", "NODE_EXTRA_CA_CERTS=", "REQUESTS_CA_BUNDLE=", "CURL_CA_BUNDLE=",
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
	missing := filepath.Join(t.TempDir(), "missing.pem")

	stderr, err := runExecutable(t, "LANGFUSE_CA_CERT="+missing, "LANGFUSE_CA_CERTS_PATH=")

	if err == nil {
		t.Fatalf("executable exited 0 with a missing explicit CA file; stderr:\n%s", stderr)
	}
	lines := logLines(t, stderr)
	if len(lines) != 1 {
		t.Fatalf("want exactly one startup error line, got %d:\n%s", len(lines), stderr)
	}
	msg, _ := lines[0]["error"].(string)
	if !strings.Contains(msg, "LANGFUSE_CA_CERT") || !strings.Contains(msg, missing) {
		t.Fatalf("startup error %q does not name LANGFUSE_CA_CERT and %s", msg, missing)
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
			"variable": "LANGFUSE_CA_CERT", "path": path, "kind": "explicit", "certificates": float64(2),
		}}
		if !reflect.DeepEqual(line["sources"], want) {
			t.Fatalf("logged sources = %v, want %v", line["sources"], want)
		}
		return
	}
	t.Fatalf("no \"CA sources loaded\" line in the startup log:\n%s", stderr)
}
