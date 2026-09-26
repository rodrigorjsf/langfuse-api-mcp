package catalog

import (
	"cmp"
	"slices"
	"strings"
)

// Search returns the operations of the operation index that match query,
// grouped by tag. query is split into terms at whitespace; an operation is
// kept when every term appears, ignoring case, in its ID, its tag or its
// description line. A query without terms keeps every operation. The caller
// bounds and checks query: it is model input.
//
// The v4 family ranks before the legacy family (ADR-0012 §5): a group whose
// kept operations include a v4-family one comes first, a group of legacy-family
// operations only comes last, and the rest sit between; groups of the same
// rank are in tag order. Within a group, operations are ranked the same way,
// then sorted by ID.
func (c Catalog) Search(query string) []Operation {
	terms := strings.Fields(strings.ToLower(query))
	ops := make([]Operation, 0, len(c.byID))
	groupRank := map[string]int{}
	for _, op := range c.byID {
		if !op.matches(terms) {
			continue
		}
		ops = append(ops, op)
		if r, ok := groupRank[op.Tag]; !ok || op.rank() < r {
			groupRank[op.Tag] = op.rank()
		}
	}
	slices.SortFunc(ops, func(a, b Operation) int {
		if a.Tag != b.Tag {
			return cmp.Or(cmp.Compare(groupRank[a.Tag], groupRank[b.Tag]), strings.Compare(a.Tag, b.Tag))
		}
		return cmp.Or(cmp.Compare(a.rank(), b.rank()), strings.Compare(a.ID, b.ID))
	})
	return ops
}

// rank orders operations by family: v4 read first, legacy last, the rest
// (no family, experiments) between.
func (o Operation) rank() int {
	switch o.Family {
	case V4ReadFamily:
		return 0
	case LegacyFamily:
		return 2
	default:
		return 1
	}
}

// matches reports whether every lower-case term appears in the operation's
// ID, tag or description line, ignoring case.
func (o Operation) matches(terms []string) bool {
	fields := []string{strings.ToLower(o.ID), strings.ToLower(o.Tag), strings.ToLower(o.DescriptionLine)}
	for _, term := range terms {
		if !slices.ContainsFunc(fields, func(f string) bool { return strings.Contains(f, term) }) {
			return false
		}
	}
	return true
}
