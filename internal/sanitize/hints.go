package sanitize

import (
	"fmt"
	"strings"
)

// Remedies are the ways a caller can ask an operation for a smaller result:
// the truncation hint names only these, so that it never sends the agent to a
// parameter the operation lacks.
type Remedies struct {
	// Fields names the field groups of the operation's fields parameter ("core,
	// io and metrics"); "" when the operation does not take it. The hints name
	// the groups, from the same compiled source as the guidance (#194).
	Fields string
	// Limit is the largest page size the operation accepts; 0 when it takes
	// none.
	Limit int
	// TimeWindow is set when the operation takes a time range.
	TimeWindow bool
	// Page is set when the operation takes the page parameter; Cursor when it
	// takes the cursor parameter, continued from meta.cursor. The hint names
	// only the way of paging the operation has (#193).
	Page, Cursor bool
}

// PagingNote is the part of a hint that says how to read the rest of a list,
// "page through the rest" and the parameters the operation pages by, or "" when
// it has none.
func (r Remedies) PagingNote() string {
	if ways := r.PagingWays(); ways != "" {
		return pagingNote + " (" + ways + ")"
	}
	return ""
}

// PagingWays names the parameters the operation pages by, as a hint writes
// them in brackets: "page", "cursor from meta.cursor", both, or "" when the
// operation has neither. No legacy list has a cursor.
func (r Remedies) PagingWays() string {
	switch {
	case r.Page && r.Cursor:
		return "page, or cursor from meta.cursor"
	case r.Page:
		return "page"
	case r.Cursor:
		return "cursor from meta.cursor"
	}
	return ""
}

// FieldsWay is the words that ask for fewer field groups and name them, or ""
// when the operation lacks fields. Every hint that recommends fewer fields uses
// it, so that none sends the agent to a parameter the operation lacks and none
// names groups the guidance does not (#194).
func (r Remedies) FieldsWay() string {
	if r.Fields == "" {
		return ""
	}
	return "fewer field groups in fields (" + r.Fields + ")"
}

// JoinWays writes the ways to narrow a query as one alternative: "a", "a or b",
// "a, b or c". A way that is empty is left out.
func JoinWays(ways ...string) string {
	var kept []string
	for _, w := range ways {
		if w != "" {
			kept = append(kept, w)
		}
	}
	switch len(kept) {
	case 0:
		return ""
	case 1:
		return kept[0]
	}
	return strings.Join(kept[:len(kept)-1], ", ") + " or " + kept[len(kept)-1]
}

// clause is the remedies as one sentence part, "" when there are none.
func (r Remedies) clause() string {
	var limit, window string
	if r.Limit > 0 {
		limit = "a lower limit"
	}
	if r.TimeWindow {
		window = "a shorter time window"
	}
	if ways := JoinWays(r.FieldsWay(), limit, window); ways != "" {
		return "narrow the query: " + ways
	}
	return ""
}

// The static parts of the truncation hints. None holds payload text.
const (
	clipNote = "values ending in \"" + clippedMarker + "\" were shortened to fit the result size cap, " +
		"each keeping its start; every other value is whole"
	tooBigNote = "a single value is larger than the result size cap and cannot be read whole through this server: " +
		"tell the user instead of retrying"
	textNote = "the payload did not fit the result size cap even with every large value clipped: " +
		"data is its start as text, ending in the truncation marker"
	pagingNote = "page through the rest"
)

// hints builds the hints of one truncated result from static text, the
// operation's remedies and numbers, never from the payload.
type hints struct {
	// remedies are the operation's parameters that ask for less; the zero
	// value when the remedy is fixed text.
	remedies Remedies
	// remedy is the sentence part that says how to ask for less; "" when
	// there is no way.
	remedy string
	// resultHint is the hint the result carries when it is not truncated, kept
	// after the truncation hint.
	resultHint string
}

// hintsFor returns the hints of an operation with remedies r.
func hintsFor(r Remedies) hints { return hints{remedies: r, remedy: r.clause()} }

// join makes the hint of parts, before the result hint.
func (n hints) join(parts ...string) string {
	if n.resultHint != "" {
		parts = append(parts, n.resultHint)
	}
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "; ")
}

// clipped is the hint of a result whose large values were clipped.
func (n hints) clipped(tooBig bool) string {
	return n.join(clipNote, n.remedy, tooBigIf(tooBig))
}

// rows is the hint of a page that kept its first kept rows of total, with
// its large values clipped when clipped.
func (n hints) rows(kept, total int, clipped, tooBig bool) string {
	head := fmt.Sprintf("only the first %d of %d rows fit the result size cap", kept, total)
	note, again := "", ""
	if clipped {
		head += " even with every large value clipped"
		note = clipNote
	}
	if n.remedies.Limit > 0 {
		again = fmt.Sprintf("call again with limit %d", min(max(kept, 1), n.remedies.Limit))
	}
	return n.join(head, note, again, n.remedy, n.remedies.PagingNote(), tooBigIf(tooBig))
}

// text is the hint of a payload cut to text.
func (n hints) text(tooBig bool) string {
	return n.join(textNote, n.remedy, tooBigIf(tooBig))
}

// tooBigIf is tooBigNote when tooBig, else "".
func tooBigIf(tooBig bool) string {
	if tooBig {
		return tooBigNote
	}
	return ""
}
