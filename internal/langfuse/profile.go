package langfuse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
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

// AllFamilies lists every operation family, in a fixed order.
func AllFamilies() []Family {
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
	return DeploymentProfile{Families: AllFamilies()}
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

// DefaultDetectionBudget is the total time DetectProfile waits for Langfuse at
// startup (ADR-0012 amendment): a slow or unreachable Langfuse never holds up
// the MCP host's initialize for longer.
const DefaultDetectionBudget = 5 * time.Second

// supportedFloor is the oldest supported Langfuse version, v3.0.0 (ADR-0012 §4).
const supportedFloor = 3

// Detection is the outcome of DetectProfile: the deployment profile and one
// warning per probe that could not decide.
type Detection struct {
	Profile  DeploymentProfile
	Warnings []ProbeWarning
}

// ProbeWarning says why a probe left its part of the profile undecided.
type ProbeWarning struct {
	// Probe is "health" or the name of the family whose sentinel it was.
	Probe string
	// Reason is fixed text chosen by the client: it never holds anything
	// Langfuse sent.
	Reason string
}

// sentinel is the probe that decides one family (ADR-0012 §2).
type sentinel struct {
	family Family
	path   string
	query  url.Values
}

// sentinels returns the probe of each family, in AllFamilies order.
func (c *Client) sentinels() []sentinel {
	return []sentinel{
		{LegacyFamily, "/api/public/traces", url.Values{"limit": {"1"}}},
		{V4ReadFamily, "/api/public/v2/observations", url.Values{"limit": {"1"}, "fields": {"core"}}},
		{ExperimentsFamily, "/api/public/experiments", url.Values{
			"limit": {"1"}, "fromStartTime": {c.now().UTC().Format(time.RFC3339)},
		}},
	}
}

// DetectProfile detects the deployment profile (ADR-0012 §3): the version from
// an unauthenticated GET /api/public/health, and each family from one
// sentinel. The requests run in parallel, once each, within the client's
// limits, and all end within budget. A family is off only when its sentinel
// answers a 404 that means "not served here" (an HTML body, or JSON naming
// events_only or a v4 write mode); 200 or 400 turns it on; anything else,
// including no answer within budget, leaves it on with a warning. A health
// answer without a plain major.minor.patch version leaves the version unknown
// with a warning. /v2/metrics is never requested: it spends the Cloud Hobby
// plan's daily metrics budget.
func (c *Client) DetectProfile(ctx context.Context, budget time.Duration) Detection {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	sentinels := c.sentinels()
	reasons := make([]string, len(sentinels))
	on := make([]bool, len(sentinels))
	var version, healthReason string
	// One goroutine per probe, bounded by their fixed number; each ends
	// with ctx at the latest. No probe returns an error: each one decides.
	var g errgroup.Group
	g.SetLimit(1 + len(sentinels))
	g.Go(func() error { version, healthReason = c.detectVersion(ctx); return nil })
	for i, s := range sentinels {
		g.Go(func() error { on[i], reasons[i] = c.probe(ctx, s); return nil })
	}
	_ = g.Wait() // always nil: see above

	d := Detection{Profile: DeploymentProfile{Version: version, Families: []Family{}}}
	if healthReason != "" {
		d.Warnings = append(d.Warnings, ProbeWarning{Probe: "health", Reason: healthReason})
	}
	for i, s := range sentinels {
		if on[i] {
			d.Profile.Families = append(d.Profile.Families, s.family)
		}
		if reasons[i] != "" {
			d.Warnings = append(d.Warnings, ProbeWarning{Probe: string(s.family), Reason: reasons[i]})
		}
	}
	return d
}

// detectVersion asks health for the version. It returns the version only
// when it is a plain major.minor.patch version, and a warning reason
// otherwise, or when the version is below the supported floor.
func (c *Client) detectVersion(ctx context.Context) (version, reason string) {
	resp, err := c.attempt(ctx, http.MethodGet, "/api/public/health", nil, false)
	switch {
	case errors.Is(err, errNotJSON):
		return "", "version unknown: the health answer is not JSON"
	case err != nil:
		return "", "version unknown: health " + failureReason(err)
	}
	var health struct {
		Version string `json:"version"`
	}
	err = json.NewDecoder(bytes.NewReader(resp.Body)).Decode(&health)
	if _, known := (DeploymentProfile{Version: health.Version}).KnownVersion(); err != nil || !known {
		return "", "version unknown: health reported no plain major.minor.patch version"
	}
	major, _, _ := strings.Cut(health.Version, ".")
	if n, _ := strconv.Atoi(major); n < supportedFloor { // at most 5 digits: always parses
		return health.Version, "unsupported Langfuse version: below the supported floor 3.0.0; operations are filtered by version range alone"
	}
	return health.Version, ""
}

// probe sends one family's sentinel and reports whether the family is on,
// with a warning reason when the answer did not decide it.
func (c *Client) probe(ctx context.Context, s sentinel) (on bool, reason string) {
	_, err := c.attempt(ctx, http.MethodGet, s.path, s.query, true)
	var apiErr *APIError
	switch {
	case err == nil, errors.Is(err, ErrResponseTooLarge), errors.Is(err, errNotJSON):
		return true, "" // a 2xx: the route answers
	case errors.Is(err, ErrOperationUnavailable):
		return false, ""
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest:
		return true, "" // the route exists; only the probe's parameters were refused
	default:
		return true, "family kept on: sentinel " + failureReason(err)
	}
}

// failureReason names, in fixed words, why a probe got no deciding answer.
func failureReason(err error) string {
	var apiErr *APIError
	switch {
	case errors.As(err, &apiErr):
		return "answered HTTP " + strconv.Itoa(apiErr.Status)
	case errors.Is(err, ErrTimeout):
		return "gave no answer within the detection budget"
	case errors.Is(err, ErrUntrustedCertificate), errors.Is(err, ErrCertificateRejected):
		return "gave no answer: the server certificate failed verification"
	case errors.Is(err, ErrNetwork):
		return "gave no answer: the host could not be reached"
	default:
		return "gave no answer: the request failed"
	}
}
