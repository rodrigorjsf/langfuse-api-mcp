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
	"strings"

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
	// Origin says where the setting was read, e.g. "environment" or
	// "config-file"; it is only reported, never interpreted.
	Origin string
}

// Sources describes every CA source to add to the OS certificate store.
type Sources struct {
	// Explicit sources come from this server's own settings; one that cannot be
	// loaded is an error.
	Explicit []Source
	// Ambient sources come from widely used variables already present in the
	// environment or set in the config file (SSL_CERT_FILE, SSL_CERT_DIR, NODE_EXTRA_CA_CERTS,
	// REQUESTS_CA_BUNDLE, CURL_CA_BUNDLE); one that cannot be loaded is skipped
	// with a warning in the report.
	Ambient []Source
	// IgnoreAmbient drops every ambient source: only the OS roots and the
	// explicit sources are trusted. Dropped sources are still reported, marked
	// Ignored.
	IgnoreAmbient bool
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

const (
	// KindExplicit marks a source named through this server's own settings.
	KindExplicit Kind = "explicit"
	// KindAmbient marks a source named through a widely used variable, from
	// the environment or the config file.
	KindAmbient Kind = "ambient"
)

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

// SourceReport describes one CA source and what was loaded from it.
type SourceReport struct {
	Variable     string `json:"variable"`
	Path         string `json:"path"`
	Kind         Kind   `json:"kind"`
	Origin       string `json:"origin"`
	Certificates int    `json:"certificates"`
	// Warning says what could not be loaded from an ambient source: the whole
	// source (Certificates is 0) or single files of a directory. Empty when
	// everything loaded.
	Warning string `json:"warning,omitempty"`
	// Ignored is true for an ambient source dropped by IgnoreAmbient: nothing
	// was loaded from it (Certificates is 0), on purpose, so it has no warning.
	Ignored bool `json:"ignored,omitempty"`
}

// Build returns the trust pool for src, or an error naming the variable of the
// first explicit source that cannot be loaded. Ambient sources never fail
// Build: what cannot be loaded is skipped and named in the report's warning.
func Build(src Sources) (Pool, Report, error) {
	roots, origin := baseRoots()
	ambient := src.Ambient
	report := Report{Roots: origin, Sources: make([]SourceReport, 0, len(src.Explicit)+len(ambient))}
	for _, s := range src.Explicit {
		certs, _, err := load(s, true)
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
			Variable: s.Variable, Path: s.Path, Kind: KindExplicit, Origin: s.Origin, Certificates: len(certs),
		})
	}
	for _, s := range ambient {
		if src.IgnoreAmbient { // listed, so a log paste tells "ignored" from "not set"
			report.Sources = append(report.Sources, SourceReport{
				Variable: s.Variable, Path: s.Path, Kind: KindAmbient, Origin: s.Origin, Ignored: true,
			})
			continue
		}
		certs, skipped, err := load(s, false)
		if err == nil && len(certs) == 0 {
			err = fmt.Errorf("%s: %w", s.Path, errNoCertificates)
		}
		var warnings []string
		if err != nil {
			certs = nil
			warnings = append(warnings, "source skipped: "+err.Error())
		}
		for _, e := range skipped {
			warnings = append(warnings, "file skipped: "+e.Error())
		}
		for _, c := range certs {
			roots.AddCert(c)
		}
		report.Sources = append(report.Sources, SourceReport{
			Variable: s.Variable, Path: s.Path, Kind: KindAmbient, Origin: s.Origin, Certificates: len(certs),
			Warning: strings.Join(warnings, "; "),
		})
	}
	return Pool{roots: roots}, report, nil
}

// load reads every certificate of one source. A file of a directory that
// cannot be loaded is an error when strict; otherwise it is returned in
// skipped and the rest of the directory is still loaded.
func load(s Source, strict bool) (certs []*x509.Certificate, skipped []error, err error) {
	if !s.Directory {
		certs, err = readFile(s.Path)
		return certs, nil, err
	}
	entries, err := os.ReadDir(s.Path)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		found, err := readDirEntry(filepath.Join(s.Path, e.Name()))
		if err != nil {
			if strict {
				return nil, nil, err
			}
			skipped = append(skipped, err)
			continue
		}
		certs = append(certs, found...)
	}
	return certs, skipped, nil
}

// readDirEntry returns every certificate of a regular file of a CA directory,
// and nothing for other entries (subdirectories, devices).
func readDirEntry(path string) ([]*x509.Certificate, error) {
	info, err := os.Stat(path) // follows symlinks, as in OS certificate directories
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	return readFile(path)
}

// readFile returns every certificate in the PEM file at path.
func readFile(path string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: CA paths come from the operator's own settings; reading them is the point
	if err != nil {
		return nil, err
	}
	var certs []*x509.Certificate
	for block, rest := pem.Decode(data); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue // other PEM blocks (e.g. keys) are not CA certificates
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		certs = append(certs, cert)
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
