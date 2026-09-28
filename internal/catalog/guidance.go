package catalog

import "slices"

// PromptsListOperationID is the operation that lists prompts; its tag and
// filter parameters have static guidance (ParamGuidance).
const PromptsListOperationID = "prompts_list"

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

// ParamGuidance returns static guidance on how to fill parameter p of the
// operation, or "" when there is none: the query of metrics_metrics (#104)
// and the tag and filter parameters of prompts_list (#141). The text is
// compiled into the binary, never built from API data or caller input.
func (o Operation) ParamGuidance(p Param) string {
	switch {
	case o.isMetricsV2Query(p):
		return metricsQueryGuidance()
	case o.ID == PromptsListOperationID && p.In == "query" && p.Name == "tag":
		return promptsTagGuidance
	case o.ID == PromptsListOperationID && p.In == "query" && p.Name == "filter":
		return promptsFilterGuidance
	}
	return ""
}

// HasGuidance reports whether any parameter of the operation has guidance.
func (o Operation) HasGuidance() bool {
	return slices.ContainsFunc(o.Params, func(p Param) bool { return o.ParamGuidance(p) != "" })
}
