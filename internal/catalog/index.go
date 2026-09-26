package catalog

import (
	"slices"
	"strings"
)

// Search returns the operations of the operation index that match query,
// grouped by tag and sorted by ID within a tag. query is split into terms at
// whitespace; an operation is kept when every term appears, ignoring case, in
// its ID, its tag or its description line. A query without terms keeps every
// operation. The caller bounds and checks query: it is model input.
func (c Catalog) Search(query string) []Operation {
	terms := strings.Fields(strings.ToLower(query))
	ops := make([]Operation, 0, len(c.byID))
	for _, op := range c.byID {
		if op.matches(terms) {
			ops = append(ops, op)
		}
	}
	slices.SortFunc(ops, func(a, b Operation) int {
		if a.Tag != b.Tag {
			return strings.Compare(a.Tag, b.Tag)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return ops
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
