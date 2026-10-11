package catalog_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam (spec #172, ticket #192): the static guidance on the body of a write
// operation. It is the catalog's, pure data: given a resolved catalog and one
// of its operations it returns text compiled into the binary, built only from
// the properties the operation's body schema has and the operations the
// catalog offers. The expected texts are literals.

// The facts behind both texts, verified against the Langfuse release specs and
// the Langfuse sources on 2026-10-11 and recorded in docs/research/langfuse.md.
const (
	commentsCreateGuidance = "projectId is the ID of the project the server's keys belong to: projects_get returns " +
		"that project, and its id is the value to send."
	scoresCreateGuidance = "A score needs exactly one target: a traceId, a sessionId or a datasetRunId; an observationId " +
		"goes only with a traceId. A body with none of the targets, or with more than one, is refused by Langfuse " +
		"with HTTP 400."
	// scoresCreateGuidanceTraceOrSession is the text where the release has no datasetRunId (3.53.0 to 3.54.x),
	// scoresCreateGuidanceTraceOnly where it has no sessionId either (before 3.53.0, traceId required).
	scoresCreateGuidanceTraceOrSession = "A score needs exactly one target: a traceId or a sessionId; an observationId " +
		"goes only with a traceId. A body with none of the targets, or with more than one, is refused by Langfuse " +
		"with HTTP 400."
	scoresCreateGuidanceTraceOnly = "A score needs a target: a traceId, with an optional observationId. " +
		"A body without it is refused by Langfuse with HTTP 400."
)

// resolved returns the catalog resolved for a deployment of the given version
// with every operation family on; "" is a deployment whose version is unknown.
func resolved(t *testing.T, version string) catalog.Catalog {
	t.Helper()
	return mustLoad(t).Resolve(catalog.Profile{Version: version, Families: catalog.AllFamilies()})
}

func bodyGuidanceOf(t *testing.T, cat catalog.Catalog, id string) string {
	t.Helper()
	op, ok := cat.Lookup(id)
	if !ok {
		t.Fatalf("operation %s is not in the catalog", id)
	}
	return cat.BodyGuidance(op)
}

// comments_create needs a projectId that the spec does not source: it is the
// project of the server's keys, which projects_get returns (spec #172, story 57).
func TestCommentsCreateBodyGuidanceSaysProjectIdIsTheProjectOfTheKeysAndProjectsGetReturnsIt(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.0", "3.80.0", "4.46.0", ""} {
		if got := bodyGuidanceOf(t, resolved(t, version), "comments_create"); got != commentsCreateGuidance {
			t.Errorf("on %q: guidance = %q, want %q", version, got, commentsCreateGuidance)
		}
	}
}

// scores_create needs a target. The target properties exist from different
// releases (docs/research/langfuse.md, section 1.14): the text names the ones
// the deployment's version has, so it is never false there.
func TestScoresCreateBodyGuidanceNamesTheTargetPropertiesOfTheDeploymentsVersion(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]string{
		"2.0.0": scoresCreateGuidanceTraceOnly, "3.0.0": scoresCreateGuidanceTraceOnly, "3.52.0": scoresCreateGuidanceTraceOnly,
		"3.53.0": scoresCreateGuidanceTraceOrSession, "3.54.1": scoresCreateGuidanceTraceOrSession,
		"3.55.0": scoresCreateGuidance, "3.80.0": scoresCreateGuidance,
		"4.46.0": scoresCreateGuidance, "99.0.0": scoresCreateGuidance, "": scoresCreateGuidance,
	} {
		if got := bodyGuidanceOf(t, resolved(t, version), "scores_create"); got != want {
			t.Errorf("on %q: guidance = %q, want %q", version, got, want)
		}
	}
}

// No other operation gains body guidance, on any deployment: the list is
// closed, so adding one means editing this test (spec #172, ticket #192).
func TestOnlyCommentsCreateAndScoresCreateCarryBodyGuidance(t *testing.T) {
	t.Parallel()
	want := []string{"comments_create", "scores_create"}

	for _, version := range []string{"3.0.0", "3.52.0", "3.54.1", "3.55.0", "3.57.0", "3.72.0", "3.80.0", "3.225.11", "4.46.0", ""} {
		cat := resolved(t, version)
		var got []string
		for _, op := range cat.Operations() {
			if cat.BodyGuidance(op) != "" {
				got = append(got, op.ID)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("on %q: operations with body guidance = %v, want %v", version, got, want)
		}
	}
}

// An operation without a body has no body guidance.
func TestAnOperationWithoutABodyHasNoBodyGuidance(t *testing.T) {
	t.Parallel()
	cat := resolved(t, "")

	for _, op := range cat.Operations() {
		if g := cat.BodyGuidance(op); op.Body == nil && g != "" {
			t.Errorf("%s takes no body and has guidance %q", op.ID, g)
		}
	}
}

// Guidance is one line of printable text with no control, invisible or bidi
// character.
func TestEveryBodyGuidanceIsOneLineOfVisibleText(t *testing.T) {
	t.Parallel()
	cat := resolved(t, "")
	checked := 0

	for _, op := range cat.Operations() {
		g := cat.BodyGuidance(op)
		if g == "" {
			continue
		}
		checked++
		if strings.TrimSpace(g) != g {
			t.Errorf("%s guidance has leading or trailing space", op.ID)
		}
		for _, r := range g {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
				t.Errorf("%s guidance holds the character %U", op.ID, r)
			}
		}
	}
	if checked != 2 {
		t.Errorf("checked the guidance of %d operations, want 2", checked)
	}
}

// Each property or operation the guidance text names exists: a property in the
// body schema of the operation, an operation in the catalog. A catalog
// regeneration that drops one fails here.
func TestTheBodyGuidanceNamesOnlyPropertiesOfTheBodySchemaAndOperationsOfTheCatalog(t *testing.T) {
	t.Parallel()
	cat := resolved(t, "")
	candidates := []string{"projectId", "traceId", "sessionId", "datasetRunId", "observationId"}

	for _, id := range []string{"comments_create", "scores_create"} {
		op, ok := cat.Lookup(id)
		if !ok {
			t.Fatalf("operation %s is not in the catalog", id)
		}
		var schema any
		if err := json.Unmarshal(op.Body.Schema, &schema); err != nil {
			t.Fatalf("body schema of %s: %v", id, err)
		}
		properties := propertyNames(schema)
		guidance := cat.BodyGuidance(op)

		named := 0
		for _, name := range candidates {
			if !strings.Contains(guidance, name) {
				continue
			}
			named++
			if !slices.Contains(properties, name) {
				t.Errorf("the guidance of %s names %s, which its body schema does not have (%v)", id, name, properties)
			}
		}
		if named == 0 {
			t.Errorf("the guidance of %s names none of %v: %q", id, candidates, guidance)
		}
		if strings.Contains(guidance, "projects_get") {
			if _, offered := cat.Lookup("projects_get"); !offered {
				t.Errorf("the guidance of %s names projects_get, which the catalog does not offer", id)
			}
		}
	}
}
