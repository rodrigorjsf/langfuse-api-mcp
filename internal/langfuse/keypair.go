package langfuse

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
)

// redacted replaces the key pair wherever it, Options or Client are printed.
const redacted = "[REDACTED]"

// KeyPair is the Langfuse public/secret key pair the client sends as HTTP
// Basic auth. Every rendering of it — fmt with any verb, JSON, slog — shows
// [REDACTED]. The keys sit behind a pointer, so a struct that holds a KeyPair
// in an unexported field prints an address, never a key. Only the client's
// auth header and RedactionValues read them.
type KeyPair struct {
	keys *keys
}

type keys struct {
	public, secret string
}

// NewKeyPair returns the key pair of a Langfuse project.
func NewKeyPair(publicKey, secretKey string) KeyPair {
	return KeyPair{keys: &keys{public: publicKey, secret: secretKey}}
}

// reveal returns the keys; the zero KeyPair has two empty keys.
func (k KeyPair) reveal() (public, secret string) {
	if k.keys == nil {
		return "", ""
	}
	return k.keys.public, k.keys.secret
}

// RedactionValues returns every value a redactor must hide for this pair: the
// public key, the secret key, their Basic auth value and the Authorization
// header value built from it.
func (k KeyPair) RedactionValues() []string {
	public, secret := k.reveal()
	basic := base64.StdEncoding.EncodeToString([]byte(public + ":" + secret))
	return []string{public, secret, basic, "Basic " + basic}
}

// String keeps the keys out of %v and %s.
func (KeyPair) String() string { return redacted }

// GoString keeps the keys out of %#v.
func (KeyPair) GoString() string { return redacted }

// Format keeps the keys out of every fmt verb, %q and %x included.
func (KeyPair) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// LogValue keeps the keys out of slog.
func (KeyPair) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON keeps the keys out of JSON.
func (KeyPair) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
