package config

import (
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"net/url"
	"slices"
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

// Secret holds a credential. It never prints its value: Format (every fmt
// verb and flag), String, GoString, LogValue and MarshalJSON all render
// [REDACTED]. Reveal returns the value for the one place that must send it.
type Secret struct {
	value string
}

const redacted = "[REDACTED]"

// Reveal returns the secret value.
func (s Secret) Reveal() string { return s.value }

// Format writes [REDACTED] for every fmt verb and flag. Without it fmt calls
// String only for %v %s %x %X %q and prints the value field by reflection
// for any other verb, such as %d or %t.
func (Secret) Format(f fmt.State, _ rune) {
	// Ignored: Format has no error return; fmt records its own write errors.
	_, _ = io.WriteString(f, redacted)
}

// String returns [REDACTED], for callers that convert a Secret to a string.
func (Secret) String() string { return redacted }

// GoString returns [REDACTED], for callers that ask for a Go-syntax rendering.
func (Secret) GoString() string { return redacted }

// LogValue returns [REDACTED], so slog never logs the value.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON returns "[REDACTED]", so JSON output never holds the value.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// Every Langfuse project key pair has these prefixes (Langfuse public API
// docs: Basic auth with pk-lf-… as user and sk-lf-… as password).
const (
	publicKeyPrefix = "pk-lf-"
	secretKeyPrefix = "sk-lf-"
)

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
	keys := []struct {
		name, prefix string
		secret       Secret
	}{{EnvPublicKey, publicKeyPrefix, conn.PublicKey}, {EnvSecretKey, secretKeyPrefix, conn.SecretKey}}
	for _, key := range keys {
		if key.secret.value == "" {
			return Connection{}, fmt.Errorf("%s is not set: set it in the environment or in your MCP client's env block", key.name)
		}
	}
	// The key values are never echoed: they are credentials.
	if strings.HasPrefix(conn.PublicKey.value, secretKeyPrefix) && strings.HasPrefix(conn.SecretKey.value, publicKeyPrefix) {
		return Connection{}, fmt.Errorf("%s and %s look swapped: the public key starts with %s, the secret key with %s",
			EnvPublicKey, EnvSecretKey, publicKeyPrefix, secretKeyPrefix)
	}
	for _, key := range keys {
		if !strings.HasPrefix(key.secret.value, key.prefix) {
			return Connection{}, fmt.Errorf("%s: want a Langfuse key starting with %s; copy it from the project's API keys settings",
				key.name, key.prefix)
		}
	}
	return conn, nil
}

// isLoopback reports whether host, a URL host name without port, is
// "localhost" or a loopback IP address. Any other name, even one that
// resolves to a loopback address, is not: resolution can change. Only ASCII
// letters fold, as in isCloudHost: Unicode folding would match "localhoſt".
func isLoopback(host string) bool {
	if asciiLower(host) == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// cloudHosts are the Cloud hosts (CONTEXT.md), the hosts of the region URLs
// README "Cloud regions" lists: EU, US, JP, HIPAA.
var cloudHosts = [...]string{"cloud.langfuse.com", "us.cloud.langfuse.com", "jp.cloud.langfuse.com", "hipaa.cloud.langfuse.com"}

// isCloudHost reports whether u's host name, without port or userinfo, is
// exactly one of cloudHosts, ignoring ASCII case. The match is exact, never by
// suffix or substring, so cloud.langfuse.com.evil.example is not Cloud; a
// trailing dot is not stripped either. Only ASCII letters fold: Unicode
// folding (strings.EqualFold) would match "ſ" (U+017F) to "s".
func isCloudHost(u *url.URL) bool {
	name := asciiLower(u.Hostname())
	return slices.Contains(cloudHosts[:], name)
}

// asciiLower returns s with the ASCII letters A-Z lowered and every other
// byte unchanged.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
