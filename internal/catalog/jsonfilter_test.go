package catalog_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam S3 (spec #172, ticket #195): Request decodes and checks a JSON filter and
// sends the text it checked. The server seam proves each refusal through
// execute_read (internal/server/jsonfilter_test.go); these tests cover the
// bounds at their edges and everything a fuzzer finds.

// traceListOp returns the operation whose filter is a JSON filter in most tests.
func traceListOp(t testing.TB) catalog.Operation {
	t.Helper()
	op, ok := mustLoad(t).Lookup("trace_list")
	if !ok {
		t.Fatal("trace_list missing from the catalog")
	}
	return op
}

// nested returns a filter nested depth levels deep, the list being level 1.
func nested(depth int) string {
	return strings.Repeat("[", depth) + strings.Repeat("]", depth)
}

// The size and depth bounds of a JSON filter are those of the metrics query: a
// filter at the bound is accepted and one past it is refused as a malformed
// filter, naming the bound.
func TestAJSONFilterIsRefusedPastTheSizeAndDepthBoundsOfTheMetricsQuery(t *testing.T) {
	t.Parallel()
	op := traceListOp(t)
	head, tail := `["`, `"]`
	tests := map[string]struct {
		filter  string
		wantErr string
	}{
		"16384 bytes":         {head + strings.Repeat("a", 16384-len(head)-len(tail)) + tail, ""},
		"16385 bytes":         {head + strings.Repeat("a", 16385-len(head)-len(tail)) + tail, "want at most 16384 bytes of JSON, got 16385 bytes"},
		"10 levels deep":      {nested(10), ""},
		"11 levels deep":      {nested(11), "want JSON nested at most 10 objects or lists deep"},
		"11 levels, no close": {strings.Repeat("[", 11), "want JSON nested at most 10 objects or lists deep"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := op.Request(map[string]any{"filter": tc.filter})

			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("Request error = %v, want the filter accepted", err)
				}
				return
			}
			if !errors.Is(err, catalog.ErrJSONFilter) || !errors.Is(err, catalog.ErrInvalidParameter) ||
				!strings.Contains(err.Error(), "parameter filter: "+tc.wantErr) {
				t.Errorf("Request error = %v, want a malformed filter naming %q", err, tc.wantErr)
			}
		})
	}
}

// A refusal of a filter is a malformed filter and never a malformed metrics
// query, whose hint tells a different story.
func TestARefusedJSONFilterIsNotAMalformedMetricsQuery(t *testing.T) {
	t.Parallel()

	_, err := traceListOp(t).Request(map[string]any{"filter": `{"column":"name"}`})

	if !errors.Is(err, catalog.ErrJSONFilter) || errors.Is(err, catalog.ErrMetricsQuery) {
		t.Errorf("Request error = %v, want ErrJSONFilter and not ErrMetricsQuery", err)
	}
}

// walkJSON calls visit on every string and every object key of a decoded value.
func walkJSON(v any, visit func(string)) {
	switch v := v.(type) {
	case string:
		visit(v)
	case []any:
		for _, x := range v {
			walkJSON(x, visit)
		}
	case map[string]any:
		for key, x := range v {
			visit(key)
			walkJSON(x, visit)
		}
	}
}

// escapesOf returns the character of every \uXXXX escape in JSON text (a
// surrogate half as the code unit it is), reading the text as a JSON reader
// does: a backslash takes the character after it, so the second backslash of a
// doubled one starts no escape.
func escapesOf(text string) []rune {
	var units []rune
	for i := 0; i < len(text); i++ {
		if text[i] != '\\' || i+1 >= len(text) {
			continue
		}
		if text[i+1] == 'u' && i+6 <= len(text) {
			var unit rune
			if n, err := fmt.Sscanf(text[i+2:i+6], "%04x", &unit); err == nil && n == 1 {
				units = append(units, unit)
			}
			i += 5
			continue
		}
		i++
	}
	return units
}

// Whatever a caller passes as a filter, the text Request sends decodes to one
// list, holds no string or key with a control, format or other invisible
// character, holds no escape of such a character or of half a surrogate pair,
// and is itself accepted and sent unchanged.
func FuzzAcceptedJSONFilterSendsNoHiddenCharacter(f *testing.F) {
	backslash := `\`
	for _, seed := range []string{
		``, `[]`, `[{}]`, `[{"column":"name","operator":"=","type":"string","value":"x"}]`,
		"[\r\n\t{\"a\": 1}\r\n]", `[{"a":1,"a":2}]`, `[{"b":1,"a":[3,2,1]}]`, `[9007199254740993, 0.10, 1E2, -0]`,
		`{"a":1}`, `"x"`, `null`, `[] x`, `[1,]`, `[[[[[[[[[[[[1]]]]]]]]]]]]`, "\xff\xfe", "[\"a\tb\"]",
		`["` + backslash + backslash + `u200b"]`, `["<b>&amp;</b>"]`,
		`["` + backslash + `u2028"]`, `["` + backslash + `ud800"]`, `["` + backslash + `ud83d` + backslash + `ude00"]`,
		`["` + backslash + `ude00` + backslash + `ud83d"]`, `["` + backslash + `u0000"]`, `["` + backslash + `u202e"]`,
		`["` + backslash + `uD83D` + backslash + `uDE00"]`, `[{"` + backslash + `u200b":1}]`, `["` + backslash + `n"]`,
	} {
		f.Add(seed)
	}
	for _, r := range []rune{0x00, 0x09, 0x0a, 0x1b, 0x7f, 0x85, 0xad, 0x200b, 0x202e, 0x2066, 0xfeff, 0xe0041, 0xe0100} {
		f.Add(`["a` + string(r) + `b"]`)
	}
	op := traceListOp(f)
	f.Fuzz(func(t *testing.T, filter string) {
		req, err := op.Request(map[string]any{"filter": filter})
		if err != nil {
			if !errors.Is(err, catalog.ErrInvalidParameter) {
				t.Fatalf("Request(%q) error %v does not wrap ErrInvalidParameter", filter, err)
			}
			for _, r := range filter {
				if hiddenOrFormat(r) && strings.ContainsRune(err.Error(), r) {
					t.Fatalf("the refusal %q repeats %U of the filter", err, r)
				}
			}
			return
		}
		sent := req.Query.Get("filter")
		dec := json.NewDecoder(strings.NewReader(sent))
		dec.UseNumber()
		var list []any
		if err := dec.Decode(&list); err != nil {
			t.Fatalf("Request(%q) sent %q, want the JSON text of a list: %v", filter, sent, err)
		}
		walkJSON(list, func(s string) {
			for _, r := range s {
				if hiddenOrFormat(r) {
					t.Fatalf("Request(%q) sent %q, which decodes to a string or key with %U", filter, sent, r)
				}
			}
		})
		for _, unit := range escapesOf(sent) {
			if unit >= 0xd800 && unit <= 0xdfff || hiddenOrFormat(unit) {
				t.Fatalf("Request(%q) sent %q, which holds the escape of %U", filter, sent, unit)
			}
		}
		again, err := op.Request(map[string]any{"filter": sent})
		if len(sent) <= 16384 && (err != nil || again.Query.Get("filter") != sent) {
			t.Fatalf("Request(%q) sent %q, which is not accepted unchanged when sent again: %v", filter, sent, err)
		}
	})
}

// The conditions and list items keep their order, and the keys of an object are
// written in sorted order: the same JSON value in a text of one shape.
func TestAJSONFilterIsSentAsCompactJSONInTheCallersOrder(t *testing.T) {
	t.Parallel()

	req, err := traceListOp(t).Request(map[string]any{
		"filter": "[ {\"value\" : [\"z\", \"a\"], \"column\" : \"b\"},\n {\"column\":\"a\"} ]",
	})

	if err != nil {
		t.Fatalf("Request = %v, want the filter accepted", err)
	}
	if got, want := req.Query.Get("filter"), `[{"column":"b","value":["z","a"]},{"column":"a"}]`; got != want {
		t.Errorf("filter sent = %s, want %s", got, want)
	}
}

// The filter of scim_listUsers is no JSON text: it is checked as any query
// value, so a plain expression is sent as it is.
func TestTheSCIMFilterIsAnOrdinaryQueryParameterValue(t *testing.T) {
	t.Parallel()
	op, ok := mustLoad(t).Lookup("scim_listUsers")
	if !ok {
		t.Fatal("scim_listUsers missing from the catalog")
	}

	req, err := op.Request(map[string]any{"filter": `userName eq "jdoe"`})

	if err != nil || !slices.Equal(req.Query["filter"], []string{`userName eq "jdoe"`}) {
		t.Errorf("Request = %+v, %v, want the SCIM expression sent as it is", req, err)
	}
}

// execute_write builds its request through Request as execute_read does, so a
// JSON filter on a write operation meets the same checks. No write operation
// has a filter query parameter today; this one is a POST that does.
func TestAJSONFilterOnAWriteOperationIsChecked(t *testing.T) {
	t.Parallel()
	write := catalog.Operation{
		ID: "trace_list", Method: "POST", Path: "/api/public/x",
		Params: []catalog.Param{{Name: "filter", In: "query", Schema: catalog.Schema{Type: "string"}}},
	}

	_, err := write.Request(map[string]any{"filter": `[{"value":"a` + "\\" + `u202eb"}]`})

	if !errors.Is(err, catalog.ErrJSONFilter) || !strings.Contains(err.Error(), "parameter filter: must not contain control or invisible characters") {
		t.Errorf("Request error = %v, want the refusal of a hidden character in a filter", err)
	}
}
