package sanitize

import (
	"encoding/json"
	"maps"
	"slices"
	"sort"
	"unicode/utf8"
)

// clippedMarker ends a string value that clipping shortened. Static text: a
// payload that holds the same words is data, never a clip.
const clippedMarker = "… [clipped]"

// clipFloor is the fewest bytes of a value clipping keeps before the marker.
// It leaves an ID, a timestamp or a model name whole, and lets a long text
// show what it is about; below it, a result that still does not fit loses
// rows instead.
const clipFloor = 128

// cursorKey is the key of a continuation cursor. Its value is opaque Langfuse
// data that the agent sends back unchanged (#93): shortened, it would break
// paging, so clipping never touches it.
const cursorKey = "cursor"

// clipper shortens the string values of one decoded payload. It owns the
// decoded value: apply rewrites its strings in place.
type clipper struct {
	// raw is the payload as sent, for when it could not be decoded.
	raw json.RawMessage
	// root holds the decoded payload as its only element, so that a payload
	// that is itself a string can be clipped like any other value.
	root []any
	// slots are the string values clipping may shorten: those that clipping to
	// the floor makes shorter. Keys and cursors are never clipped.
	slots []slot
	// longest is the length in bytes of the longest slot.
	longest int
}

// slot is one string value of a decoded payload, with how to write it back.
type slot struct {
	text string
	set  func(string)
}

// newClipper decodes payload, a valid JSON value, and finds its clippable
// strings.
func newClipper(payload json.RawMessage) *clipper {
	c := &clipper{raw: payload}
	v, err := Decode(payload)
	if err != nil {
		return c // unreachable for valid JSON; nothing to clip
	}
	c.root = []any{v}
	c.collect(c.root)
	return c
}

// collect adds the clippable strings below v, and v's own children, to c.
func (c *clipper) collect(v any) {
	add := func(item any, set func(string)) {
		if s, ok := item.(string); ok {
			if len(s) > clipFloor+len(clippedMarker) {
				c.slots = append(c.slots, slot{text: s, set: set})
				c.longest = max(c.longest, len(s))
			}
			return
		}
		c.collect(item)
	}
	switch v := v.(type) {
	case []any:
		for i, item := range v {
			add(item, func(s string) { v[i] = s })
		}
	case map[string]any:
		for k, item := range v {
			if k != cursorKey {
				add(item, func(s string) { v[k] = s })
			}
		}
	}
}

// marshal returns the payload with its strings as they are now.
func (c *clipper) marshal() json.RawMessage {
	if c.root == nil {
		return c.raw
	}
	return marshal(c.root[0])
}

// apply clips every slot to at most ceil bytes of its start, followed by the
// marker, and returns how many it shortened. A value that clipping would not
// make shorter is left whole. Applying a larger ceiling restores what a
// smaller one cut.
func (c *clipper) apply(ceil int) int {
	n := 0
	for _, s := range c.slots {
		text := clip(s.text, ceil)
		s.set(text)
		if len(text) < len(s.text) {
			n++
		}
	}
	return n
}

// clip returns s cut to at most ceil bytes, at a character boundary, followed
// by the marker; or s when that would not be shorter.
func clip(s string, ceil int) string {
	if len(s) <= ceil+len(clippedMarker) {
		return s
	}
	return s[:runeBoundary(s, ceil)] + clippedMarker
}

// runeBoundary returns the largest n' <= n at which s can be cut without
// splitting a character.
func runeBoundary(s string, n int) int {
	for n > 0 && n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

// fit clips the payload as little as it takes for env, holding hint, to fit
// limit bytes: the largest ceiling for which it does, found by bisection, so
// that the largest values lose the most and a value that fits is not touched.
// It returns false when even the floor does not fit, or when nothing can be
// clipped.
func (c *clipper) fit(env Envelope, limit int, hint string) (Envelope, bool) {
	if len(c.slots) == 0 {
		return env, false
	}
	env.Hint = hint
	// With every slot empty the envelope is its skeleton; each slot then adds
	// its own encoded size, minus the two bytes of the empty string.
	for _, s := range c.slots {
		s.set("")
	}
	env.Data = c.marshal()
	skeleton := size(env) - 2*len(c.slots)
	sizeAt := func(ceil int) int {
		total := skeleton
		for _, s := range c.slots {
			total += len(marshal(clip(s.text, ceil)))
		}
		return total
	}
	if sizeAt(clipFloor) > limit {
		return env, false
	}
	// Nothing clipped is the whole payload, which does not fit: the largest
	// ceiling that does is below the longest slot.
	ceil := clipFloor + sort.Search(c.longest-clipFloor, func(i int) bool { return sizeAt(clipFloor+1+i) > limit })
	c.apply(ceil)
	env.Data = c.marshal()
	return env, size(env) <= limit
}

// tooBig reports whether some value is larger than any result can hold: an
// envelope holding that value alone, with hint, would exceed limit bytes.
// Such a value cannot be read whole through this server, whatever the query.
func (c *clipper) tooBig(operationID string, limit int, hint string) bool {
	room := limit - (size(Envelope{Label: UntrustedLabel, OperationID: operationID, Hint: hint, Data: json.RawMessage(`[""]`)}) - 2)
	for _, s := range c.slots {
		if len(s.text)*6+2 > room && len(marshal(s.text)) > room { // an encoded character takes at most 6 bytes
			return true
		}
	}
	return false
}

// rowList is the list of rows of a payload, and how to replace it.
type rowList struct {
	rows []any
	set  func([]any)
}

// rowList finds the rows of the payload: the "data" list of a page; else the
// payload itself when it is a list; else the largest list reachable through
// objects, such as the observations of a trace. It returns false when there
// is none.
func (c *clipper) rowList() (rowList, bool) {
	if c.root == nil {
		return rowList{}, false
	}
	return findRows(c.root[0], func(rows []any) { c.root[0] = rows })
}

// findRows is rowList below v, which set replaces.
func findRows(v any, set func([]any)) (rowList, bool) {
	switch v := v.(type) {
	case []any:
		return rowList{rows: v, set: set}, true
	case map[string]any:
		if rows, ok := v["data"].([]any); ok {
			return rowList{rows: rows, set: func(r []any) { v["data"] = r }}, true
		}
		var best rowList
		found, bestSize := false, -1
		for _, k := range slices.Sorted(maps.Keys(v)) {
			if list, ok := findRows(v[k], func(r []any) { v[k] = r }); ok {
				if n := len(marshal(list.rows)); n > bestSize {
					best, found, bestSize = list, true, n
				}
			}
		}
		return best, found
	}
	return rowList{}, false
}

// keepRows leaves the first k rows of the payload and finds the clippable
// strings of what stays.
func (c *clipper) keepRows(k int) {
	list, ok := c.rowList()
	if !ok {
		return
	}
	list.set(list.rows[:k])
	c.slots, c.longest = nil, 0
	c.collect(c.root)
}
