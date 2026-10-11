package catalog

import (
	"encoding/json"
	"strings"
)

// Static guidance on the body of a write operation (#192, campaign findings of
// 2026-10-10). The Langfuse spec leaves two requirements unsaid, and an agent
// learned each from a Langfuse 400. The guidance follows the rule of the
// parameter guidance (ParamGuidance): text compiled into the binary, never
// built from API data or caller input. It is also true for the connected
// deployment: it names only the properties the operation's body schema has, the
// operations the resolved catalog offers and, for a rule a release introduced,
// only a deployment whose version has it. The closed list is comments_create
// and scores_create; a test fails when another operation gains guidance.

// The operations whose body has guidance, and the one the comments text names.
const (
	commentsCreateOperationID = "comments_create"
	scoresCreateOperationID   = "scores_create"
	projectsGetOperationID    = "projects_get"
)

// scoresTargetRuleSince is the first Langfuse release whose scores_create
// takes the target properties the guidance names (traceId, sessionId and
// datasetRunId) and refuses a body without exactly one of them: the release
// specs have sessionId from 3.53.0 and datasetRunId from 3.55.0, and the
// release source holds the rule from 3.55.0 (docs/research/langfuse.md). The
// union catalog keeps the newest release's body schema for every deployment,
// so the schema alone cannot say that an older deployment lacks them.
const scoresTargetRuleSince = "3.55.0"

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

// scoresCreateGuidance says that a score needs exactly one target and names the
// target properties the body schema has: a traceId (with an optional
// observationId), a sessionId or a datasetRunId. Langfuse answers 400 to a body
// with none or with more than one, and to an observationId without a traceId.
func scoresCreateGuidance(op Operation) string {
	if !op.deploymentAtLeast(scoresTargetRuleSince) {
		return ""
	}
	var targets []string
	for _, name := range []string{"traceId", "sessionId", "datasetRunId"} {
		if op.Body.hasProperty(name) {
			targets = append(targets, "a "+name)
		}
	}
	if len(targets) == 0 {
		return ""
	}
	text := "A score needs exactly one target: " + joinOr(targets)
	if op.Body.hasProperty("observationId") && op.Body.hasProperty("traceId") {
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

// deploymentAtLeast reports whether the deployment's version is at least
// since, a plain version. A deployment whose version was not detected is the
// newest known release, so it is.
func (o Operation) deploymentAtLeast(since string) bool {
	at, known := parseVersion(o.deployment)
	return !known || !at.less(mustParseVersion(since))
}

// hasProperty reports whether the top level of the body schema declares the
// property name.
func (b *RequestBody) hasProperty(name string) bool {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(b.Schema, &s) != nil {
		return false
	}
	_, ok := s.Properties[name]
	return ok
}
