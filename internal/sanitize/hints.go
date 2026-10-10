package sanitize

import (
	"fmt"
	"strings"
)

// Remedies are the ways a caller can ask an operation for a smaller result:
// the truncation hint names only these, so that it never sends the agent to a
// parameter the operation lacks.
type Remedies struct {
	// Fields is set when the operation takes the fields parameter.
	Fields bool
	// Limit is the largest page size the operation accepts; 0 when it takes
	// none.
	Limit int
	// TimeWindow is set when the operation takes a time range.
	TimeWindow bool
	// Paging is set when the operation pages through its rows.
	Paging bool
}

// clause is the remedies as one sentence part, "" when there are none.
func (r Remedies) clause() string {
	var ways []string
	if r.Fields {
		ways = append(ways, "fewer field groups in fields")
	}
	if r.Limit > 0 {
		ways = append(ways, "a lower limit")
	}
	if r.TimeWindow {
		ways = append(ways, "a shorter time window")
	}
	switch len(ways) {
	case 0:
		return ""
	case 1:
		return "narrow the query: " + ways[0]
	}
	return "narrow the query: " + strings.Join(ways[:len(ways)-1], ", ") + " or " + ways[len(ways)-1]
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
	note, again, page := "", "", ""
	if clipped {
		head += " even with every large value clipped"
		note = clipNote
	}
	if n.remedies.Limit > 0 {
		again = fmt.Sprintf("call again with limit %d", min(max(kept, 1), n.remedies.Limit))
	}
	if n.remedies.Paging {
		page = pagingNote
	}
	return n.join(head, note, again, n.remedy, page, tooBigIf(tooBig))
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
