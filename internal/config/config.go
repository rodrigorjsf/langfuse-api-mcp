// Package config turns the process environment and the optional config file
// (ADR-0011) into plain settings values.
//
// It only carries values (paths, flags); it never reads certificates or opens
// connections. The executable passes these values on to the modules that act on
// them (ADR-0009).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Names of the explicit CA source variables (ADR-0006).
const (
	// EnvCACert names a PEM file holding one or more CA certificates.
	EnvCACert = "LANGFUSE_CA_CERT"
	// EnvCACertsPath names a directory of PEM files.
	EnvCACertsPath = "LANGFUSE_CA_CERTS_PATH"
	// EnvIgnoreAmbientCA, when true, drops every ambient CA source.
	EnvIgnoreAmbientCA = "LANGFUSE_MCP_IGNORE_AMBIENT_CA"
)

// Names of the ambient CA source variables (ADR-0006).
const (
	EnvSSLCertFile      = "SSL_CERT_FILE"
	EnvSSLCertDir       = "SSL_CERT_DIR"
	EnvNodeExtraCACerts = "NODE_EXTRA_CA_CERTS"
	EnvRequestsCABundle = "REQUESTS_CA_BUNDLE"
	EnvCurlCABundle     = "CURL_CA_BUNDLE"
)

// Names of the Langfuse key variables. They are secrets, so they may come only
// from the environment, never from the config file (ADR-0011).
const (
	EnvPublicKey = "LANGFUSE_PUBLIC_KEY"
	EnvSecretKey = "LANGFUSE_SECRET_KEY"
)

// Config is the server's settings. The zero value means "nothing configured".
type Config struct {
	// CACert is the path of the explicit CA file; its Value is "" when unset.
	CACert Setting
	// CACertsPath is the path of the explicit CA directory; its Value is "" when unset.
	CACertsPath Setting
	// IgnoreAmbientCA is true when the ambient CA sources must not be used.
	IgnoreAmbientCA bool
	// AmbientInFile holds the ambient CA variables set in the config file.
	AmbientInFile Ambient
	// Connection is the Langfuse host and key pair.
	Connection Connection
}

// Ambient holds the values of the ambient CA source variables; "" when unset.
//
// Load fills it from the config file only: the executable reads the
// environment's values itself, before config loads, because it must remove
// SSL_CERT_FILE/SSL_CERT_DIR from the environment first (ADR-0006).
type Ambient struct {
	SSLCertFile      string
	SSLCertDir       string
	NodeExtraCACerts string
	RequestsCABundle string
	CurlCABundle     string
}

// Lookup returns the value of the ambient variable named variable, or "".
func (a Ambient) Lookup(variable string) string {
	switch variable {
	case EnvSSLCertFile:
		return a.SSLCertFile
	case EnvSSLCertDir:
		return a.SSLCertDir
	case EnvNodeExtraCACerts:
		return a.NodeExtraCACerts
	case EnvRequestsCABundle:
		return a.RequestsCABundle
	case EnvCurlCABundle:
		return a.CurlCABundle
	}
	return ""
}

// Setting is one configured value and where it was read.
type Setting struct {
	Value  string
	Origin Origin
}

// Origin says where a setting was read.
type Origin string

const (
	// OriginEnvironment means the process environment.
	OriginEnvironment Origin = "environment"
	// OriginConfigFile means the config file.
	OriginConfigFile Origin = "config-file"
)

// File is the optional config file (ADR-0011): its location and its content.
type File struct {
	// Path is where the file is (or would be); error messages name it.
	Path string
	// Content is the file's bytes; empty when the file is absent.
	Content []byte
}

// ReadFile reads the config file langfuse-mcp/config.env below the OS user
// config directory (os.UserConfigDir): $XDG_CONFIG_HOME or ~/.config on Linux,
// ~/Library/Application Support on macOS, %AppData% on Windows. An absent
// file, or an OS with no user config directory, is not an error: it yields an
// empty File. A file that exists but cannot be read is an error.
func ReadFile() (File, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return File{}, nil // no user config directory (e.g. $HOME unset) means no config file
	}
	path := filepath.Join(dir, "langfuse-mcp", "config.env")
	content, err := os.ReadFile(path) //nolint:gosec // G304: fixed file name below the OS user config directory
	if errors.Is(err, fs.ErrNotExist) {
		return File{Path: path}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("config file %s: %w", path, err)
	}
	return File{Path: path, Content: content}, nil
}

// Load reads the settings from env, a variable-name-to-value map (the process
// environment in production, a literal map in tests), then from file for every
// setting env leaves unset. An empty value counts as unset. It fails, naming
// the variable, on a value it cannot parse and when the host or a key is
// missing. It also returns the config file lines whose key is not a known
// setting (#26), so the caller can warn about them; they never stop startup.
func Load(env map[string]string, file File) (Config, []IgnoredKey, error) {
	fromFile, ignored, err := parseFile(file)
	if err != nil {
		return Config{}, nil, err
	}
	setting := func(name string) Setting {
		if v := env[name]; v != "" {
			return Setting{Value: v, Origin: OriginEnvironment}
		}
		if v := fromFile[name]; v != "" {
			return Setting{Value: v, Origin: OriginConfigFile}
		}
		return Setting{}
	}
	cfg := Config{
		CACert:      setting(EnvCACert),
		CACertsPath: setting(EnvCACertsPath),
		AmbientInFile: Ambient{
			SSLCertFile:      fromFile[EnvSSLCertFile],
			SSLCertDir:       fromFile[EnvSSLCertDir],
			NodeExtraCACerts: fromFile[EnvNodeExtraCACerts],
			RequestsCABundle: fromFile[EnvRequestsCABundle],
			CurlCABundle:     fromFile[EnvCurlCABundle],
		},
	}
	if flag := setting(EnvIgnoreAmbientCA); flag.Value != "" {
		ignore, err := strconv.ParseBool(flag.Value)
		if err != nil && flag.Origin == OriginConfigFile {
			// File errors never quote content: the file may hold a secret by mistake.
			return Config{}, nil, fmt.Errorf("config file %s: %s: want true or false", file.Path, EnvIgnoreAmbientCA)
		}
		if err != nil {
			return Config{}, nil, fmt.Errorf("%s=%q: want true or false", EnvIgnoreAmbientCA, flag.Value)
		}
		cfg.IgnoreAmbientCA = ignore
	}
	if cfg.Connection, err = loadConnection(env, fromFile); err != nil {
		return Config{}, nil, err
	}
	return cfg, ignored, nil
}

// IgnoredKey is a config file line whose key is not a known setting, most
// likely a misspelling: Load ignores it. It carries no value, which could be a
// secret filed under a misspelled key name.
type IgnoredKey struct {
	// Line is the 1-based line number in the config file.
	Line int
	// Name is the key, cut to 64 runes and with control, invisible and bidi
	// characters escaped (Go string-literal escapes), so it cannot flood,
	// forge or hide text in a log.
	Name string
}

// isKnownFileKey reports whether key is a setting the config file may hold:
// one Load reads today or one the README documents as Planned, so a documented
// setting never draws a warning. The Langfuse keys are not: parseFile refuses them.
func isKnownFileKey(key string) bool {
	switch key {
	case EnvCACert, EnvCACertsPath, EnvIgnoreAmbientCA,
		EnvSSLCertFile, EnvSSLCertDir, EnvNodeExtraCACerts, EnvRequestsCABundle, EnvCurlCABundle,
		EnvBaseURL, EnvHost,
		// Planned (README "Certificates and proxy" and "Behavior"); no code reads them yet.
		"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "LANGFUSE_MCP_ALLOW_WRITES", "LANGFUSE_MCP_TRANSPORT":
		return true
	}
	return false
}

// parseFile returns the KEY=VALUE settings of a config file and the lines whose
// key is not a known setting. Errors name the file and line number but never
// quote the line, which could hold a secret.
func parseFile(file File) (map[string]string, []IgnoredKey, error) {
	settings := map[string]string{}
	var ignored []IgnoredKey
	n := 0
	content := strings.TrimPrefix(string(file.Content), "\uFEFF") // byte order mark some Windows editors write
	for line := range strings.Lines(content) {
		n++
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, nil, fmt.Errorf("config file %s line %d: expected KEY=VALUE", file.Path, n)
		}
		// Accept the dotenv "export KEY=VALUE" form, so it cannot hide a key either.
		k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
		for _, secret := range []string{EnvPublicKey, EnvSecretKey} {
			if strings.EqualFold(k, secret) { // any spelling: a key must never sit in the file
				return nil, nil, fmt.Errorf("config file %s line %d: %s is not allowed in the config file; "+
					"set it in the environment or in your MCP client's env block", file.Path, n, secret)
			}
		}
		if !isKnownFileKey(k) {
			ignored = append(ignored, IgnoredKey{Line: n, Name: logSafeKeyName(k)})
			continue
		}
		settings[k] = strings.TrimSpace(v)
	}
	return settings, ignored, nil
}

// maxKeyNameRunes bounds the key name an IgnoredKey carries, so a huge line
// cannot flood the log.
const maxKeyNameRunes = 64

// logSafeKeyName returns key cut to maxKeyNameRunes runes (marked with "..."),
// with every non-printable rune (control, zero-width, bidi, tag characters)
// and backslash written as a Go string-literal escape.
func logSafeKeyName(key string) string {
	cut := ""
	if runes := []rune(key); len(runes) > maxKeyNameRunes {
		key, cut = string(runes[:maxKeyNameRunes]), "..."
	}
	quoted := strconv.Quote(key)
	return quoted[1:len(quoted)-1] + cut
}
