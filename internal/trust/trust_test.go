package trust_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/trust"
)

func explicitFile(path string) trust.Sources {
	return trust.Sources{Explicit: []trust.Source{{Variable: "LANGFUSE_CA_CERT", Path: path}}}
}

func TestTrustPoolTrustsAServerSignedByACAFromAnExplicitFile(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t, "corp root")
	addr := serveTLS(t, ca)
	path := writeFile(t, t.TempDir(), "corp-root.pem", ca.pem)

	pool, _, err := trust.Build(explicitFile(path))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if err := handshake(t, pool.TLSConfig(), addr); err != nil {
		t.Fatalf("handshake with the CA in an explicit file: %v", err)
	}
}

func TestTrustPoolRejectsAServerSignedByACAFromNoSource(t *testing.T) {
	t.Parallel()
	addr := serveTLS(t, newTestCA(t, "unknown root"))
	other := newTestCA(t, "corp root")
	path := writeFile(t, t.TempDir(), "corp-root.pem", other.pem)

	pool, _, err := trust.Build(explicitFile(path))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if err := handshake(t, pool.TLSConfig(), addr); err == nil {
		t.Fatal("handshake succeeded against a server whose CA is in no source")
	}
}

func TestTrustPoolTrustsServersSignedByEveryCAInAnExplicitDirectory(t *testing.T) {
	t.Parallel()
	root, issuing := newTestCA(t, "corp root"), newTestCA(t, "corp issuing")
	rootAddr, issuingAddr := serveTLS(t, root), serveTLS(t, issuing)
	dir := t.TempDir()
	writeFile(t, dir, "root.pem", root.pem)
	writeFile(t, dir, "issuing.crt", issuing.pem)
	writeFile(t, dir, "README.txt", []byte("not a certificate"))
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}

	pool, _, err := trust.Build(trust.Sources{Explicit: []trust.Source{
		{Variable: "LANGFUSE_CA_CERTS_PATH", Path: dir, Directory: true},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for name, addr := range map[string]string{"root": rootAddr, "issuing": issuingAddr} {
		if err := handshake(t, pool.TLSConfig(), addr); err != nil {
			t.Errorf("handshake with the %s CA from the directory: %v", name, err)
		}
	}
}

func TestTrustPoolTrustsEveryCAInAMultiCertificateBundle(t *testing.T) {
	t.Parallel()
	first, second := newTestCA(t, "first root"), newTestCA(t, "second root")
	firstAddr, secondAddr := serveTLS(t, first), serveTLS(t, second)
	bundle := append(append([]byte{}, first.pem...), second.pem...)
	path := writeFile(t, t.TempDir(), "bundle.pem", bundle)

	pool, _, err := trust.Build(explicitFile(path))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for name, addr := range map[string]string{"first": firstAddr, "second": secondAddr} {
		if err := handshake(t, pool.TLSConfig(), addr); err != nil {
			t.Errorf("handshake with the %s CA of the bundle: %v", name, err)
		}
	}
}

func TestBuildFailsNamingTheVariableAndPathOfABrokenExplicitSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	emptyDir := filepath.Join(dir, "empty-dir")
	if err := os.Mkdir(emptyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	junkDir := filepath.Join(dir, "junk-dir")
	if err := os.Mkdir(junkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, junkDir, "notes.txt", []byte("no certificates here"))

	tests := map[string]trust.Source{
		"missing file": {Variable: "LANGFUSE_CA_CERT", Path: filepath.Join(dir, "missing.pem")},
		"empty file":   {Variable: "LANGFUSE_CA_CERT", Path: writeFile(t, dir, "empty.pem", nil)},
		"non-PEM file": {Variable: "LANGFUSE_CA_CERT", Path: writeFile(t, dir, "key.der", []byte{0x30, 0x82, 0x01})},
		"corrupt certificate in a bundle": {Variable: "LANGFUSE_CA_CERT", Path: writeFile(t, dir, "corrupt.pem",
			append(append([]byte{}, newTestCA(t, "good").pem...), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{0x30, 0x03, 0x02, 0x01, 0x01}})...))},
		"missing directory":  {Variable: "LANGFUSE_CA_CERTS_PATH", Path: filepath.Join(dir, "missing-dir"), Directory: true},
		"empty directory":    {Variable: "LANGFUSE_CA_CERTS_PATH", Path: emptyDir, Directory: true},
		"directory, no PEMs": {Variable: "LANGFUSE_CA_CERTS_PATH", Path: junkDir, Directory: true},
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := trust.Build(trust.Sources{Explicit: []trust.Source{src}})

			if err == nil {
				t.Fatal("Build succeeded, want an error")
			}
			if msg := err.Error(); !strings.Contains(msg, src.Variable) || !strings.Contains(msg, src.Path) {
				t.Fatalf("error %q does not name the variable %s and the path %s", msg, src.Variable, src.Path)
			}
		})
	}
}

func TestReportListsEachExplicitSourceWithItsCertificateCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, second, third := newTestCA(t, "first"), newTestCA(t, "second"), newTestCA(t, "third")
	bundle := writeFile(t, dir, "bundle.pem", append(append([]byte{}, first.pem...), second.pem...))
	certsDir := filepath.Join(dir, "certs")
	if err := os.Mkdir(certsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, certsDir, "third.pem", third.pem)

	_, report, err := trust.Build(trust.Sources{Explicit: []trust.Source{
		{Variable: "LANGFUSE_CA_CERT", Path: bundle},
		{Variable: "LANGFUSE_CA_CERTS_PATH", Path: certsDir, Directory: true},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	want := []trust.SourceReport{
		{Variable: "LANGFUSE_CA_CERT", Path: bundle, Kind: trust.KindExplicit, Certificates: 2},
		{Variable: "LANGFUSE_CA_CERTS_PATH", Path: certsDir, Kind: trust.KindExplicit, Certificates: 1},
	}
	if !slices.Equal(report.Sources, want) {
		t.Fatalf("report sources = %+v, want %+v", report.Sources, want)
	}
}

func TestReportHoldsNoCertificateBytes(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t, "corp root")
	path := writeFile(t, t.TempDir(), "corp-root.pem", ca.pem)

	_, report, err := trust.Build(explicitFile(path))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	body := strings.Split(string(ca.pem), "\n")[1] // first base64 line of the certificate
	for _, rendered := range []string{string(encoded), fmt.Sprintf("%+v", report)} {
		if strings.Contains(rendered, "CERTIFICATE") || strings.Contains(rendered, body) {
			t.Fatalf("report renders certificate content: %s", rendered)
		}
	}
}

// publicRoot is ISRG Root X1 (Let's Encrypt), a public root in every OS store.
// Verifying it against a pool is an offline check that the OS roots are present.
func publicRoot(t *testing.T) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "isrg-root-x1.crt"))
	if err != nil {
		t.Fatalf("read public root fixture: %v", err)
	}
	cert, err := parseFirstCertificate(data)
	if err != nil {
		t.Fatalf("parse public root fixture: %v", err)
	}
	return cert
}

func TestTrustPoolKeepsTheOSRootsWhenExplicitSourcesAreAdded(t *testing.T) {
	t.Parallel()
	root := publicRoot(t)
	system, err := x509.SystemCertPool()
	if err != nil {
		t.Skipf("this machine has no OS certificate store: %v", err)
	}
	if _, err := root.Verify(x509.VerifyOptions{Roots: system}); err != nil {
		t.Skipf("this machine's OS store does not trust ISRG Root X1: %v", err)
	}
	path := writeFile(t, t.TempDir(), "corp-root.pem", newTestCA(t, "corp root").pem)

	pool, _, err := trust.Build(explicitFile(path))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if _, err := root.Verify(x509.VerifyOptions{Roots: pool.TLSConfig().RootCAs}); err != nil {
		t.Fatalf("an OS root is no longer trusted after adding an explicit source: %v", err)
	}
}

func TestTrustPoolRequiresAtLeastTLS12(t *testing.T) {
	t.Parallel()

	pool, _, err := trust.Build(trust.Sources{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := pool.TLSConfig().MinVersion; got != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %#x, want TLS 1.2 (%#x)", got, tls.VersionTLS12)
	}
}
