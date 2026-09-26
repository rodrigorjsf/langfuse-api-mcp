package langfuse

import (
	"regexp"
	"slices"
)

// Family is an operation family: a group of operations a deployment turns on
// or off together through its write mode (ADR-0012 §2).
type Family string

// The operation families of ADR-0012 §2, in the order they are listed.
const (
	LegacyFamily      Family = "legacy"
	V4ReadFamily      Family = "v4 read"
	ExperimentsFamily Family = "experiments"
)

// Families lists every operation family, in a fixed order.
func Families() []Family {
	return []Family{LegacyFamily, V4ReadFamily, ExperimentsFamily}
}

// DeploymentProfile is the connected deployment's detected Langfuse version
// plus the operation families that answered at startup (ADR-0012 §3). It is
// a plain value, fixed for the process lifetime.
type DeploymentProfile struct {
	// Version is the version /api/public/health reported, or "" when it is
	// unknown. It comes from Langfuse, so it is untrusted: read it through
	// KnownVersion, never directly into text shown to the agent.
	Version string
	// Families are the operation families that are on.
	Families []Family
}

// UnknownProfile is the profile when nothing was detected: the version is
// unknown and every family stays on, so that nothing is hidden (ADR-0012 §3).
func UnknownProfile() DeploymentProfile {
	return DeploymentProfile{Families: Families()}
}

// versionPattern accepts a plain major.minor.patch version, such as "3.80.0".
var versionPattern = regexp.MustCompile(`^[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}$`)

// KnownVersion returns the version and true when it is a plain
// major.minor.patch version; anything else, including instruction-like text
// or hidden characters, is reported as unknown and never returned.
func (p DeploymentProfile) KnownVersion() (string, bool) {
	if !versionPattern.MatchString(p.Version) {
		return "", false
	}
	return p.Version, true
}

// On reports whether family f is on.
func (p DeploymentProfile) On(f Family) bool {
	return slices.Contains(p.Families, f)
}
