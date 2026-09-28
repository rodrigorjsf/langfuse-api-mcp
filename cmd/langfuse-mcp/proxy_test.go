package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Ticket #59 (spec #58), seam S3: the executable sends its Langfuse requests
// through the proxy named by HTTPS_PROXY in its environment. The fake Langfuse
// answers for tunnelHost, which never resolves (RFC 6761) and is not loopback,
// which Go never proxies: a call that succeeds went through the proxy.

const tunnelHost = "langfuse.test"

// tunnelLangfuse starts a TLS fake Langfuse whose certificate names
// tunnelHost, signed by ca; it answers health with version 4.46.0 and every
// other request, the deployment profile's sentinels included, with a trace.
func tunnelLangfuse(t *testing.T, ca certAuthority) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: randomSerial(t), Subject: pkix.Name{CommonName: tunnelHost}, DNSNames: []string{tunnelHost},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create server certificate: %v", err)
	}
	fake := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/public/health" {
			_, _ = io.WriteString(w, health4460) // a failed write leaves the version unknown
			return
		}
		_, _ = io.WriteString(w, `{"id":"trace-1","name":"checkout"}`) // a failed write fails the call
	}))
	fake.Config.ErrorLog = log.New(io.Discard, "", 0)
	fake.TLS = &tls.Config{ //nolint:gosec // G402: test server; the executable under test sets MinVersion
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}
	fake.StartTLS()
	t.Cleanup(fake.Close)
	return fake
}

// connectProxy is a fake HTTP CONNECT proxy that tunnels tunnelHost:443 to
// the fake Langfuse and records the CONNECT targets and Proxy-Authorization
// headers it was sent.
type connectProxy struct {
	url             *url.URL
	mu              sync.Mutex
	connects, auths []string
	tunnels         []net.Conn
}

func newConnectProxy(t *testing.T, langfuseAddr string) *connectProxy {
	t.Helper()
	p := &connectProxy{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.connects = append(p.connects, r.Host)
		p.auths = append(p.auths, r.Header.Get("Proxy-Authorization"))
		p.mu.Unlock()
		if r.Method != http.MethodConnect || r.Host != tunnelHost+":443" {
			http.Error(w, "this proxy only tunnels to "+tunnelHost+":443", http.StatusForbidden)
			return
		}
		upstream, err := new(net.Dialer).DialContext(r.Context(), "tcp", langfuseAddr)
		if err != nil {
			http.Error(w, "dial the fake Langfuse", http.StatusBadGateway)
			return
		}
		client, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			_ = upstream.Close()
			t.Errorf("hijack the CONNECT request: %v", err)
			return
		}
		p.mu.Lock()
		p.tunnels = append(p.tunnels, client, upstream)
		p.mu.Unlock()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n") // a failed write fails the call
		go func() { _, _ = io.Copy(upstream, rw.Reader); _ = upstream.Close() }()    // ends when either side closes
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { // runs first: hijacked connections outlive srv.Close
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, c := range p.tunnels {
			_ = c.Close() // already closed when the executable ended the tunnel
		}
	})
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	p.url = u
	return p
}

func (p *connectProxy) seen() (connects, auths []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.connects...), append([]string(nil), p.auths...)
}

// proxyLogLine returns the startup log line about the proxy.
func proxyLogLine(t *testing.T, stderr []byte) map[string]any {
	t.Helper()
	for _, line := range logLines(t, stderr) {
		if line["msg"] == "proxy" {
			return line
		}
	}
	t.Fatalf("no startup log line about the proxy:\n%s", stderr)
	return nil
}

func TestExecutableReachesLangfuseThroughTheProxyInItsEnvironment(t *testing.T) {
	t.Parallel()
	const user, password = "proxyuser-7f3a", "proxysecret-9c1e" //nolint:gosec // G101: fake proxy credentials
	ca, caFile := newCertAuthority(t, "proxy test CA")
	proxy := newConnectProxy(t, tunnelLangfuse(t, ca).Listener.Addr().String())
	s := startStdio(t, "LANGFUSE_BASE_URL=https://"+tunnelHost, "LANGFUSE_CA_CERT="+caFile,
		"HTTPS_PROXY=http://"+user+":"+password+"@"+proxy.url.Host)

	s.initialize()
	call := s.callTraceGet()
	s.stop()

	if call.IsError {
		t.Fatalf("execute_read through the proxy failed: %+v\nstderr:\n%s", call.StructuredContent, s.stderr)
	}
	connects, auths := proxy.seen()
	assertTunnelledOnly(t, connects)
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	if slices.ContainsFunc(auths, func(a string) bool { return a != want }) {
		t.Errorf("the proxy received Proxy-Authorization %q, want %q on every CONNECT", auths, want)
	}
	got := proxyLogLine(t, s.stderr.Bytes())
	if got["endpoint"] != "http://"+proxy.url.Host || got["variable"] != "HTTPS_PROXY" ||
		got["source"] != "environment" || got["noProxySet"] != false {
		t.Errorf("proxy log line = %v, want endpoint http://%s, variable HTTPS_PROXY, source environment, noProxySet false",
			got, proxy.url.Host)
	}
	for _, secret := range []string{user, password} {
		if strings.Contains(s.stderr.String(), secret) {
			t.Errorf("stderr contains the proxy credential %q:\n%s", secret, s.stderr)
		}
	}
}

func TestExecutableBypassesTheProxyForAHostInNoProxy(t *testing.T) {
	t.Parallel()
	ca, caFile := newCertAuthority(t, "proxy test CA")
	proxy := newConnectProxy(t, tunnelLangfuse(t, ca).Listener.Addr().String())
	s := startStdio(t, "LANGFUSE_BASE_URL=https://"+tunnelHost, "LANGFUSE_CA_CERT="+caFile,
		"HTTPS_PROXY="+proxy.url.String(), "NO_PROXY="+tunnelHost)

	s.initialize()
	call := s.callTraceGet()
	s.stop()

	// Bypassed, the executable connects directly, and tunnelHost never resolves.
	toolErr, _ := call.StructuredContent["error"].(map[string]any)
	if code, _ := toolErr["code"].(string); !call.IsError || code != "network_error" {
		t.Errorf("tools/call = %+v, want a network_error: %s is reached directly", call, tunnelHost)
	}
	if connects, _ := proxy.seen(); len(connects) != 0 {
		t.Errorf("the proxy saw CONNECT %v, want none: NO_PROXY lists %s", connects, tunnelHost)
	}
	if got := proxyLogLine(t, s.stderr.Bytes()); got["noProxySet"] != true {
		t.Errorf("proxy log line = %v, want noProxySet true", got)
	}
}

func TestExecutableLogsThatNoProxyIsSet(t *testing.T) {
	t.Parallel()

	stderr, err := runExecutable(t)
	if err != nil {
		t.Fatalf("executable did not exit 0: %v\nstderr:\n%s", err, stderr)
	}

	if got := proxyLogLine(t, stderr); got["endpoint"] != "none" || got["noProxySet"] != false {
		t.Errorf("proxy log line = %v, want endpoint none, noProxySet false", got)
	}
}

func TestStartupFailsNamingAnInvalidProxyVariableButNeverItsValue(t *testing.T) {
	t.Parallel()
	const value = "htps://proxyuser:proxysecret-9c1e@proxy.example.com:8080" //nolint:gosec // G101: a fake proxy credential

	stderr, err := runExecutable(t, "HTTPS_PROXY="+value)

	if err == nil {
		t.Fatalf("executable exited 0 with an invalid HTTPS_PROXY; stderr:\n%s", stderr)
	}
	if msg := startupError(t, stderr); !strings.Contains(msg, "HTTPS_PROXY") || !strings.Contains(msg, "environment") {
		t.Errorf("startup error %q does not name HTTPS_PROXY and its source", msg)
	}
	for _, leak := range []string{value, "proxysecret-9c1e", "proxy.example.com"} {
		if strings.Contains(string(stderr), leak) {
			t.Errorf("stderr contains %q from the proxy value:\n%s", leak, stderr)
		}
	}
}

// Ticket #45 (spec #58), seam S3: a proxy set in the config file reaches the
// Langfuse client, in either spelling; the environment wins over the file.

// throughTheProxy calls execute_read on a session and checks the call
// succeeded through proxy, with exactly one CONNECT to tunnelHost.
func throughTheProxy(t *testing.T, s *stdioSession, proxy *connectProxy) {
	t.Helper()
	s.initialize()
	call := s.callTraceGet()
	s.stop()
	if call.IsError {
		t.Fatalf("execute_read through the proxy failed: %+v\nstderr:\n%s", call.StructuredContent, s.stderr)
	}
	connects, _ := proxy.seen()
	assertTunnelledOnly(t, connects)
}

// assertTunnelledOnly checks that the proxy saw at least one CONNECT, and
// only to tunnelHost:443. Startup's deployment profile detection sends its
// probes in parallel, so there may be several connections.
func assertTunnelledOnly(t *testing.T, connects []string) {
	t.Helper()
	if len(connects) == 0 || slices.ContainsFunc(connects, func(c string) bool { return c != tunnelHost+":443" }) {
		t.Errorf("the proxy saw CONNECT %v, want at least one, each to %s:443", connects, tunnelHost)
	}
}

func TestExecutableReachesLangfuseThroughTheProxyInItsConfigFile(t *testing.T) {
	t.Parallel()
	ca, caFile := newCertAuthority(t, "proxy test CA")
	proxy := newConnectProxy(t, tunnelLangfuse(t, ca).Listener.Addr().String())
	// hermeticEnv leaves every proxy variable of the environment empty.
	s := startStdioWithConfigFile(t, "HTTPS_PROXY="+proxy.url.String()+"\n",
		"LANGFUSE_BASE_URL=https://"+tunnelHost, "LANGFUSE_CA_CERT="+caFile)

	throughTheProxy(t, s, proxy)

	got := proxyLogLine(t, s.stderr.Bytes())
	if got["endpoint"] != "http://"+proxy.url.Host || got["variable"] != "HTTPS_PROXY" || got["source"] != "config-file" {
		t.Errorf("proxy log line = %v, want endpoint http://%s, variable HTTPS_PROXY, source config-file", got, proxy.url.Host)
	}
}

func TestExecutableHonoursALowerCaseProxyKeyInItsConfigFileWithoutAWarning(t *testing.T) {
	t.Parallel()
	ca, caFile := newCertAuthority(t, "proxy test CA")
	proxy := newConnectProxy(t, tunnelLangfuse(t, ca).Listener.Addr().String())
	s := startStdioWithConfigFile(t, "https_proxy="+proxy.url.String()+"\n",
		"LANGFUSE_BASE_URL=https://"+tunnelHost, "LANGFUSE_CA_CERT="+caFile)

	throughTheProxy(t, s, proxy)

	for _, line := range logLines(t, s.stderr.Bytes()) {
		if line["level"] == "WARN" {
			t.Errorf("startup logged a warning for the lower-case proxy key: %v", line)
		}
	}
	if got := proxyLogLine(t, s.stderr.Bytes()); got["variable"] != "https_proxy" || got["source"] != "config-file" {
		t.Errorf("proxy log line = %v, want variable https_proxy, source config-file", got)
	}
}

func TestExecutableUsesTheEnvironmentsProxyOverTheConfigFiles(t *testing.T) {
	t.Parallel()
	ca, caFile := newCertAuthority(t, "proxy test CA")
	langfuseAddr := tunnelLangfuse(t, ca).Listener.Addr().String()
	envProxy, fileProxy := newConnectProxy(t, langfuseAddr), newConnectProxy(t, langfuseAddr)
	// The file's upper-case spelling would win over the environment's lower
	// case in Go's order if the file were exported regardless: it is not.
	s := startStdioWithConfigFile(t, "HTTPS_PROXY="+fileProxy.url.String()+"\n",
		"LANGFUSE_BASE_URL=https://"+tunnelHost, "LANGFUSE_CA_CERT="+caFile, "https_proxy="+envProxy.url.String())

	throughTheProxy(t, s, envProxy)

	if connects, _ := fileProxy.seen(); len(connects) != 0 {
		t.Errorf("the config file's proxy saw CONNECT %v, want none: the environment wins", connects)
	}
	if got := proxyLogLine(t, s.stderr.Bytes()); got["variable"] != "https_proxy" || got["source"] != "environment" {
		t.Errorf("proxy log line = %v, want variable https_proxy, source environment", got)
	}
}
