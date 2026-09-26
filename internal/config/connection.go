package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"strings"
)

// Names of the host variables: LANGFUSE_BASE_URL is the Langfuse docs'
// current name, LANGFUSE_HOST is accepted as an alias.
const (
	EnvBaseURL = "LANGFUSE_BASE_URL"
	EnvHost    = "LANGFUSE_HOST"
)

// Connection is how the server reaches Langfuse: the host and the key pair.
type Connection struct {
	// Host is the Langfuse base URL; the API lives under <host>/api/public.
	Host *url.URL
	// PublicKey and SecretKey are the key pair, read only from the environment.
	PublicKey Secret
	SecretKey Secret
}

// Secret holds a credential. It never prints its value: String, GoString,
// LogValue and MarshalJSON all return [REDACTED]. Reveal returns the value
// for the one place that must send it.
type Secret struct {
	value string
}

const redacted = "[REDACTED]"

// Reveal returns the secret value.
func (s Secret) Reveal() string { return s.value }

// String returns [REDACTED], so %v and %s never print the value.
func (Secret) String() string { return redacted }

// GoString returns [REDACTED], so %#v never prints the value.
func (Secret) GoString() string { return redacted }

// LogValue returns [REDACTED], so slog never logs the value.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON returns "[REDACTED]", so JSON output never holds the value.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// loadConnection reads the connection settings. The host comes from
// LANGFUSE_BASE_URL, then its alias LANGFUSE_HOST, in the environment first,
// then in the config file; the keys come only from the environment (the config
// file refuses them). It fails naming the first missing or invalid variable.
func loadConnection(env, fromFile map[string]string) (Connection, error) {
	var hostVar, host string
	for _, source := range []map[string]string{env, fromFile} {
		for _, name := range []string{EnvBaseURL, EnvHost} {
			if v := source[name]; v != "" && host == "" {
				hostVar, host = name, v
			}
		}
	}
	if host == "" {
		return Connection{}, fmt.Errorf("%s is not set (%s is accepted as an alias): set it to your Langfuse host, "+
			"e.g. https://cloud.langfuse.com", EnvBaseURL, EnvHost)
	}
	u, err := url.Parse(host)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		// The value is not quoted: an operator may have pasted credentials into it.
		return Connection{}, fmt.Errorf("%s: want an absolute http or https URL, e.g. https://cloud.langfuse.com", hostVar)
	}
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		// The keys travel in every request: plain http is only safe when the
		// request never leaves this machine.
		return Connection{}, fmt.Errorf("%s: want an https URL; plain http is accepted only for a loopback host "+
			"(localhost, 127.0.0.0/8, ::1)", hostVar)
	}
	conn := Connection{Host: u, PublicKey: Secret{env[EnvPublicKey]}, SecretKey: Secret{env[EnvSecretKey]}}
	for _, key := range []struct {
		name   string
		secret Secret
	}{{EnvPublicKey, conn.PublicKey}, {EnvSecretKey, conn.SecretKey}} {
		if key.secret.value == "" {
			return Connection{}, fmt.Errorf("%s is not set: set it in the environment or in your MCP client's env block", key.name)
		}
	}
	return conn, nil
}

// isLoopback reports whether host, a URL host name without port, is
// "localhost" or a loopback IP address. Any other name, even one that
// resolves to a loopback address, is not: resolution can change.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
