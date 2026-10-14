package catalog

import (
	"encoding/json"
	"slices"
	"strings"
)

// Static guidance on the body of a write operation (#192, campaign findings of
// 2026-10-10). The Langfuse spec leaves two requirements unsaid, and an agent
// learned each from a Langfuse 400. The guidance follows the rule of the
// parameter guidance (ParamGuidance): text compiled into the binary, never
// built from API data or caller input. It is also true for the connected
// deployment: it names only the properties the operation's body schema has, the
// body schema of the deployment's version (#202), and the operations the resolved
// catalog offers. The closed list is comments_create and scores_create; a test
// fails when another operation gains guidance.

// The operations whose body has guidance, and the one the comments text names.
const (
	commentsCreateOperationID = "comments_create"
	scoresCreateOperationID   = "scores_create"
	projectsGetOperationID    = "projects_get"
)

// scoreTargets are the target properties of a score body. They exist from
// different releases (docs/research/langfuse.md, section 1.14: traceId from the
// start and required up to 3.52.0, sessionId from 3.53.0, datasetRunId from
// 3.55.0), so the guidance names a target only when the body schema has it:
// the body schema is the deployment's own (Resolve), and no release table is
// kept here besides the release specs.
var scoreTargets = []string{"traceId", "sessionId", "datasetRunId"}

// BodyGuidance returns static guidance on how to fill the body of op, an
// operation of this catalog, or "" when it has none. It holds no caller input
// and no Langfuse data.
func (c Catalog) BodyGuidance(op Operation) string {
	if op.Body == nil {
		return ""
	}
	switch op.ID {
	case commentsCreateOperationID:
		return c.commentsCreateGuidance(op)
	case scoresCreateOperationID:
		return scoresCreateGuidance(op)
	}
	return ""
}

// commentsCreateGuidance says where the projectId of a comment comes from: it
// is the ID of the project the server's keys belong to ("Get Project associated
// with API key" is the spec's own summary of projects_get), which projects_get
// returns as the id of that project. The spec describes it as "the id of the
// project to attach the comment to" only.
func (c Catalog) commentsCreateGuidance(op Operation) string {
	if !op.Body.hasProperty("projectId") {
		return ""
	}
	text := "projectId is the ID of the project the server's keys belong to"
	if _, offered := c.byID[projectsGetOperationID]; offered {
		return text + ": " + projectsGetOperationID + " returns that project, and its id is the value to send."
	}
	return text + "."
}

// scoresCreateGuidance says that a score needs a target and names the target
// properties the body schema has: a traceId (with an optional observationId), a
// sessionId or a datasetRunId. Langfuse answers 400 to a body with none, and,
// from 3.53.0, with more than one, and to an observationId without a traceId.
func scoresCreateGuidance(op Operation) string {
	var targets []string
	for _, t := range scoreTargets {
		if op.Body.hasProperty(t) {
			targets = append(targets, "a "+t)
		}
	}
	withTrace := op.Body.hasProperty("observationId") && slices.Contains(targets, "a traceId")
	switch {
	case len(targets) == 0:
		return ""
	case len(targets) == 1 && withTrace:
		return "A score needs a target: a traceId, with an optional observationId. A body without it is refused by Langfuse with HTTP 400."
	case len(targets) == 1:
		return "A score needs a target: " + targets[0] + ". A body without it is refused by Langfuse with HTTP 400."
	}
	text := "A score needs exactly one target: " + joinOr(targets)
	if withTrace {
		text += "; an observationId goes only with a traceId"
	}
	return text + ". A body with none of the targets, or with more than one, is refused by Langfuse with HTTP 400."
}

// joinOr joins items as "a, b or c".
func joinOr(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}

// hasProperty reports whether the object the body schema describes declares
// the property name: at its top level, or in an alternative of its oneOf,
// anyOf or allOf (from Langfuse 4.55.0 the score body is a oneOf of one score
// object and a list of them, #225).
func (b *RequestBody) hasProperty(name string) bool {
	return declaresProperty(b.Schema, name)
}

func declaresProperty(schema json.RawMessage, name string) bool {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
		OneOf      []json.RawMessage          `json:"oneOf"`
		AnyOf      []json.RawMessage          `json:"anyOf"`
		AllOf      []json.RawMessage          `json:"allOf"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return false
	}
	if _, ok := s.Properties[name]; ok {
		return true
	}
	for _, alternatives := range [][]json.RawMessage{s.OneOf, s.AnyOf, s.AllOf} {
		if slices.ContainsFunc(alternatives, func(a json.RawMessage) bool { return declaresProperty(a, name) }) {
			return true
		}
	}
	return false
}
