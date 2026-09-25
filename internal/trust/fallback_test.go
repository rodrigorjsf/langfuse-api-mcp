package trust_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/trust"
)

// childEnv marks a child process started by this test binary. Go loads the OS
// roots once per process, so a test that needs "no OS roots" runs Build in a
// child whose environment hides them.
const childEnv = "LANGFUSE_MCP_TEST_TRUST_CHILD"

// childResult is what the child reports back on stdout.
type childResult struct {
	Report        trust.Report `json:"report"`
	PublicRootErr string       `json:"publicRootErr"`
}

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) == "1" {
		os.Exit(runChild())
	}
	os.Exit(m.Run())
}

// runChild builds a pool with no sources and checks the public root against it.
func runChild() int {
	pool, report, err := trust.Build(trust.Sources{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Build:", err)
		return 1
	}
	res := childResult{Report: report}
	data, err := os.ReadFile(filepath.Join("testdata", "isrg-root-x1.crt"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "read fixture:", err)
		return 1
	}
	root, err := parseFirstCertificate(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse fixture:", err)
		return 1
	}
	if _, err := root.Verify(x509.VerifyOptions{Roots: pool.TLSConfig().RootCAs}); err != nil {
		res.PublicRootErr = err.Error()
	}
	if err := json.NewEncoder(os.Stdout).Encode(res); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		return 1
	}
	return 0
}

func TestTrustPoolFallsBackToBundledRootsWhenTheOSOffersNone(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		// Hiding the OS roots without touching the host store is only possible on Linux,
		// where SSL_CERT_FILE/SSL_CERT_DIR replace the distro locations.
		t.Skip("an empty OS certificate store can only be simulated on linux")
	}
	dir := t.TempDir()
	emptyFile := writeFile(t, dir, "empty.pem", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^$") //nolint:gosec // G204: exe is this test binary, not external input
	cmd.Env = append(os.Environ(), childEnv+"=1", "SSL_CERT_FILE="+emptyFile, "SSL_CERT_DIR="+dir)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("child failed: %v", err)
	}
	var res childResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("decode child output %q: %v", out, err)
	}

	if res.Report.Roots != trust.RootsFallback {
		t.Errorf("report roots = %q, want %q", res.Report.Roots, trust.RootsFallback)
	}
	if res.PublicRootErr != "" {
		t.Errorf("a public root is not trusted with the bundled fallback roots: %s", res.PublicRootErr)
	}
}
