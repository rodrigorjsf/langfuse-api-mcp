package sanitize_test

import (
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam S2 (spec #172, ticket #203): the words that ask for a smaller answer are
// one compiled source. The truncation hint, the hint of response_too_large and
// the hint of a timeout or canceled call all name the ways of narrowing from
// NarrowWays, so they cannot drift apart. Expected texts are literals.

// NarrowWays names fields, a lower limit and a shorter time window, in that
// order, each only when the operation has it; none is "".
func TestNarrowWaysNamesEachWayOfNarrowingTheOperationHasAndNoOther(t *testing.T) {
	t.Parallel()
	const groups = "core, io and metrics"
	tests := map[string]struct {
		remedies sanitize.Remedies
		want     string
	}{
		"none":              {sanitize.Remedies{}, ""},
		"fields alone":      {sanitize.Remedies{FieldGroups: groups}, "fewer field groups in fields (core, io and metrics)"},
		"limit alone":       {sanitize.Remedies{Limit: 100}, "a lower limit"},
		"time window alone": {sanitize.Remedies{TimeWindow: true}, "a shorter time window"},
		"fields and limit": {sanitize.Remedies{FieldGroups: groups, Limit: 100},
			"fewer field groups in fields (core, io and metrics) or a lower limit"},
		"fields and window": {sanitize.Remedies{FieldGroups: groups, TimeWindow: true},
			"fewer field groups in fields (core, io and metrics) or a shorter time window"},
		"limit and window": {sanitize.Remedies{Limit: 100, TimeWindow: true}, "a lower limit or a shorter time window"},
		"all three": {sanitize.Remedies{FieldGroups: groups, Limit: 100, TimeWindow: true},
			"fewer field groups in fields (core, io and metrics), a lower limit or a shorter time window"},
		"the metrics query": {sanitize.Remedies{MetricsQuery: true},
			"a shorter time range, more filters or a lower config.row_limit in the query JSON"},
		"paging is not a way": {sanitize.Remedies{Page: true, Cursor: true}, ""},
		"another operation's groups": {sanitize.Remedies{FieldGroups: "score and trace"},
			"fewer field groups in fields (score and trace)"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.remedies.NarrowWays(); got != tc.want {
				t.Errorf("NarrowWays = %q, want %q", got, tc.want)
			}
		})
	}
}

// The truncation hint is built from the same words: it names the ways of
// narrowing that NarrowWays names, after "narrow the query: ", and none for an
// operation that has only paging.
func TestTheTruncationHintNamesTheWaysOfNarrowingThatNarrowWaysNames(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		remedies sanitize.Remedies
		want     string
	}{
		"limit alone":       {sanitize.Remedies{Limit: 100}, "narrow the query: a lower limit"},
		"time window alone": {sanitize.Remedies{TimeWindow: true}, "narrow the query: a shorter time window"},
		"limit and window":  {sanitize.Remedies{Limit: 100, TimeWindow: true}, "narrow the query: a lower limit or a shorter time window"},
		"the metrics query": {sanitize.Remedies{MetricsQuery: true},
			"narrow the query: a shorter time range, more filters or a lower config.row_limit in the query JSON"},
		"fields and window": {sanitize.Remedies{FieldGroups: "core and io", TimeWindow: true},
			"narrow the query: fewer field groups in fields (core and io) or a shorter time window"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := sanitize.Wrap("op", payloadOf(t, pageOf(10, 30000)), 16384, tc.remedies)

			if !env.Truncated || !strings.Contains(env.Hint, tc.want) {
				t.Errorf("truncated %v, hint %q; want it to contain %q", env.Truncated, env.Hint, tc.want)
			}
		})
	}
	t.Run("paging alone is not a way of narrowing", func(t *testing.T) {
		t.Parallel()
		env := sanitize.Wrap("op", payloadOf(t, pageOf(10, 30000)), 16384, sanitize.Remedies{Page: true})

		if strings.Contains(env.Hint, "narrow the query") {
			t.Errorf("hint %q has narrowing words for an operation that has only paging", env.Hint)
		}
	})
}
