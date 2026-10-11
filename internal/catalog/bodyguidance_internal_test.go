package catalog

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// guidanceSpec returns a minimal spec with a write operation of the given ID
// whose JSON body schema declares the given properties, and projects_get when
// withProjectsGet is true.
func guidanceSpec(t *testing.T, id string, properties []string, withProjectsGet bool) []byte {
	t.Helper()
	props := map[string]any{}
	for _, p := range properties {
		props[p] = map[string]any{"type": "string"}
	}
	paths := map[string]any{
		"/api/public/x": map[string]any{"post": map[string]any{
			"operationId": id,
			"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{
				"schema": map[string]any{"type": "object", "properties": props},
			}}},
		}},
	}
	if withProjectsGet {
		paths["/api/public/projects"] = map[string]any{"get": map[string]any{"operationId": "projects_get"}}
	}
	spec, err := json.Marshal(map[string]any{"paths": paths})
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	return spec
}

func guidanceOf(t *testing.T, spec []byte, id string) string {
	t.Helper()
	cat, err := load(spec)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	op, ok := cat.Lookup(id)
	if !ok {
		t.Fatalf("operation %s is not in the catalog", id)
	}
	return cat.BodyGuidance(op)
}

// The text names only what the catalog has: projects_get only when it offers
// it, projectId only when the body schema declares it.
func TestCommentsCreateBodyGuidanceNamesOnlyWhatTheCatalogAndTheBodySchemaHave(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		properties      []string
		withProjectsGet bool
		want            string
	}{
		"projects_get is not offered": {
			properties: []string{"projectId", "content"}, want: "projectId is the ID of the project the server's keys belong to.",
		},
		"the body schema has no projectId": {properties: []string{"content"}, withProjectsGet: true, want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := guidanceOf(t, guidanceSpec(t, "comments_create", tc.properties, tc.withProjectsGet), "comments_create")
			if got != tc.want {
				t.Errorf("guidance = %q, want %q", got, tc.want)
			}
		})
	}
}

// The target properties named are those of the body schema; the observationId
// clause needs both observationId and traceId.
func TestScoresCreateBodyGuidanceNamesOnlyTheTargetPropertiesTheBodySchemaHas(t *testing.T) {
	t.Parallel()
	const tail = ". A body with none of the targets, or with more than one, is refused by Langfuse with HTTP 400."
	for name, tc := range map[string]struct {
		properties []string
		want       string
	}{
		"trace and observation only": {
			properties: []string{"name", "traceId", "observationId"},
			want:       "A score needs exactly one target: a traceId; an observationId goes only with a traceId" + tail,
		},
		"trace and session": {
			properties: []string{"name", "traceId", "sessionId", "observationId"},
			want:       "A score needs exactly one target: a traceId or a sessionId; an observationId goes only with a traceId" + tail,
		},
		"session and dataset run, observationId without traceId": {
			properties: []string{"name", "sessionId", "datasetRunId", "observationId"},
			want:       "A score needs exactly one target: a sessionId or a datasetRunId" + tail,
		},
		"no target property": {properties: []string{"name", "value"}, want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := guidanceOf(t, guidanceSpec(t, "scores_create", tc.properties, false), "scores_create")
			if got != tc.want {
				t.Errorf("guidance = %q, want %q", got, tc.want)
			}
			for _, absent := range []string{"traceId", "sessionId", "datasetRunId", "observationId"} {
				if !slices.Contains(tc.properties, absent) && strings.Contains(got, absent) {
					t.Errorf("guidance %q names %s, which the body schema does not have", got, absent)
				}
			}
		})
	}
}

// An operation outside the closed list has no guidance, whatever its body
// declares.
func TestAnOperationOutsideTheClosedListHasNoBodyGuidanceWhateverItsBodyDeclares(t *testing.T) {
	t.Parallel()

	got := guidanceOf(t, guidanceSpec(t, "other_create", []string{"projectId", "traceId", "sessionId", "datasetRunId"}, true), "other_create")

	if got != "" {
		t.Errorf("guidance = %q, want none", got)
	}
}
