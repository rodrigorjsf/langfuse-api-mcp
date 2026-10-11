package catalog

import "strings"

// fieldsParam is the name of the query parameter that selects the field groups
// of a row.
const fieldsParam = "fields"

// fieldGroup is one value of the fields parameter of an operation.
type fieldGroup struct {
	name string
	// holds says what the group holds when the release specs say so; the
	// guidance gives it, the hint does not.
	holds string
	// since names the release that added the group after the operation served
	// fields, "" for a group every release has. The guidance and the hint both
	// give it, so that no text offers a group a deployment may not have.
	since string
}

// fieldsGuide is what is known of the fields parameter of one operation: the
// groups, and the text of the guidance around them. The guidance of the
// parameter and the groups the hints name are both built from it, so they
// cannot name different groups (#194).
type fieldsGuide struct {
	// lead opens the guidance and ends before the colon that precedes the
	// groups.
	lead   string
	groups []fieldGroup
	// tail follows the groups: an example value, what comes back without fields
	// and what to keep in mind when using the parameter.
	tail string
}

// The facts below are those of docs/research/langfuse.md, section 1.5: the
// description of fields in the Langfuse release specs (every release from
// 3.73.0 to 4.56.0 was read, since the union catalog keeps no description of a
// parameter) and the API reference. A text names a group a later release
// added only with the release that added it, so each text is true for every
// release that serves the parameter. Nothing is checked locally: the groups
// differ between releases, and Langfuse ignores an unknown group or answers 400.
var fieldsGuides = map[string]fieldsGuide{
	// trace_list (#178, campaign finding H2): the values are those of the
	// Langfuse API reference of GET /api/public/traces (checked with the
	// langfuse-docs MCP), and the campaign saw the parameter work on 3.80.0
	// (144,257 bytes of trace_list rows against 537 with fields=core).
	// trace_get (3.166.0) has the same text in the release specs.
	"trace_list": traceFieldsGuide("rows", "row"),
	"trace_get":  traceFieldsGuide("the trace", "trace"),
	// Observations v2 (3.141.0): core is always included, and a field of a
	// group that was not asked for is absent, not null (docs/research/langfuse.md,
	// section 1.5). trace_context came with 3.175.0.
	"observations_getMany": {
		lead: "Comma-separated field groups to return",
		groups: []fieldGroup{
			{name: "core", holds: "always included"},
			{name: "basic", holds: "name, level, statusMessage and more"},
			{name: "time"},
			{name: "io", holds: "input, output"},
			{name: "metadata"},
			{name: "model"},
			{name: "usage"},
			{name: "prompt"},
			{name: "metrics"},
			{name: "trace_context", since: "3.175.0"},
		},
		tail: "; for example core,basic,usage. Without fields core and basic are returned. A field of a group that was " +
			"not asked for is absent from the row, not null: with core alone a row has no name.",
	},
	// Scores v2 (3.144.0): sessionId joined the trace properties in 3.156.0,
	// which the guidance leaves to "such as". The 400 is in the spec of every
	// release that has fields.
	"scores_get-many": {
		lead: "Comma-separated field groups to return",
		groups: []fieldGroup{
			{name: "score", holds: "the core score fields"},
			{name: "trace", holds: "properties of the score's trace, such as userId, tags and environment"},
		},
		tail: ". Without fields both are returned; score alone leaves out the trace data. A call that filters by userId " +
			"or traceTags must keep trace in fields, otherwise Langfuse answers 400.",
	},
	// Scores v3 (3.179.0): core is not a value but the fields that are always
	// returned; the specs of 3.180.0 and 3.181.0 listed core among the allowed
	// names, no later one does, so the text never suggests it. The specs before
	// 3.182.0 do not say what the groups add, so it is not said here either.
	"scoresV3_getManyV3": {
		lead:   "Comma-separated field groups to add to the core fields, which are always returned",
		groups: []fieldGroup{{name: "details"}, {name: "subject"}, {name: "annotation"}},
		tail: "; for example details,subject. Without fields only the core fields are returned. Langfuse answers an " +
			"unknown group name with 400, so send only these three.",
	},
	// Experiments (3.206.0): the specs say the names and the default, nothing more.
	"experiments_list": {
		lead:   "Comma-separated field groups to return",
		groups: []fieldGroup{{name: "core"}, {name: "metadata"}, {name: "scores"}},
		tail:   "; for example core,scores. Without fields core is returned.",
	},
	"experiments_listItems": {
		lead: "Comma-separated field groups to return",
		groups: []fieldGroup{
			{name: "core"}, {name: "dataset"}, {name: "io"}, {name: "metadata"}, {name: "itemMetadata"},
			{name: "experimentMetadata"}, {name: "scores"},
		},
		tail: "; for example core,dataset,scores. Without fields core and dataset are returned.",
	},
}

// traceFieldsGuide is the guide of the two trace operations, which share the
// text of the release specs: large says what is large without fields, small
// what the smallest answer is.
func traceFieldsGuide(large, small string) fieldsGuide {
	return fieldsGuide{
		lead: "Comma-separated field groups to return",
		groups: []fieldGroup{
			{name: "core", holds: "always included"},
			{name: "io", holds: "input, output, metadata"},
			{name: "scores"},
			{name: "observations"},
			{name: "metrics"},
		},
		tail: "; for example core,scores. Without fields every group is returned, which makes " + large +
			" large; core alone is the smallest " + small + ". A group left out comes back empty: scores and " +
			"observations as empty lists, metrics as -1 for totalCost and latency.",
	}
}

// guidance is the text describe_operation gives on the fields parameter.
func (g fieldsGuide) guidance() string {
	items := make([]string, len(g.groups))
	for i, gr := range g.groups {
		items[i] = gr.name
		if note := gr.note(); note != "" {
			items[i] += " (" + note + ")"
		}
	}
	return g.lead + ": " + joinAnd(items) + g.tail
}

// hint is the groups as a hint names them: the name of each, and the release
// that added the ones that came later.
func (g fieldsGuide) hint() string {
	items := make([]string, len(g.groups))
	for i, gr := range g.groups {
		items[i] = gr.name
		if gr.since != "" {
			items[i] += " from Langfuse " + gr.since
		}
	}
	return joinAnd(items)
}

// note is what the guidance says in brackets after the name of the group.
func (gr fieldGroup) note() string {
	var parts []string
	if gr.holds != "" {
		parts = append(parts, gr.holds)
	}
	if gr.since != "" {
		parts = append(parts, "from Langfuse "+gr.since)
	}
	return strings.Join(parts, "; ")
}

// joinAnd writes items as a list: "a", "a and b", "a, b and c".
func joinAnd(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// FieldGroups returns the field groups of the operation's fields parameter as
// a hint names them ("core, io, scores, observations and metrics"), or "" when
// the deployment's version does not serve fields on the operation. The hints
// that recommend fewer fields name these groups, and only for an operation
// that serves them (#194).
func (o Operation) FieldGroups() string {
	if !o.HasQueryParam(fieldsParam) {
		return ""
	}
	return fieldsGuides[o.ID].hint()
}
