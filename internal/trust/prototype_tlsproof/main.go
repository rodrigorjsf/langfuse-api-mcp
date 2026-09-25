// PROTOTYPE — throwaway. Answers issue #4/#13: does "capture SSL_CERT_* then
// os.Unsetenv at the top of main" keep OS-store trust AND add ambient CAs,
// on Go 1.27.1, per OS? Not production code; see internal/trust for the real one.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "github.com/modelcontextprotocol/go-sdk/mcp" // proves no init() of the SDK loads system roots
)

// Captured before anything else in main (mode "unset").
var capturedFile, capturedDir string

func main() {
	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "child-unset" {
		capturedFile, capturedDir = os.Getenv("SSL_CERT_FILE"), os.Getenv("SSL_CERT_DIR")
		os.Unsetenv("SSL_CERT_FILE")
		os.Unsetenv("SSL_CERT_DIR")
	}
	switch mode {
	case "gen":
		gen(os.Args[2])
	case "run":
		run(os.Args[2])
	case "child-nounset", "child-unset", "child-late":
		child(mode, os.Args[2:])
	default:
		fmt.Println("usage: tlsproof gen <dir> | run <dir>")
		os.Exit(2)
	}
}

// ---- cert generation -------------------------------------------------------

func newCA(name string) (*x509.Certificate, *ecdsa.PrivateKey) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	t := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true,
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true}
	d, _ := x509.CreateCertificate(rand.Reader, t, t, &k.PublicKey, k)
	c, _ := x509.ParseCertificate(d)
	return c, k
}

func leaf(ca *x509.Certificate, caKey *ecdsa.PrivateKey) ([]byte, []byte) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	t := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage: x509.KeyUsageDigitalSignature}
	d, _ := x509.CreateCertificate(rand.Reader, t, ca, &k.PublicKey, caKey)
	kd, _ := x509.MarshalECPrivateKey(k)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: d}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})
}

func gen(dir string) {
	os.MkdirAll(filepath.Join(dir, "ambient-dir"), 0o755)
	for _, n := range []string{"os", "ambient"} {
		ca, k := newCA("PROTOTYPE " + n + " CA")
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
		os.WriteFile(filepath.Join(dir, n+"-ca.pem"), caPEM, 0o644)
		c, kk := leaf(ca, k)
		os.WriteFile(filepath.Join(dir, n+"-leaf.pem"), c, 0o644)
		os.WriteFile(filepath.Join(dir, n+"-leaf.key"), kk, 0o600)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "ambient-ca.pem"))
	os.WriteFile(filepath.Join(dir, "ambient-dir", "ambient-ca.pem"), b, 0o644)
	fmt.Println("generated in", dir)
}

// ---- parent: servers + child matrix -----------------------------------------

func server(dir, n string) string {
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, n+"-leaf.pem"), filepath.Join(dir, n+"-leaf.key"))
	if err != nil {
		panic(err)
	}
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	s.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	s.StartTLS()
	return s.URL
}

func run(dir string) {
	osURL, ambURL := server(dir, "os"), server(dir, "ambient")
	public := "https://cloud.langfuse.com/api/public/health"
	targets := []string{"os-store-ca=" + osURL, "ambient-ca=" + ambURL, "public=" + public}
	ambFile := filepath.Join(dir, "ambient-ca.pem")
	ambDir := filepath.Join(dir, "ambient-dir")
	cases := []struct{ mode, env string }{
		{"child-nounset", ""},
		{"child-nounset", "SSL_CERT_FILE=" + ambFile},
		{"child-unset", "SSL_CERT_FILE=" + ambFile},
		{"child-nounset", "SSL_CERT_DIR=" + ambDir},
		{"child-unset", "SSL_CERT_DIR=" + ambDir},
		{"child-late", "SSL_CERT_FILE=" + ambFile},
		{"child-nounset", "SSL_CERT_FILE=" + ambFile + "\x00SSL_CERT_DIR=" + ambDir},
		{"child-unset", "SSL_CERT_FILE=" + ambFile + "\x00SSL_CERT_DIR=" + ambDir},
		{"child-late", "SSL_CERT_FILE=" + ambFile + "\x00SSL_CERT_DIR=" + ambDir},
	}
	self, _ := os.Executable()
	fmt.Printf("parent go=%s os=%s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	for _, c := range cases {
		cmd := exec.Command(self, append([]string{c.mode}, targets...)...)
		env := []string{}
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "SSL_CERT_") {
				env = append(env, e)
			}
		}
		if c.env != "" {
			env = append(env, strings.Split(c.env, "\x00")...)
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		fmt.Printf("\n### %s  env=[%s]  err=%v\n%s", c.mode, strings.ReplaceAll(strings.ReplaceAll(c.env, dir, "<dir>"), "\x00", " "), err, out)
	}
}

// ---- child -----------------------------------------------------------------

func appendPEMs(pool *x509.CertPool) (n int) {
	add := func(p string) {
		if b, err := os.ReadFile(p); err == nil && pool.AppendCertsFromPEM(b) {
			n++
		}
	}
	if capturedFile != "" {
		add(capturedFile)
	}
	for _, d := range filepath.SplitList(capturedDir) {
		es, _ := os.ReadDir(d)
		for _, e := range es {
			add(filepath.Join(d, e.Name()))
		}
	}
	return
}

func child(mode string, targets []string) {
	res := map[string]any{"go": runtime.Version(), "os": runtime.GOOS, "mode": mode}
	if mode == "child-late" {
		x509.SystemCertPool() // roots loaded while SSL_CERT_FILE is still set (the bug the guard test must catch)
		capturedFile, capturedDir = os.Getenv("SSL_CERT_FILE"), os.Getenv("SSL_CERT_DIR")
		os.Unsetenv("SSL_CERT_FILE")
		os.Unsetenv("SSL_CERT_DIR")
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		res["systemPoolErr"] = err.Error()
		pool = x509.NewCertPool()
	}
	if mode != "child-nounset" {
		res["appendedFiles"] = appendPEMs(pool)
	}
	_, f := os.LookupEnv("SSL_CERT_FILE")
	_, d := os.LookupEnv("SSL_CERT_DIR")
	res["envAfter"] = map[string]bool{"SSL_CERT_FILE": f, "SSL_CERT_DIR": d}
	cl := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	r := map[string]string{}
	for _, t := range targets {
		name, url, _ := strings.Cut(t, "=")
		resp, err := cl.Get(url)
		if err != nil {
			msg := err.Error()
			if i := strings.Index(msg, "tls: "); i >= 0 {
				msg = msg[i:]
			}
			r[name] = "FAIL " + msg
			continue
		}
		resp.Body.Close()
		r[name] = fmt.Sprintf("OK %d", resp.StatusCode)
	}
	res["results"] = r
	b, _ := json.Marshal(res)
	fmt.Println(string(b))
}
