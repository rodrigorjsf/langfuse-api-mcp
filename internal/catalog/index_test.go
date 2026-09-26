package catalog_test

import (
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam 2 (spec #68): the operation index is built from the catalog alone.

func TestEveryOperationCarriesItsTagAndTheFirstLineOfItsDescription(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	for id, want := range map[string][2]string{
		"health_health":    {"Health", "Check health of API and database"},
		"prompts_get":      {"Prompts", "Get a prompt"},
		"models_delete":    {"Models", "Delete a model. Cannot delete models managed by Langfuse. You can create your own definition with the same modelName to override the definition though."},
		"experiments_list": {"Experiments", "List experiments with cursor-based pagination. Results are ordered by"},
	} {
		op, ok := cat.Lookup(id)
		if !ok {
			t.Fatalf("operation %s is not in the catalog", id)
		}
		if op.Tag != want[0] || op.DescriptionLine != want[1] {
			t.Errorf("%s: tag %q, description line %q; want %q, %q", id, op.Tag, op.DescriptionLine, want[0], want[1])
		}
	}
}

// ADR-0002 amendment: descriptions are third-party text; the index never
// carries a character that hides or reorders text.
func TestNoTagOrDescriptionLineOfTheEmbeddedCatalogHoldsAForbiddenCharacter(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	for _, op := range cat.Operations() {
		if op.Tag == "" || op.DescriptionLine == "" {
			t.Errorf("%s: tag %q, description line %q; want both non-empty", op.ID, op.Tag, op.DescriptionLine)
		}
		for _, s := range []string{op.Tag, op.DescriptionLine} {
			if i := slices.IndexFunc([]rune(s), forbidden); i >= 0 {
				t.Errorf("%s: %q holds forbidden character U+%04X", op.ID, s, []rune(s)[i])
			}
		}
	}
}

// forbidden reports an invisible, bidirectional formatting or control
// character, including the tag block U+E0000–E007F.
func forbidden(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || (r >= 0xE0000 && r <= 0xE007F) || r == '�'
}

// ids returns the IDs of ops, in order.
func ids(ops []catalog.Operation) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		out = append(out, op.ID)
	}
	return out
}

// ADR-0002 amendment: an operation is kept when every whitespace-separated
// term appears, ignoring case, in its ID, its tag or its description line;
// the result is grouped by tag, then sorted by ID.
func TestSearchKeepsTheOperationsMatchingEveryTermGroupedByTag(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	for query, want := range map[string][]string{
		// every term in the ID, tag or description line
		"annotation queue item": {
			"annotationQueues_createQueueItem", "annotationQueues_deleteQueueItem", "annotationQueues_getQueueItem",
			"annotationQueues_listQueueItems", "annotationQueues_updateQueueItem",
		},
		// "get" is in prompts_get's ID and in prompts_list's description line ("Get a list of prompt names…")
		"prompt get": {"prompts_get", "prompts_list"},
		// case-insensitive; "prompts" from the tag, "versions" from the description line
		"PROMPTS versions": {"prompts_delete", "prompts_list"},
		// tabs and repeated spaces separate terms too
		" health\t ":        {"health_health"},
		"no such operation": {},
	} {
		if got := ids(cat.Search(query)); !slices.Equal(got, want) {
			t.Errorf("Search(%q) = %v, want %v", query, got, want)
		}
	}
}

func TestSearchWithoutTermsReturnsEveryOperationGroupedByTag(t *testing.T) {
	t.Parallel()
	cat := mustLoad(t)

	got := cat.Search("")

	if len(got) != 104 {
		t.Fatalf("Search(\"\") returned %d operations, want all 104", len(got))
	}
	if first := got[:3]; !slices.Equal(ids(first), []string{
		"annotationQueues_createQueue", "annotationQueues_createQueueAssignment", "annotationQueues_createQueueItem",
	}) {
		t.Errorf("first operations = %v, want the AnnotationQueues tag first, sorted by ID", ids(first))
	}
	if !slices.IsSortedFunc(got, func(a, b catalog.Operation) int {
		if a.Tag != b.Tag {
			return strings.Compare(a.Tag, b.Tag)
		}
		return strings.Compare(a.ID, b.ID)
	}) {
		t.Errorf("operations are not grouped by tag and sorted by ID: %v", ids(got))
	}
}
