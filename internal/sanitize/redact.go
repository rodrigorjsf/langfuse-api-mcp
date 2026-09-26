package sanitize

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
)

// Redacted replaces a secret wherever the server would otherwise show it.
const Redacted = "[REDACTED]"

// keyPattern matches any Langfuse API key, the configured pair or not:
// pk-lf-… and sk-lf-….
var keyPattern = regexp.MustCompile(`[ps]k-lf-[A-Za-z0-9_-]+`)

// Redactor replaces secrets in text shown to the agent or written to a log.
// The zero Redactor still redacts every Langfuse API key.
type Redactor struct {
	secrets []string
}

// NewRedactor returns a Redactor for the given secret values, such as the key
// pair and the Authorization header built from it; empty values are ignored.
func NewRedactor(secrets ...string) Redactor {
	kept := slices.DeleteFunc(slices.Clone(secrets), func(s string) bool { return s == "" })
	// Longest first, so that a secret containing another is replaced whole.
	slices.SortFunc(kept, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return Redactor{secrets: kept}
}

// Redact returns s with every secret of r and every Langfuse API key replaced
// by Redacted.
func (r Redactor) Redact(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, Redacted)
	}
	if !strings.Contains(s, "k-lf-") {
		return s // the common case skips the regular expression
	}
	return keyPattern.ReplaceAllString(s, Redacted)
}
