package trust_test

import (
	"os"
	"slices"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/trust"
)

// Not parallel: the capture step reads and changes the process environment.
func TestCaptureAmbientReadsTheFiveVariablesAndSplitsSSLCertDir(t *testing.T) {
	sep := string(os.PathListSeparator)
	t.Setenv("SSL_CERT_FILE", "/etc/corp/ssl.pem")
	t.Setenv("SSL_CERT_DIR", "/etc/corp/one"+sep+sep+"/etc/corp/two")
	t.Setenv("NODE_EXTRA_CA_CERTS", "/etc/corp/node.pem")
	t.Setenv("REQUESTS_CA_BUNDLE", "/etc/corp/requests.pem")
	t.Setenv("CURL_CA_BUNDLE", "/etc/corp/curl.pem")

	got, err := trust.CaptureAmbient()
	if err != nil {
		t.Fatalf("CaptureAmbient: %v", err)
	}

	want := []trust.Source{
		{Variable: "SSL_CERT_FILE", Path: "/etc/corp/ssl.pem", Origin: "environment"},
		{Variable: "SSL_CERT_DIR", Path: "/etc/corp/one", Directory: true, Origin: "environment"},
		{Variable: "SSL_CERT_DIR", Path: "/etc/corp/two", Directory: true, Origin: "environment"},
		{Variable: "NODE_EXTRA_CA_CERTS", Path: "/etc/corp/node.pem", Origin: "environment"},
		{Variable: "REQUESTS_CA_BUNDLE", Path: "/etc/corp/requests.pem", Origin: "environment"},
		{Variable: "CURL_CA_BUNDLE", Path: "/etc/corp/curl.pem", Origin: "environment"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("CaptureAmbient() = %+v, want %+v", got, want)
	}
}

// Not parallel: the capture step reads and changes the process environment.
func TestCaptureAmbientRemovesOnlySSLCertFileAndDirFromTheEnvironment(t *testing.T) {
	for _, v := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
		t.Setenv(v, "/etc/corp/"+v)
	}

	if _, err := trust.CaptureAmbient(); err != nil {
		t.Fatalf("CaptureAmbient: %v", err)
	}

	for _, v := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, ok := os.LookupEnv(v); ok {
			t.Errorf("%s is still in the environment (%q); Go would let it replace the OS roots", v, value)
		}
	}
	for _, v := range []string{"NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
		if _, ok := os.LookupEnv(v); !ok {
			t.Errorf("%s was removed from the environment; only SSL_CERT_FILE/SSL_CERT_DIR should be", v)
		}
	}
}

// Not parallel: the capture step reads and changes the process environment.
func TestCaptureAmbientFindsNothingWhenTheVariablesAreEmpty(t *testing.T) {
	for _, v := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
		t.Setenv(v, "")
	}

	got, err := trust.CaptureAmbient()
	if err != nil {
		t.Fatalf("CaptureAmbient: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("CaptureAmbient() = %+v, want no ambient CA sources", got)
	}
}
