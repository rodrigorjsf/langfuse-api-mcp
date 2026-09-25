package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"maps"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/trust"
)

// Process-level trust proof (spec #7, seam S2; ADR-0006).
//
// The test re-executes this test binary as a child that starts exactly like the
// executable (start), then handshakes with local TLS servers through the trust
// pool the executable built, and prints what it observed as JSON on stdout.

const (
	// probeEnv holds the JSON map of target name to host:port a probe child
	// handshakes with. When set, TestMain runs probe instead of the tests.
	probeEnv = "LANGFUSE_MCP_TEST_PROBE"
	// loadRootsFirstEnv makes the probe child load the OS roots before start:
	// the ordering bug the guard test must catch, used as its control case.
	loadRootsFirstEnv = "LANGFUSE_MCP_TEST_LOAD_ROOTS_FIRST"
	// osCADirEnv names the directory holding the throwaway "OS" test CA
	// (ca.pem, ca-key.pem) that CI installed into the runner's system trust
	// store before the tests (scripts/gen-os-test-ca). Never set locally.
	osCADirEnv = "LANGFUSE_MCP_TEST_OS_CA_DIR"
)

// Names of the probe targets: local TLS servers signed by the OS-store CA and
// by the ambient (SSL_CERT_FILE) CA.
const (
	osStoreTarget = "os-store-ca"
	ambientTarget = "ambient-ca"
)

// probeResult is what a probe child observed after startup.
type probeResult struct {
	// Environment says, per variable, whether it was still set after startup.
	Environment map[string]bool `json:"environment"`
	// Handshakes maps each target name to "ok" or the handshake error.
	Handshakes map[string]string `json:"handshakes"`
}

// probe runs in the child: it starts like the executable, then handshakes with
// every target through the executable's trust pool and prints a probeResult.
func probe(targetsJSON string) int {
	var targets map[string]string
	if err := json.Unmarshal([]byte(targetsJSON), &targets); err != nil {
		log.Printf("decode %s: %v", probeEnv, err)
		return 2
	}
	if os.Getenv(loadRootsFirstEnv) == "1" {
		_, _ = x509.SystemCertPool() // deliberately before the capture: the roots are cached now
	}
	pool, ok := start()
	if !ok {
		return 1
	}
	result := probeResult{Environment: map[string]bool{}, Handshakes: map[string]string{}}
	for _, v := range []string{trust.EnvSSLCertFile, trust.EnvSSLCertDir} {
		_, set := os.LookupEnv(v)
		result.Environment[v] = set
	}
	for name, addr := range targets {
		result.Handshakes[name] = handshake(pool, addr)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		log.Printf("encode probe result: %v", err)
		return 2
	}
	return 0
}

// handshake completes a TLS handshake with addr trusting exactly pool, and
// returns "ok" or the error.
func handshake(pool trust.Pool, addr string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialer := tls.Dialer{Config: pool.TLSConfig()}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err.Error()
	}
	_ = conn.Close()
	return "ok"
}

// probeExecutable runs a probe child with the targets and extra environment
// entries, and returns what it observed.
func probeExecutable(t *testing.T, targets map[string]string, env ...string) probeResult {
	t.Helper()
	encoded, err := json.Marshal(targets)
	if err != nil {
		t.Fatalf("encode targets: %v", err)
	}
	configEnv, _ := userConfigLocation(t)
	env = append(append(configEnv, probeEnv+"="+string(encoded)), env...)

	stdout, stderr, err := runChildOutput(t, env)
	if err != nil {
		t.Fatalf("probe child failed: %v\nstderr:\n%s", err, stderr)
	}
	var result probeResult
	if err := json.Unmarshal(stdout, &result); err != nil {
		t.Fatalf("decode probe result %q: %v\nstderr:\n%s", stdout, err, stderr)
	}
	return result
}

// certAuthority is a CA whose key can sign server certificates.
type certAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// newCertAuthority generates a throwaway CA and writes its certificate as PEM
// to a fresh temp directory, returning the CA and the certificate's path.
func newCertAuthority(t *testing.T, name string) (certAuthority, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          randomSerial(t),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write CA certificate: %v", err)
	}
	return certAuthority{cert: cert, key: key}, path
}

func randomSerial(t *testing.T) *big.Int {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatalf("generate serial number: %v", err)
	}
	return serial
}

// serveTLS starts a local TLS server whose certificate for 127.0.0.1 is signed
// by ca, and returns its host:port.
func serveTLS(t *testing.T, ca certAuthority) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: randomSerial(t),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create server certificate: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	// The probe closes right after the handshake; the server's EOF log is noise.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.TLS = &tls.Config{ //nolint:gosec // G402: test server; the client side under test sets MinVersion
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

func TestExecutableRemovesSSLCertFileAndSSLCertDirFromItsEnvironment(t *testing.T) {
	t.Parallel()
	_, file := newCertAuthority(t, "ambient test CA")

	got := probeExecutable(t, map[string]string{},
		trust.EnvSSLCertFile+"="+file, trust.EnvSSLCertDir+"="+filepath.Dir(file))

	want := map[string]bool{trust.EnvSSLCertFile: false, trust.EnvSSLCertDir: false}
	if !maps.Equal(got.Environment, want) {
		t.Fatalf("still set after startup = %v, want %v", got.Environment, want)
	}
}

// installedOSCA returns the "OS" test CA that CI installed into the runner's
// system trust store. Local runs never touch the OS store, so the test is
// skipped there; in CI a missing CA is a failure, never a silent skip.
func installedOSCA(t *testing.T) certAuthority {
	t.Helper()
	dir := os.Getenv(osCADirEnv)
	if dir == "" {
		if os.Getenv("CI") == "true" {
			t.Fatalf("%s is not set: the CI step installing the OS test CA did not run", osCADirEnv)
		}
		t.Skipf("%s is not set: the OS-store proof runs only in CI, which installs a test CA into the system trust store", osCADirEnv)
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, "ca.pem")) //nolint:gosec // G304: path from the CI step
	if err != nil {
		t.Fatalf("read OS test CA certificate: %v", err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, "ca-key.pem")) //nolint:gosec // G304: path from the CI step
	if err != nil {
		t.Fatalf("read OS test CA key: %v", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certBlock == nil || keyBlock == nil {
		t.Fatalf("OS test CA files in %s hold no PEM block", dir)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse OS test CA certificate: %v", err)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("parse OS test CA key: %v", err)
	}
	return certAuthority{cert: cert, key: key}
}

// TestExecutableTrustsTheOSStoreAndAmbientCAsAtOnce is the cross-OS proof of
// ADR-0006: the running executable trusts a CA installed in the OS store and a
// CA exported through SSL_CERT_FILE, in the same process.
//
// It is also the guard of the capture order: Go caches the OS roots the first
// time anything loads them, so if certificate handling ran before the ambient
// capture with both SSL_CERT_FILE and SSL_CERT_DIR exported, the OS store would
// be lost for good (on Linux only when both are exported; on macOS/Windows
// either one alone switches the platform verifier off). The control case does
// exactly that on purpose and must lose the OS store; it proves the guard can
// fail on this runner. The probe enters through start, so the guard covers
// start and every package init; main itself only calls start.
func TestExecutableTrustsTheOSStoreAndAmbientCAsAtOnce(t *testing.T) {
	t.Parallel()
	osCA := installedOSCA(t)
	osServer := serveTLS(t, osCA)
	ambientCA, ambientFile := newCertAuthority(t, "ambient test CA")
	ambientServer := serveTLS(t, ambientCA)
	targets := map[string]string{osStoreTarget: osServer, ambientTarget: ambientServer}
	both := []string{trust.EnvSSLCertFile + "=" + ambientFile, trust.EnvSSLCertDir + "=" + filepath.Dir(ambientFile)}

	tests := map[string]struct {
		env      []string
		wantOSCA bool
	}{
		"SSL_CERT_FILE exported": {
			env:      []string{trust.EnvSSLCertFile + "=" + ambientFile},
			wantOSCA: true,
		},
		"guard: SSL_CERT_FILE and SSL_CERT_DIR exported": {
			env:      both,
			wantOSCA: true,
		},
		"control: roots loaded before the capture lose the OS store": {
			env:      append([]string{loadRootsFirstEnv + "=1"}, both...),
			wantOSCA: false,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := probeExecutable(t, targets, tc.env...).Handshakes

			if got[ambientTarget] != "ok" {
				t.Errorf("handshake with the ambient-CA server: %s, want ok", got[ambientTarget])
			}
			if trusted := got[osStoreTarget] == "ok"; trusted != tc.wantOSCA {
				t.Errorf("handshake with the OS-store-CA server: %s; want trusted=%v", got[osStoreTarget], tc.wantOSCA)
			}
		})
	}
}
