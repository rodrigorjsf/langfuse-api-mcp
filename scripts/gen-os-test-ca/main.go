// Command gen-os-test-ca writes a throwaway "OS" test CA for the cross-OS trust
// proof (spec #7, seam S2).
//
// WHAT: generates a fresh, one-day, self-signed ECDSA P-256 CA and writes
// <dir>/ca.pem (certificate) and <dir>/ca-key.pem (private key), both 0600.
//
// WHY: the process-level trust test in cmd/langfuse-mcp must prove that the
// executable trusts a CA installed in the OS certificate store. The test signs
// a local TLS server's certificate with this key, so it needs a CA that exists
// only for one CI job: the key is never committed and dies with the runner.
//
// WHEN: in CI only, before the step that installs ca.pem into the runner's
// system trust store (.github/workflows/ci.yml). Never install it on a
// developer machine: the test skips the OS-store proof locally.
//
// HOW:
//
//	go run ./scripts/gen-os-test-ca "$RUNNER_TEMP/os-test-ca"
//
// then point LANGFUSE_MCP_TEST_OS_CA_DIR at that directory for `go test`.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gen-os-test-ca <output directory>")
		os.Exit(2)
	}
	if err := write(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "gen-os-test-ca:", err)
		os.Exit(1)
	}
}

func write(dir string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return fmt.Errorf("generate serial number: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "langfuse-mcp CI OS test CA (throwaway)"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("encode key: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // G703: the operator names the output directory; writing there is the point
		return err
	}
	files := map[string]*pem.Block{
		"ca.pem":     {Type: "CERTIFICATE", Bytes: der},
		"ca-key.pem": {Type: "EC PRIVATE KEY", Bytes: keyDER},
	}
	for name, block := range files {
		if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o600); err != nil { //nolint:gosec // G703: as above
			return err
		}
	}
	return nil
}
