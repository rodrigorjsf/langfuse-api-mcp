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
// the variable, on a value it cannot parse.
func Load(env map[string]string, file File) (Config, error) {
	fromFile, err := parseFile(file)
	if err != nil {
		return Config{}, err
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
	}
	// The ignore flag is read from the environment only: a config-file value
	// would be quoted in the parse error, and file errors never quote content.
	if v := env[EnvIgnoreAmbientCA]; v != "" {
		ignore, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("%s=%q: want true or false", EnvIgnoreAmbientCA, v)
		}
		cfg.IgnoreAmbientCA = ignore
	}
	return cfg, nil
}

// parseFile returns the KEY=VALUE settings of a config file. Unknown keys are
// kept and ignored by Load, without a warning (see #26). Errors name the
// file and line number but never quote the line, which could hold a secret.
func parseFile(file File) (map[string]string, error) {
	settings := map[string]string{}
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
			return nil, fmt.Errorf("config file %s line %d: expected KEY=VALUE", file.Path, n)
		}
		// Accept the dotenv "export KEY=VALUE" form, so it cannot hide a key either.
		k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
		for _, secret := range []string{EnvPublicKey, EnvSecretKey} {
			if strings.EqualFold(k, secret) { // any spelling: a key must never sit in the file
				return nil, fmt.Errorf("config file %s line %d: %s is not allowed in the config file; "+
					"set it in the environment or in your MCP client's env block", file.Path, n, secret)
			}
		}
		settings[k] = strings.TrimSpace(v)
	}
	return settings, nil
}
