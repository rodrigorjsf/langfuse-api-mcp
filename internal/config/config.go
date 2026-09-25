// Package config turns the process environment into plain settings values.
//
// It only carries values (paths, flags); it never reads certificates or opens
// connections. The executable passes these values on to the modules that act on
// them (ADR-0009).
package config

// Names of the explicit CA source variables (ADR-0006).
const (
	// EnvCACert names a PEM file holding one or more CA certificates.
	EnvCACert = "LANGFUSE_CA_CERT"
	// EnvCACertsPath names a directory of PEM files.
	EnvCACertsPath = "LANGFUSE_CA_CERTS_PATH"
)

// Config is the server's settings. The zero value means "nothing configured".
type Config struct {
	// CACert is the path of the explicit CA file, or "" when unset.
	CACert string
	// CACertsPath is the path of the explicit CA directory, or "" when unset.
	CACertsPath string
}

// Load reads the settings from env, a variable-name-to-value map (the process
// environment in production, a literal map in tests). An empty value counts as unset.
func Load(env map[string]string) Config {
	return Config{
		CACert:      env[EnvCACert],
		CACertsPath: env[EnvCACertsPath],
	}
}
