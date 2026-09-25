package trust

import (
	"fmt"
	"os"
	"path/filepath"
)

// Names of the ambient CA source variables (ADR-0006): widely used variables
// that other tools already read, picked up automatically when set.
const (
	// EnvSSLCertFile names a PEM file (OpenSSL/Go convention).
	EnvSSLCertFile = "SSL_CERT_FILE"
	// EnvSSLCertDir names one or more directories, separated by the OS list
	// separator (":" on Unix, ";" on Windows).
	EnvSSLCertDir = "SSL_CERT_DIR"
	// EnvNodeExtraCACerts names a PEM file (Node.js convention).
	EnvNodeExtraCACerts = "NODE_EXTRA_CA_CERTS"
	// EnvRequestsCABundle names a PEM file (Python requests convention).
	EnvRequestsCABundle = "REQUESTS_CA_BUNDLE"
	// EnvCurlCABundle names a PEM file (curl convention).
	EnvCurlCABundle = "CURL_CA_BUNDLE"
)

// originEnvironment is the Origin of the ambient sources CaptureAmbient reads
// from the process environment; callers name other origins through
// AmbientSources.
const originEnvironment = "environment"

// CaptureAmbient reads the ambient CA source variables from the process
// environment, then removes SSL_CERT_FILE and SSL_CERT_DIR from it.
//
// Go treats SSL_CERT_FILE/SSL_CERT_DIR as a replacement of its default
// certificate locations (and on macOS/Windows as a switch-off of the platform
// verifier), so they must be gone before anything touches certificate handling:
// the executable calls this first. The other three variables are only read; Go
// ignores them. Every returned source has Origin "environment".
//
// It is the only function of this module that touches the environment; Build
// takes the returned sources as plain values.
func CaptureAmbient() ([]Source, error) {
	paths := AmbientSources(os.Getenv, originEnvironment)
	for _, v := range []string{EnvSSLCertFile, EnvSSLCertDir} {
		if err := os.Unsetenv(v); err != nil {
			return nil, fmt.Errorf("remove %s from the environment: %w", v, err)
		}
	}
	return paths, nil
}

// AmbientSources returns the ambient CA sources named by the variables that
// lookup returns a value for, each tagged with origin. SSL_CERT_DIR is split
// with the OS list separator. Empty values and empty SSL_CERT_DIR entries are
// skipped. It reads nothing itself, so the executable can also pass the
// ambient variables set in the config file.
func AmbientSources(lookup func(variable string) string, origin string) []Source {
	var paths []Source
	if f := lookup(EnvSSLCertFile); f != "" {
		paths = append(paths, Source{Variable: EnvSSLCertFile, Path: f, Origin: origin})
	}
	for _, d := range filepath.SplitList(lookup(EnvSSLCertDir)) {
		if d != "" {
			paths = append(paths, Source{Variable: EnvSSLCertDir, Path: d, Directory: true, Origin: origin})
		}
	}
	for _, v := range []string{EnvNodeExtraCACerts, EnvRequestsCABundle, EnvCurlCABundle} {
		if f := lookup(v); f != "" {
			paths = append(paths, Source{Variable: v, Path: f, Origin: origin})
		}
	}
	return paths
}
