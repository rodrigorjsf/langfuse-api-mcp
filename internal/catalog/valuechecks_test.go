package catalog_test

import (
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam S3 (spec #172, ticket #179): Request checks the text of the values it
// builds a request from. The server seam proves each rule through execute_read
// (internal/server/valuechecks_test.go); these tests cover what the embedded
// catalog cannot show: a repeated date-time, and every value a fuzzer finds.

// timestampList is an operation whose one parameter is a list of timestamps,
// which the embedded catalog has none of.
func timestampList() catalog.Operation {
	return catalog.Operation{
		ID: "x_list", Method: "GET", Path: "/api/public/x",
		Params: []catalog.Param{{Name: "since", In: "query", Schema: catalog.Schema{
			Type: "array", Items: &catalog.Schema{Type: "string", Format: "date-time"},
		}}},
	}
}

// A list of timestamps is checked item by item: the refusal names the item and
// the format, never the value.
func TestAListItemThatIsNotRFC3339IsRefusedNamingTheItemAndTheFormat(t *testing.T) {
	t.Parallel()

	_, err := timestampList().Request(map[string]any{"since": []any{"2026-10-10T12:00:00Z", "2031-04-05"}})

	if !errors.Is(err, catalog.ErrInvalidParameter) ||
		!strings.Contains(err.Error(), "parameter since: item 1: want a date-time in RFC 3339 format") || strings.Contains(err.Error(), "2031") {
		t.Fatalf("Request error = %v, want ErrInvalidParameter naming item 1 of since and the format, without the value", err)
	}
}

// A list of RFC 3339 timestamps is sent item by item, unchanged.
func TestAListOfRFC3339TimestampsIsSentUnchanged(t *testing.T) {
	t.Parallel()

	req, err := timestampList().Request(map[string]any{"since": []any{"2026-10-10T12:00:00Z", "2026-10-11T12:00:00+02:00"}})

	if err != nil {
		t.Fatalf("Request = %v, want two RFC 3339 timestamps accepted", err)
	}
	if got := req.Query["since"]; len(got) != 2 || got[0] != "2026-10-10T12:00:00Z" || got[1] != "2026-10-11T12:00:00+02:00" {
		t.Fatalf("query since = %q, want both timestamps unchanged", got)
	}
}

// The page number of a list operation starts at 1, with the refusal naming its
// minimum and never the value.
func TestAPageBelowOneIsRefusedNamingItsMinimum(t *testing.T) {
	t.Parallel()
	op, ok := mustLoad(t).Lookup("trace_list")
	if !ok {
		t.Fatal("trace_list missing from the catalog")
	}

	for _, page := range []float64{0, -1, -1e9} {
		_, err := op.Request(map[string]any{"page": page})
		if !errors.Is(err, catalog.ErrInvalidParameter) || !strings.Contains(err.Error(), "parameter page: want a value of at least 1") {
			t.Errorf("Request(page %v) error = %v, want ErrInvalidParameter naming the minimum 1", page, err)
		}
	}
}

// Page 1 and every later page are accepted.
func TestPageOneAndLaterPagesAreAccepted(t *testing.T) {
	t.Parallel()
	op, ok := mustLoad(t).Lookup("trace_list")
	if !ok {
		t.Fatal("trace_list missing from the catalog")
	}

	for _, page := range []float64{1, 2, 1e6} {
		if _, err := op.Request(map[string]any{"page": page}); err != nil {
			t.Errorf("Request(page %v) = %v, want it accepted", page, err)
		}
	}
}

// hiddenOrFormat reports whether r is a control character, a format character
// (Unicode category Cf), the tag block, a variation selector or another
// default-ignorable code point.
func hiddenOrFormat(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r >= 0xe0000 && r <= 0xe007f ||
		unicode.In(r, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}

// Whatever a caller passes as a plain query value, a filter or a path
// parameter, Request accepts it only free of control, format and other
// invisible characters (tab, line feed and carriage return excepted in a
// filter, a JSON text), and a refusal never repeats such a character.
func FuzzRequestAcceptsNoControlOrFormatCharacter(f *testing.F) {
	for _, seed := range []string{"", "a", "ab", "a\tb", "a\nb", "a\rb", "\xff\xfe"} {
		f.Add(seed)
	}
	// One of each kind of character that hides or reorders text, by code point.
	for _, r := range []rune{0x00, 0x01, 0x0b, 0x1b, 0x7f, 0x85, 0xad, 0x34f, 0x3164, 0x200b, 0x200d, 0x200e, 0x202e,
		0x2060, 0x2066, 0xfe0f, 0xfeff, 0xe0041, 0xe0100} {
		f.Add("a" + string(r) + "b")
	}
	cat := mustLoad(f)
	traceList, ok := cat.Lookup("trace_list")
	if !ok {
		f.Fatal("trace_list missing from the catalog")
	}
	sessionGet, ok := cat.Lookup("sessions_get")
	if !ok {
		f.Fatal("sessions_get missing from the catalog")
	}
	f.Fuzz(func(t *testing.T, value string) {
		for _, tc := range []struct {
			op              catalog.Operation
			param           string
			keepsWhitespace bool
		}{{traceList, "filter", true}, {traceList, "userId", false}, {sessionGet, "sessionId", false}} {
			_, err := tc.op.Request(map[string]any{tc.param: value})
			if err != nil {
				if !errors.Is(err, catalog.ErrInvalidParameter) {
					t.Fatalf("%s: Request(%q) error %v does not wrap ErrInvalidParameter", tc.op.ID, value, err)
				}
				for _, r := range value {
					if hiddenOrFormat(r) && strings.ContainsRune(err.Error(), r) {
						t.Fatalf("%s: the refusal %q repeats %U of the value", tc.op.ID, err, r)
					}
				}
				continue
			}
			for _, r := range value {
				if tc.keepsWhitespace && (r == '\t' || r == '\n' || r == '\r') {
					continue
				}
				if hiddenOrFormat(r) {
					t.Fatalf("%s %s: Request(%q) accepted %U", tc.op.ID, tc.param, value, r)
				}
			}
		}
	})
}
