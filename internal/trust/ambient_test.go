package trust_test

import (
	"encoding/pem"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/trust"
)

func TestTrustPoolTrustsAServerSignedByACAFromEachAmbientVariable(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{ // variable → names a directory
		"SSL_CERT_FILE":       false,
		"SSL_CERT_DIR":        true,
		"NODE_EXTRA_CA_CERTS": false,
		"REQUESTS_CA_BUNDLE":  false,
		"CURL_CA_BUNDLE":      false,
	}
	for variable, directory := range tests {
		t.Run(variable, func(t *testing.T) {
			t.Parallel()
			ca := newTestCA(t, "ambient root")
			addr := serveTLS(t, ca)
			dir := t.TempDir()
			path := writeFile(t, dir, "ambient-root.pem", ca.pem)
			if directory {
				path = dir
			}

			pool, report, err := trust.Build(trust.Sources{Ambient: []trust.Source{
				{Variable: variable, Path: path, Directory: directory},
			}})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			if err := handshake(t, pool.TLSConfig(), addr); err != nil {
				t.Fatalf("handshake with the CA from %s: %v", variable, err)
			}
			want := trust.SourceReport{Variable: variable, Path: path, Kind: trust.KindAmbient, Certificates: 1}
			if len(report.Sources) != 1 || report.Sources[0] != want {
				t.Fatalf("report sources = %+v, want [%+v]", report.Sources, want)
			}
		})
	}
}

func TestBuildSkipsABrokenAmbientSourceWithAWarningNamingItsPath(t *testing.T) {
	t.Parallel()
	tests := map[string]func(t *testing.T) trust.Source{
		"missing file": func(t *testing.T) trust.Source {
			return trust.Source{Variable: "NODE_EXTRA_CA_CERTS", Path: filepath.Join(t.TempDir(), "deleted.pem")}
		},
		"empty file": func(t *testing.T) trust.Source {
			return trust.Source{Variable: "REQUESTS_CA_BUNDLE", Path: writeFile(t, t.TempDir(), "empty.pem", nil)}
		},
		"damaged certificate": func(t *testing.T) trust.Source {
			damaged := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not DER")})
			return trust.Source{Variable: "CURL_CA_BUNDLE", Path: writeFile(t, t.TempDir(), "damaged.pem", damaged)}
		},
		"missing directory": func(t *testing.T) trust.Source {
			return trust.Source{Variable: "SSL_CERT_DIR", Path: filepath.Join(t.TempDir(), "gone"), Directory: true}
		},
		"directory without certificates": func(t *testing.T) trust.Source {
			return trust.Source{Variable: "SSL_CERT_DIR", Path: t.TempDir(), Directory: true}
		},
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := source(t)

			_, report, err := trust.Build(trust.Sources{Ambient: []trust.Source{src}})
			if err != nil {
				t.Fatalf("Build failed on a broken ambient source: %v", err)
			}

			if len(report.Sources) != 1 {
				t.Fatalf("report sources = %+v, want the skipped source listed", report.Sources)
			}
			got := report.Sources[0]
			if got.Kind != trust.KindAmbient || got.Certificates != 0 || !strings.Contains(got.Warning, src.Path) {
				t.Fatalf("report source = %+v, want kind ambient, 0 certificates and a warning naming %s", got, src.Path)
			}
		})
	}
}

func TestBuildKeepsTheGoodCertificatesOfAnAmbientDirectoryWithABrokenFile(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t, "ambient root")
	addr := serveTLS(t, ca)
	dir := t.TempDir()
	writeFile(t, dir, "good.pem", ca.pem)
	damaged := writeFile(t, dir, "damaged.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not DER")}))

	pool, report, err := trust.Build(trust.Sources{Ambient: []trust.Source{
		{Variable: "SSL_CERT_DIR", Path: dir, Directory: true},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if err := handshake(t, pool.TLSConfig(), addr); err != nil {
		t.Fatalf("handshake with the good CA of the directory: %v", err)
	}
	got := report.Sources[0]
	if got.Certificates != 1 || !strings.Contains(got.Warning, damaged) {
		t.Fatalf("report source = %+v, want 1 certificate and a warning naming %s", got, damaged)
	}
}

func TestTrustPoolIgnoresAmbientSourcesWhenAskedTo(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t, "ambient root")
	addr := serveTLS(t, ca)
	path := writeFile(t, t.TempDir(), "ambient-root.pem", ca.pem)

	pool, _, err := trust.Build(trust.Sources{
		Ambient:       []trust.Source{{Variable: "SSL_CERT_FILE", Path: path}},
		IgnoreAmbient: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if err := handshake(t, pool.TLSConfig(), addr); err == nil {
		t.Fatal("handshake succeeded with a CA from an ignored ambient source")
	}
}

func TestTrustPoolTrustsExplicitAndAmbientCAsAtOnce(t *testing.T) {
	t.Parallel()
	explicit, ambient := newTestCA(t, "explicit root"), newTestCA(t, "ambient root")
	explicitAddr, ambientAddr := serveTLS(t, explicit), serveTLS(t, ambient)
	dir := t.TempDir()
	explicitPath := writeFile(t, dir, "explicit.pem", explicit.pem)
	ambientPath := writeFile(t, dir, "ambient.pem", ambient.pem)

	pool, _, err := trust.Build(trust.Sources{
		Explicit: []trust.Source{{Variable: "LANGFUSE_CA_CERT", Path: explicitPath}},
		Ambient:  []trust.Source{{Variable: "NODE_EXTRA_CA_CERTS", Path: ambientPath}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for name, addr := range map[string]string{"explicit": explicitAddr, "ambient": ambientAddr} {
		if err := handshake(t, pool.TLSConfig(), addr); err != nil {
			t.Errorf("handshake with the %s CA: %v", name, err)
		}
	}
}

func TestTrustPoolTrustsEveryDirectoryOfAMultiDirectorySSLCertDir(t *testing.T) {
	t.Parallel()
	first, second := newTestCA(t, "first root"), newTestCA(t, "second root")
	firstAddr, secondAddr := serveTLS(t, first), serveTLS(t, second)
	firstDir, secondDir := t.TempDir(), t.TempDir()
	writeFile(t, firstDir, "first.pem", first.pem)
	writeFile(t, secondDir, "second.pem", second.pem)

	pool, _, err := trust.Build(trust.Sources{Ambient: []trust.Source{
		{Variable: "SSL_CERT_DIR", Path: firstDir, Directory: true},
		{Variable: "SSL_CERT_DIR", Path: secondDir, Directory: true},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for name, addr := range map[string]string{"first": firstAddr, "second": secondAddr} {
		if err := handshake(t, pool.TLSConfig(), addr); err != nil {
			t.Errorf("handshake with the CA of the %s directory: %v", name, err)
		}
	}
}

func TestReportTellsExplicitAndAmbientSourcesApart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	explicitPath := writeFile(t, dir, "explicit.pem", newTestCA(t, "explicit root").pem)
	ambientPath := writeFile(t, dir, "ambient.pem", newTestCA(t, "ambient root").pem)

	_, report, err := trust.Build(trust.Sources{
		Explicit: []trust.Source{{Variable: "LANGFUSE_CA_CERT", Path: explicitPath}},
		Ambient:  []trust.Source{{Variable: "REQUESTS_CA_BUNDLE", Path: ambientPath}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	want := []trust.SourceReport{
		{Variable: "LANGFUSE_CA_CERT", Path: explicitPath, Kind: "explicit", Certificates: 1},
		{Variable: "REQUESTS_CA_BUNDLE", Path: ambientPath, Kind: "ambient", Certificates: 1},
	}
	if !slices.Equal(report.Sources, want) {
		t.Fatalf("report sources = %+v, want %+v", report.Sources, want)
	}
}
