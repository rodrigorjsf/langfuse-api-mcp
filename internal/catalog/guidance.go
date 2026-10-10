package catalog

// guidedParam names one query parameter of one operation that has static
// guidance.
type guidedParam struct{ operationID, param string }

// promptsListOperationID is the operation that lists prompts; its tag and
// filter parameters have static guidance.
const promptsListOperationID = "prompts_list"

// promptsGetOperationID is the operation that reads one prompt; its label and
// version parameters have static guidance, and a call with neither gets a
// not-found hint about the implicit production label (ImpliesProductionLabel).
const promptsGetOperationID = "prompts_get"

// The guidance on the tag and filter query parameters of prompts_list (#141).
// The Langfuse spec describes neither, and its current API reference lists
// only tag, so a small model asked for the prompts with a tag sent filter
// instead. The guidance names tag as the tag filter and says filter is not;
// it says nothing more about filter, whose meaning is unverified. filter
// stays in the catalog: it comes from the Langfuse release spec (ADR-0004).
const (
	promptsTagGuidance    = "The tag filter: send one tag name as a string to list only the prompts that carry that tag."
	promptsFilterGuidance = "Not the tag filter: to list the prompts that carry a tag, send the tag name in tag instead."
)

// The guidance on the label and version query parameters of prompts_get
// (#178, campaign finding M4). The Langfuse spec describes neither; with no
// label and no version Langfuse serves the version labelled production, and
// the label latest, which Langfuse keeps on the newest version, is how to ask
// for that one (docs/research/langfuse.md, section on prompts). Label and
// version cannot be sent together: Langfuse answers 400.
const (
	promptsGetLabelGuidance = "Selects the version that carries this label. With no label and no version, Langfuse " +
		"returns the version labelled production, which may not be the newest one; send label latest to get the newest " +
		"version. Send label or version, not both."
	promptsGetVersionGuidance = "Selects one version by its number. With no version and no label, Langfuse " +
		"returns the version labelled production, which may not be the newest one; send label latest to get the newest " +
		"version. Send version or label, not both."
)

// traceListOperationID is the legacy operation that lists traces; its fields
// parameter has static guidance.
const traceListOperationID = "trace_list"

// The guidance on the fields query parameter of trace_list (#178, campaign
// finding H2). The union catalog keeps no description of a parameter, so
// nothing else tells an agent the values; they are those of the Langfuse API
// reference of GET /api/public/traces (checked with the langfuse-docs MCP),
// and the campaign saw the parameter work on 3.80.0 (144,257 bytes of
// trace_list rows against 537 with fields=core). Only a deployment whose
// version serves fields lists the parameter, so only there does it carry this.
const traceListFieldsGuidance = "Comma-separated field groups to return: core (always included), io (input, output, " +
	"metadata), scores, observations and metrics; for example core,scores. Without fields every group is returned, " +
	"which makes rows large; core alone is the smallest row. A group left out comes back empty: scores and " +
	"observations as empty lists, metrics as -1 for totalCost and latency."

// staticGuidance is the guidance of every parameter that has any, keyed by
// operation and query parameter: the query of the two metrics operations
// (#104, #178), the tag and filter of prompts_list (#141), the label and
// version of prompts_get and the fields of trace_list (#178). Every text is
// compiled into the binary, never built from API data or caller input; the
// metrics ones are built once, from the validator's own key list.
var staticGuidance = map[guidedParam]string{
	{MetricsOperationID, "query"}:       metricsQueryGuidance(),
	{legacyMetricsOperationID, "query"}: legacyMetricsQueryGuidance(),
	{promptsListOperationID, "tag"}:     promptsTagGuidance,
	{promptsListOperationID, "filter"}:  promptsFilterGuidance,
	{promptsGetOperationID, "label"}:    promptsGetLabelGuidance,
	{promptsGetOperationID, "version"}:  promptsGetVersionGuidance,
	{traceListOperationID, "fields"}:    traceListFieldsGuidance,
}

// ParamGuidance returns static guidance on how to fill parameter p of the
// operation, or "" when there is none. Callers pass the operation's own
// parameters (Params, the served ones), so an operation carries guidance only
// on a parameter its deployment serves.
func (o Operation) ParamGuidance(p Param) string {
	if p.In != "query" {
		return ""
	}
	return staticGuidance[guidedParam{o.ID, p.Name}]
}

// ImpliesProductionLabel reports whether r, a request of this operation,
// reads a prompt with neither a label nor a version: Langfuse then serves the
// version labelled production, and a prompt without that label is not found
// (#178, campaign finding M4).
func (o Operation) ImpliesProductionLabel(r Request) bool {
	return o.ID == promptsGetOperationID && !r.Query.Has("label") && !r.Query.Has("version")
}
