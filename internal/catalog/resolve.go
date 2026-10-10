package catalog

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
)

// Family is an operation family: a group of operations a Langfuse deployment
// turns on or off together through its migration mode
// (LANGFUSE_MIGRATION_V4_WRITE_MODE, ADR-0012 §2) — not the server's own
// write mode.
type Family string

// The operation families of ADR-0012 §2.
const (
	LegacyFamily      Family = "legacy"
	V4ReadFamily      Family = "v4 read"
	ExperimentsFamily Family = "experiments"
)

// AllFamilies lists every operation family, in a fixed order.
func AllFamilies() []Family { return []Family{LegacyFamily, V4ReadFamily, ExperimentsFamily} }

// Profile is the deployment profile an operation set is resolved for: the
// Langfuse version and the operation families that are on (ADR-0012 §3).
type Profile struct {
	// Version is the deployment's Langfuse version, "major.minor.patch"; any
	// other text, including "", is an unknown version.
	Version string
	// Families are the operation families that are on.
	Families []Family
}

// Resolve returns the catalog of the operations the deployment profile p
// serves, chosen from the whole union catalog, whatever c was resolved for.
// It keeps an operation when p's version is in its range, then when its
// family, if any, is on:
//   - an unknown version keeps every range, so nothing is hidden;
//   - a version newer than the newest known one is the newest known one;
//   - a version below SupportedFloor, the union's oldest known one, is
//     filtered by the ranges alone, taken at the floor, since the union
//     knows nothing older; its families are ignored.
//
// When several kept operations share an ID, the one introduced last wins.
//
// Each kept operation offers only its served parameters (#173): those whose
// range holds the version, taken as above; an unknown version is the newest
// known one, whose spec lists the parameters no later release removed. The
// operation's other parameters stay known to Request, which refuses them
// naming the release that serves them.
func (c Catalog) Resolve(p Profile) Catalog {
	u := c.union
	at, known := parseVersion(p.Version)
	byFamily := true
	if known && !u.newest.isZero() && u.newest.less(at) {
		at = u.newest
	}
	if known && at.less(floor) {
		at, byFamily = floor, false
	}
	paramVersion, deployment := at, ""
	if known {
		deployment = p.Version
	} else {
		paramVersion = u.newest
	}
	byID := map[string]rangedOperation{}
	for _, op := range u.ops {
		if known && !op.span.contains(at) {
			continue
		}
		if byFamily && op.Family != "" && !slices.Contains(p.Families, op.Family) {
			continue
		}
		if prev, ok := byID[op.ID]; ok && !prev.span.introduced.less(op.span.introduced) {
			continue
		}
		byID[op.ID] = op
	}
	out := Catalog{union: u, byID: make(map[string]Operation, len(byID))}
	for id, op := range byID {
		out.byID[id] = op.withServedParams(paramVersion, deployment)
	}
	return out
}

// union is the whole union catalog: every in-scope operation of every known
// release, and the newest release version it was built from (zero when
// unknown); its oldest is SupportedFloor.
type union struct {
	ops    []rangedOperation
	newest version
}

// rangedOperation is an operation and its parsed version range, and the ranges
// of its parameters, in the order of Operation.Params.
type rangedOperation struct {
	Operation
	span       span
	paramSpans []span
}

// withServedParams returns the operation with the parameters version v serves in
// Params and the others in unserved; deployment is the deployment's plain
// version, "" when it was not detected.
func (o rangedOperation) withServedParams(v version, deployment string) Operation {
	out := o.Operation
	out.deployment = deployment
	out.Params, out.unserved = nil, nil
	for i, p := range o.Params {
		if o.paramSpans[i].serves(v) {
			out.Params = append(out.Params, p)
		} else {
			out.unserved = append(out.unserved, p)
		}
	}
	return out
}

// span is a version range: from introduced (zero: no lower bound) up to, not
// including, removed (zero: no upper bound).
type span struct{ introduced, removed version }

func (s span) contains(v version) bool {
	return !v.less(s.introduced) && (s.removed.isZero() || v.less(s.removed))
}

// serves reports whether a parameter with range s is served at version v.
// The zero version, which no release has, is the newest known one that is
// unknown: it serves the parameters no release removed.
func (s span) serves(v version) bool {
	if v.isZero() {
		return s.removed.isZero()
	}
	return s.contains(v)
}

// rangeOf parses an operation's version range.
func rangeOf(o Operation) (span, error) { return parseSpan(o.Introduced, o.Removed) }

// parameterRange parses a parameter's version range.
func parameterRange(p Param) (span, error) { return parseSpan(p.Introduced, p.Removed) }

// parseSpan parses the introduced and removed versions of a union catalog
// entry; "" is no bound.
func parseSpan(introduced, removed string) (span, error) {
	from, err := optionalVersion(introduced)
	if err != nil {
		return span{}, err
	}
	to, err := optionalVersion(removed)
	return span{introduced: from, removed: to}, err
}

// optionalVersion parses a version of the union catalog; "" is the zero
// version (none).
func optionalVersion(s string) (version, error) {
	if s == "" {
		return version{}, nil
	}
	v, ok := parseVersion(s)
	if !ok {
		return version{}, fmt.Errorf("version %q is not major.minor.patch", s)
	}
	return v, nil
}

// version is a parsed Langfuse release version; the zero value means none.
type version [3]int

// versionPattern is the one version grammar (#94): a plain major.minor.patch
// of at most five digits per part, such as "3.80.0".
var versionPattern = regexp.MustCompile(`^([0-9]{1,5})\.([0-9]{1,5})\.([0-9]{1,5})$`)

// SupportedFloor is the oldest supported Langfuse version (ADR-0012 §4) and
// the oldest release the union catalog is built from: the one Go definition
// (#94). The generator's FLOOR (scripts/gen-union-catalog.py) must match;
// load refuses a union catalog whose x-oldest-version differs.
const SupportedFloor = "3.0.0"

// floor is SupportedFloor parsed; an unparsable floor stops the program.
var floor = mustParseVersion(SupportedFloor)

func mustParseVersion(s string) version {
	v, ok := parseVersion(s)
	if !ok {
		panic("catalog: version " + s + " is not major.minor.patch")
	}
	return v
}

// IsPlainVersion reports whether s is a plain major.minor.patch version.
// Anything else, including instruction-like text or hidden characters, is
// not, so only a plain version may reach text shown to the agent or logged.
func IsPlainVersion(s string) bool {
	_, ok := parseVersion(s)
	return ok
}

// BelowSupportedFloor reports whether s is a plain version older than
// SupportedFloor; any other text is not below it.
func BelowSupportedFloor(s string) bool {
	v, ok := parseVersion(s)
	return ok && v.less(floor)
}

// parseVersion parses a plain "major.minor.patch" version.
func parseVersion(s string) (version, bool) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return version{}, false
	}
	var v version
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1]) // at most 5 digits: always fits
	}
	return v, true
}

func (v version) less(w version) bool { return slices.Compare(v[:], w[:]) < 0 }

func (v version) isZero() bool { return v == version{} }
