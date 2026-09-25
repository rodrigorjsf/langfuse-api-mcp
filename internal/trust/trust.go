// Package trust builds the trust pool: the OS certificate store plus every
// certificate from the configured CA sources (ADR-0006).
package trust

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/x509roots/fallback/bundle"
)

// Source is one CA source: a PEM file (which may hold several certificates) or
// a directory of PEM files.
type Source struct {
	// Variable names the setting the source came from, e.g. LANGFUSE_CA_CERT.
	Variable string
	// Path is the file or directory path.
	Path string
	// Directory is true when Path is a directory: every regular file directly
	// inside it (not in subdirectories) that holds PEM certificates is loaded.
	Directory bool
}

// Sources describes every CA source to add to the OS certificate store.
type Sources struct {
	// Explicit sources come from this server's own settings; one that cannot be
	// loaded is an error.
	Explicit []Source
}

var errNoCertificates = errors.New("no PEM certificate found")

// Pool is the trust pool, built once at startup and shared read-only.
type Pool struct {
	roots *x509.CertPool
}

// TLSConfig returns a new client TLS configuration that trusts exactly the pool.
func (p Pool) TLSConfig() *tls.Config {
	return &tls.Config{RootCAs: p.roots, MinVersion: tls.VersionTLS12}
}

// Kind says how a CA source was configured.
type Kind string

// KindExplicit marks a source named through this server's own settings.
const KindExplicit Kind = "explicit"

// Report describes what Build loaded, for the startup log. It holds paths and
// counts only, never certificate contents.
type Report struct {
	// Roots says where the base roots came from: the OS, or the bundled fallback.
	Roots Roots `json:"roots"`
	// Sources lists the CA sources appended to the base roots, in order.
	Sources []SourceReport `json:"sources"`
}

// Roots names the origin of the base roots the CA sources are appended to.
type Roots string

const (
	// RootsOS means the OS certificate store (or platform verifier) is used.
	RootsOS Roots = "os"
	// RootsFallback means the OS offered no certificates, so the roots bundled
	// into the binary are used instead (e.g. a minimal container image).
	RootsFallback Roots = "bundled-fallback"
)

// SourceReport describes one loaded CA source.
type SourceReport struct {
	Variable     string `json:"variable"`
	Path         string `json:"path"`
	Kind         Kind   `json:"kind"`
	Certificates int    `json:"certificates"`
}

// Build returns the trust pool for src, or an error naming the variable of the
// first explicit source that cannot be loaded.
func Build(src Sources) (Pool, Report, error) {
	roots, origin := baseRoots()
	report := Report{Roots: origin}
	for _, s := range src.Explicit {
		certs, err := load(s)
		if err == nil && len(certs) == 0 {
			err = errNoCertificates
		}
		if err != nil {
			return Pool{}, Report{}, fmt.Errorf("explicit CA source %s=%s: %w", s.Variable, s.Path, err)
		}
		for _, c := range certs {
			roots.AddCert(c)
		}
		report.Sources = append(report.Sources, SourceReport{
			Variable: s.Variable, Path: s.Path, Kind: KindExplicit, Certificates: len(certs),
		})
	}
	return Pool{roots: roots}, report, nil
}

// load reads every certificate of one source.
func load(s Source) ([]*x509.Certificate, error) {
	if !s.Directory {
		return readFile(s.Path)
	}
	entries, err := os.ReadDir(s.Path)
	if err != nil {
		return nil, err
	}
	var certs []*x509.Certificate
	for _, e := range entries {
		path := filepath.Join(s.Path, e.Name())
		info, err := os.Stat(path) // follows symlinks, as in OS certificate directories
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		found, err := readFile(path)
		if err != nil {
			return nil, err
		}
		certs = append(certs, found...)
	}
	return certs, nil
}

// readFile returns every certificate in the PEM file at path.
func readFile(path string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: CA paths come from the operator's own settings; reading them is the point
	if err != nil {
		return nil, err
	}
	var certs []*x509.Certificate
	for block, rest := pem.Decode(data); block != nil; block, rest = pem.Decode(rest) {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err == nil {
			certs = append(certs, cert)
		}
	}
	return certs, nil
}

// baseRoots returns the OS roots, or the bundled fallback roots when the OS
// offers none (no system pool, or an empty one as in a minimal container).
//
// The bundle is used directly instead of importing
// golang.org/x/crypto/x509roots/fallback for its init side effect, so the
// report can say which roots are in use.
func baseRoots() (*x509.CertPool, Roots) {
	system, err := x509.SystemCertPool()
	if err == nil && !system.Equal(x509.NewCertPool()) {
		return system, RootsOS
	}
	pool := x509.NewCertPool()
	for root := range bundle.Roots() {
		cert, err := x509.ParseCertificate(root.Certificate)
		if err != nil {
			continue // the bundle is generated from parsed certificates; never expected
		}
		if root.Constraint == nil {
			pool.AddCert(cert)
		} else {
			pool.AddCertWithConstraint(cert, root.Constraint)
		}
	}
	return pool, RootsFallback
}
