package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam S1 (spec #172, ticket #195): the JSON filter of an operation is decoded
// and checked by the server before any request, as the metrics query is. Every
// refusal here is an invalid_argument that names filter and the rule or the
// bound, never the value, and the fake Langfuse receives nothing.

// jsonFilterOperations are the operations whose filter is a JSON text: a list
// of conditions. The release specs describe each as "a JSON string containing
// an array of filter conditions" (docs/research/langfuse.md, section 1.15).
var jsonFilterOperations = []string{
	"trace_list", "legacy_observationsV1_getMany", "observations_getMany", "scores_get-many",
	"experiments_list", "experiments_listItems", "prompts_list",
}

// jsonFilterOps returns the catalog operations of jsonFilterOperations.
func jsonFilterOps(t *testing.T) []catalog.Operation {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	ops := make([]catalog.Operation, 0, len(jsonFilterOperations))
	for _, id := range jsonFilterOperations {
		op, ok := cat.Lookup(id)
		if !ok {
			t.Fatalf("%s missing from the catalog", id)
		}
		ops = append(ops, op)
	}
	return ops
}

// decodedFilter returns the list the fake Langfuse received as the filter,
// decoded: the tests compare the value, never the bytes the server chose to
// write it with.
func decodedFilter(t *testing.T, got received) []any {
	t.Helper()
	var list []any
	if err := json.Unmarshal([]byte(got.query.Get("filter")), &list); err != nil {
		t.Fatalf("Langfuse received filter %q, want the JSON text of a list: %v", got.query.Get("filter"), err)
	}
	return list
}

// jsonEscapeUpper is jsonEscape with upper-case hexadecimal digits.
func jsonEscapeUpper(r rune) string {
	if r > 0xffff {
		high, low := utf16.EncodeRune(r)
		return fmt.Sprintf(`\u%04X\u%04X`, high, low)
	}
	return fmt.Sprintf(`\u%04X`, r)
}

// nestedFilter wraps innermost, the text of the deepest container, in
// containers up to depth levels: the list itself is level 1, and the odd levels
// are lists and the even ones objects.
func nestedFilter(depth int, innermost string) string {
	text := innermost
	for level := depth - 1; level >= 1; level-- {
		if level%2 == 1 {
			text = "[" + text + "]"
		} else {
			text = `{"k":` + text + `}`
		}
	}
	return text
}

// filterRefusal is the message every refusal of a hidden character names.
const filterRefusal = "parameter filter: must not contain control or invisible characters"

// A character written as a JSON escape in a filter is what Langfuse decodes, so
// it is refused like the character itself, for every JSON filter, with no
// request.
func TestExecuteReadRefusesAJSONFilterHoldingTheEscapeOfAHiddenCharacterWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	for _, op := range jsonFilterOps(t) {
		t.Run(op.ID, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
			cs := connect(t, fake)
			for name, r := range hiddenCharacters {
				filter := `[{"column":"name","value":"` + plantedText + jsonEscape(r) + `"}]`

				got := toolErrorOf(t, callExecuteRead(t, cs, withParameter(op, "filter", filter))).Error

				if got.Code != "invalid_argument" || got.OperationID != op.ID || !strings.Contains(got.Message, filterRefusal) {
					t.Errorf("%s: error = %+v, want invalid_argument naming filter and the rule", name, got)
				}
				if all := got.Message + got.Hint; strings.Contains(all, plantedText) || strings.ContainsRune(all, r) {
					t.Errorf("%s: the refusal repeats the filter or its hidden character: %+v", name, got)
				}
				assertNoRequest(t, seen)
			}
		})
	}
}

// scimListUsers is the one read operation whose filter is not a JSON text: a
// SCIM filter expression.
func scimListUsers(t *testing.T) catalog.Operation {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	op, ok := cat.Lookup("scim_listUsers")
	if !ok {
		t.Fatal("scim_listUsers missing from the catalog")
	}
	return op
}

// The filter of scim_listUsers is a SCIM expression, not JSON: ordinary text is
// sent as it is, and is not refused for not being a list.
func TestExecuteReadSendsAScimFilterAsOrdinaryText(t *testing.T) {
	t.Parallel()
	const filter = `userName eq "jdoe@example.test"`
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"Resources":[]}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, withParameter(scimListUsers(t), "filter", filter))

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	if got := receivedOne(t, seen).query.Get("filter"); got != filter {
		t.Errorf("Langfuse received filter %q, want %q unchanged", got, filter)
	}
}

// A SCIM filter is an ordinary query parameter value: a control or invisible
// character is refused, tab, line feed and carriage return too (a JSON text
// may span lines, a SCIM expression does not).
func TestExecuteReadRefusesAControlOrInvisibleCharacterInAScimFilterWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	op := scimListUsers(t)
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"Resources":[]}`)
	cs := connect(t, fake)
	for name, r := range hiddenCharacters {
		got := toolErrorOf(t, callExecuteRead(t, cs, withParameter(op, "filter", "userName eq "+string(r)+plantedText))).Error

		if got.Code != "invalid_argument" || !strings.Contains(got.Message, filterRefusal) {
			t.Errorf("%s: error = %+v, want invalid_argument naming filter and the rule", name, got)
		}
		if all := got.Message + got.Hint; strings.Contains(all, plantedText) || strings.ContainsRune(all, r) {
			t.Errorf("%s: the refusal repeats the filter or its hidden character: %+v", name, got)
		}
		assertNoRequest(t, seen)
	}
}

// The escape is refused wherever the filter holds a string: in a condition's
// value, in an object key, in an item of a list value and at the deepest level
// the depth bound allows, with the hexadecimal digits in lower and upper case
// and a character above U+FFFF written as a surrogate pair.
func TestExecuteReadRefusesTheEscapeOfAHiddenCharacterInEveryPlaceAFilterHoldsAString(t *testing.T) {
	t.Parallel()
	places := map[string]func(text string) string{
		"in a condition's value":     func(text string) string { return `[{"column":"name","value":"` + text + `"}]` },
		"in an object key":           func(text string) string { return `[{"column":"name","` + text + `":1}]` },
		"in an item of a list value": func(text string) string { return `[{"column":"tags","value":["ok","` + text + `"]}]` },
		"in a string at level 10":    func(text string) string { return nestedFilter(10, `{"k":"`+text+`"}`) },
		"in a key at level 10":       func(text string) string { return nestedFilter(10, `{"`+text+`":1}`) },
	}
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)
	traceList := jsonFilterOps(t)[0]
	for name, r := range hiddenCharacters {
		for where, build := range places {
			for digits, escape := range map[string]string{"lower-case": jsonEscape(r), "upper-case": jsonEscapeUpper(r)} {
				filter := build(plantedText + escape)

				got := toolErrorOf(t, callExecuteRead(t, cs, withParameter(traceList, "filter", filter))).Error

				if got.Code != "invalid_argument" || !strings.Contains(got.Message, filterRefusal) {
					t.Errorf("%s %s, %s digits: error = %+v, want invalid_argument naming filter and the rule", name, where, digits, got)
				}
				if all := got.Message + got.Hint; strings.Contains(all, plantedText) || strings.ContainsRune(all, r) {
					t.Errorf("%s %s: the refusal repeats the filter or its hidden character: %+v", name, where, got)
				}
				assertNoRequest(t, seen)
			}
		}
	}
}

// sentFilter calls trace_list with filter and returns the text of the filter
// the fake Langfuse received.
func sentFilter(t *testing.T, filter string) string {
	t.Helper()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"filter": filter},
	})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	return receivedOne(t, seen).query.Get("filter")
}

// A key written twice in an object keeps its last value only, the one the
// server checked: the text sent holds the key once.
func TestExecuteReadSendsADuplicatedKeyOnceWithItsLastValue(t *testing.T) {
	t.Parallel()

	sent := sentFilter(t, `[{"column":"name","value":"first","value":"last"}]`)

	if want := `[{"column":"name","value":"last"}]`; sent != want {
		t.Errorf("Langfuse received filter %s, want %s", sent, want)
	}
}

// The value a duplicated key held before its last one is never sent, so a
// hidden character written there reaches nobody.
func TestExecuteReadDoesNotSendTheEarlierValueOfADuplicatedKey(t *testing.T) {
	t.Parallel()
	escape := jsonEscape(hiddenCharacters["right-to-left override"])

	sent := sentFilter(t, `[{"column":"name","value":"`+plantedText+escape+`","value":"last"}]`)

	if strings.Contains(sent, plantedText) || strings.Contains(sent, escape) {
		t.Errorf("Langfuse received filter %s, want only the last value of the duplicated key", sent)
	}
}

// The last value of a duplicated key is the one checked: a hidden character
// there is refused.
func TestExecuteReadRefusesAHiddenCharacterInTheLastValueOfADuplicatedKey(t *testing.T) {
	t.Parallel()
	escape := jsonEscape(hiddenCharacters["right-to-left override"])
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)

	got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"filter": `[{"value":"first","value":"` + escape + `"}]`},
	})).Error

	if got.Code != "invalid_argument" || !strings.Contains(got.Message, filterRefusal) {
		t.Errorf("error = %+v, want invalid_argument naming filter and the rule", got)
	}
	assertNoRequest(t, seen)
}

// The order of the conditions and of every list is kept: a condition list is
// ordered, and a value list may be.
func TestExecuteReadSendsTheConditionsAndListItemsInTheCallersOrder(t *testing.T) {
	t.Parallel()

	sent := sentFilter(t, `[{"column":"tags","value":["z","a","m"]},{"column":"name","value":"b"},{"column":"id","value":"a"}]`)

	if want := `[{"column":"tags","value":["z","a","m"]},{"column":"name","value":"b"},{"column":"id","value":"a"}]`; sent != want {
		t.Errorf("Langfuse received filter %s, want %s", sent, want)
	}
}

// Numbers are sent as they were written: an integer above 2^53 keeps its
// digits and a decimal fraction its trailing zero.
func TestExecuteReadSendsTheNumbersOfAFilterAsWritten(t *testing.T) {
	t.Parallel()

	sent := sentFilter(t, `[{"value":9007199254740993},{"value":0.10},{"value":1E2},{"value":-0}]`)

	if want := `[{"value":9007199254740993},{"value":0.10},{"value":1E2},{"value":-0}]`; sent != want {
		t.Errorf("Langfuse received filter %s, want %s", sent, want)
	}
}

// A doubled backslash before the u is a backslash and text, not an escape: it
// is no hidden character, so it is sent, as the one escaped backslash it is.
func TestExecuteReadSendsADoubledBackslashBeforeAnUAsOrdinaryText(t *testing.T) {
	t.Parallel()
	text := `a\\` + `u200bb`

	sent := sentFilter(t, `[{"column":"name","value":"`+text+`"}]`)

	if want := `[{"column":"name","value":"` + text + `"}]`; sent != want {
		t.Errorf("Langfuse received filter %s, want %s", sent, want)
	}
}

// A surrogate pair of escapes is sent as the character it makes, and an escape
// of half a pair as the replacement character: the text sent holds no escape
// that Langfuse could read as half of a pair.
func TestExecuteReadSendsNoEscapeOfAHalfSurrogatePair(t *testing.T) {
	t.Parallel()
	pair := `\` + `ud83d\` + `ude00`
	half := `\` + `ud83d`

	sent := sentFilter(t, `[{"value":"`+pair+`"},{"value":"`+half+`x"}]`)

	if want := `[{"value":"` + string(rune(0x1f600)) + `"},{"value":"` + string(rune(0xfffd)) + `x"}]`; sent != want {
		t.Errorf("Langfuse received filter %q, want %q", sent, want)
	}
}

// A value that holds an instruction or markup and no hidden character is data:
// it reaches Langfuse as it is, inside the query string, and changes neither
// the path nor the host of the request.
func TestExecuteReadSendsInstructionsAndMarkupInAFilterAsInertData(t *testing.T) {
	t.Parallel()
	const value = plantedText + ` <script>alert(1)</script> & https://evil.example/../../etc/passwd?x=1#y`
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"filter": `[{"column":"name","value":"` + value + `"}]`},
	})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	got := receivedOne(t, seen)
	if got.path != "/api/public/traces" || got.method != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/public/traces", got.method, got.path)
	}
	if want := `[{"column":"name","value":"` + value + `"}]`; got.query.Get("filter") != want {
		t.Errorf("Langfuse received filter %q, want %q", got.query.Get("filter"), want)
	}
}

// filterOfSize returns a valid filter of exactly size bytes.
func filterOfSize(t *testing.T, size int) string {
	t.Helper()
	const head, tail = `[{"value":"`, `"}]`
	if size < len(head)+len(tail) {
		t.Fatalf("size %d is smaller than the smallest filter of this shape", size)
	}
	return head + strings.Repeat("a", size-len(head)-len(tail)) + tail
}

// refusedFilter calls trace_list with filter, checks that Langfuse is not
// called, and returns the tool error.
func refusedFilter(t *testing.T, filter any) toolErrorFields {
	t.Helper()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)

	got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"filter": filter},
	})).Error

	assertNoRequest(t, seen)
	if got.Code != "invalid_argument" || got.OperationID != "trace_list" || got.Hint == "" {
		t.Fatalf("tool error = %+v, want invalid_argument for trace_list with a hint", got)
	}
	return got
}

// A filter of 16384 bytes, the bound of the metrics query, is sent.
func TestExecuteReadSendsAFilterOf16384Bytes(t *testing.T) {
	t.Parallel()

	sent := sentFilter(t, filterOfSize(t, 16384))

	if len(sent) != 16384 {
		t.Errorf("Langfuse received a filter of %d bytes, want 16384", len(sent))
	}
}

// One byte more is refused, naming the bound and the size as numbers only.
func TestExecuteReadRefusesAFilterOf16385BytesNamingTheBoundAndTheSize(t *testing.T) {
	t.Parallel()

	got := refusedFilter(t, filterOfSize(t, 16385))

	if want := "parameter filter: want at most 16384 bytes of JSON, got 16385 bytes"; !strings.Contains(got.Message, want) {
		t.Errorf("message = %q, want it to contain %q", got.Message, want)
	}
}

// A filter nested 10 levels deep, the bound of the metrics query, is sent.
func TestExecuteReadSendsAFilterNested10LevelsDeep(t *testing.T) {
	t.Parallel()
	filter := nestedFilter(10, `{}`)

	sent := sentFilter(t, filter)

	if sent != filter {
		t.Errorf("Langfuse received filter %s, want %s", sent, filter)
	}
}

// One level more is refused, naming the bound.
func TestExecuteReadRefusesAFilterNested11LevelsDeepNamingTheBound(t *testing.T) {
	t.Parallel()

	got := refusedFilter(t, nestedFilter(11, `[]`))

	if want := "parameter filter: want JSON nested at most 10 objects or lists deep"; !strings.Contains(got.Message, want) {
		t.Errorf("message = %q, want it to contain %q", got.Message, want)
	}
}

// The depth is checked before the text is decoded: 16 KiB of opening brackets
// is no JSON, and is refused for its depth, not for being malformed.
func TestExecuteReadChecksTheDepthOfAFilterBeforeDecodingIt(t *testing.T) {
	t.Parallel()

	got := refusedFilter(t, strings.Repeat("[", 16384))

	if want := "parameter filter: want JSON nested at most 10 objects or lists deep"; !strings.Contains(got.Message, want) {
		t.Errorf("message = %q, want it to contain %q", got.Message, want)
	}
}

// jsonFilterHint is the hint of every refusal of a JSON filter, as a literal:
// text compiled into the binary that says how a filter is sent, never the
// filter, and not the hint of a refused metrics query.
const jsonFilterHint = `send filter as one string whose value is the JSON text of a list of conditions, ` +
	`such as [{...},{...}]: not a list, and the value itself has no backslash before its quotes; ` +
	`at most 16384 bytes and 10 levels deep, with no control or invisible character in any string or key, ` +
	`written as itself or as a JSON escape`

// A filter that is not the JSON text of one list is refused naming filter and
// what is wanted, with the static hint, and never repeats the filter.
func TestExecuteReadRefusesAFilterThatIsNotTheJSONTextOfOneListWithoutEchoingIt(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		filter  any
		message string
	}{
		"a list as the argument":    {[]any{plantedText}, "parameter filter: want a string, got a list"},
		"an object as the argument": {map[string]any{"column": plantedText}, "parameter filter: want a string, got an object"},
		"a number as the argument":  {42, "parameter filter: want a string, got a number"},
		"a boolean as the argument": {true, "parameter filter: want a string, got a boolean"},
		"the JSON text of an object": {`{"column":"` + plantedText + `"}`,
			"parameter filter: want a string holding the JSON text of one list, got an object"},
		"the JSON text of a string": {`"` + plantedText + `"`,
			"parameter filter: want a string holding the JSON text of one list, got a string"},
		"the JSON text of a number": {`42`, "parameter filter: want a string holding the JSON text of one list, got a number"},
		"the JSON text of true":     {`true`, "parameter filter: want a string holding the JSON text of one list, got a boolean"},
		"the JSON text of null":     {`null`, "parameter filter: want a string holding the JSON text of one list, got null"},
		"an empty string":           {``, "parameter filter: want a string holding the JSON text of one list"},
		"only blanks":               {" \r\n\t ", "parameter filter: want a string holding the JSON text of one list"},
		"malformed JSON":            {`[{"column":"` + plantedText + `",}`, "parameter filter: want a string holding the JSON text of one list"},
		"a list followed by more data": {`[{"column":"name"}] {"x":"` + plantedText + `"}`,
			"parameter filter: want one JSON list, got data after it"},
		"a list followed by text": {`[] ` + plantedText, "parameter filter: want one JSON list, got data after it"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := refusedFilter(t, tc.filter)

			if !strings.Contains(got.Message, tc.message) {
				t.Errorf("message = %q, want it to contain %q", got.Message, tc.message)
			}
			if got.Hint != jsonFilterHint {
				t.Errorf("hint = %q, want %q", got.Hint, jsonFilterHint)
			}
			if strings.Contains(got.Message+got.Hint, plantedText) {
				t.Errorf("the refusal repeats the filter: %+v", got)
			}
		})
	}
}

// Every refusal of a filter, the size, the depth and the hidden character
// included, carries the same static hint.
func TestExecuteReadGivesEveryFilterRefusalTheSameStaticHint(t *testing.T) {
	t.Parallel()
	escape := jsonEscape(hiddenCharacters["zero width space"])
	for name, filter := range map[string]string{
		"too large":         filterOfSize(t, 16385),
		"too deep":          nestedFilter(11, `[]`),
		"an escaped hidden": `[{"value":"a` + escape + `"}]`,
		"a raw hidden":      "[{\"value\":\"a" + string(hiddenCharacters["zero width space"]) + "\"}]",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := refusedFilter(t, filter)

			if got.Hint != jsonFilterHint {
				t.Errorf("hint = %q, want %q", got.Hint, jsonFilterHint)
			}
		})
	}
}

// A filter on a deployment whose version does not serve it keeps the refusal of
// a parameter that is not served, which comes first: the filter is not read.
func TestExecuteReadRefusesAFilterTheVersionDoesNotServeBeforeReadingIt(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connectServing(t, fake, "3.80.0", catalog.LegacyFamily)

	// trace_list serves filter from 3.116.1, and this one is no list at all.
	got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"filter": `{"column":"` + plantedText + `"}`},
	})).Error

	if got.Code != "invalid_argument" || !strings.Contains(got.Message, `parameter "filter": operation trace_list does not serve it on this deployment`) ||
		!strings.Contains(got.Message, "from 3.116.1") {
		t.Errorf("tool error = %+v, want the refusal of a parameter the version does not serve", got)
	}
	if got.Hint == jsonFilterHint || strings.Contains(got.Message+got.Hint, plantedText) {
		t.Errorf("tool error = %+v, want the not-served hint and no filter text", got)
	}
	assertNoRequest(t, seen)
}

// The same filter is read once the version serves it.
func TestExecuteReadReadsAFilterOnceTheVersionServesIt(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connectServing(t, fake, "3.116.1", catalog.LegacyFamily)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"filter": `[{"column":"name","value":"x"}]`},
	})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	if got := receivedOne(t, seen).query.Get("filter"); got != `[{"column":"name","value":"x"}]` {
		t.Errorf("Langfuse received filter %q, want the condition", got)
	}
}

// The grammar of a condition is Langfuse's: an unknown key, column, operator or
// type is not refused here, and is sent.
func TestExecuteReadDoesNotCheckTheGrammarOfAFilter(t *testing.T) {
	t.Parallel()
	const filter = `[{"nope":true,"column":"no_such_column","operator":"zzz","type":"zzz","value":[1,"a",null,{"x":1.5}]},{}]`

	sent := sentFilter(t, filter)

	if want := `[{"column":"no_such_column","nope":true,"operator":"zzz","type":"zzz","value":[1,"a",null,{"x":1.5}]},{}]`; sent != want {
		t.Errorf("Langfuse received filter %s, want %s", sent, want)
	}
}

// A value that holds a URL, a scheme, a host or traversal text is sent as
// percent-encoded query text: it changes neither the path nor the host of the
// request and leaves no raw "://" or "../" in the query.
func TestExecuteReadSendsAURLInAFilterAsPercentEncodedQueryText(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{
			"filter": `[{"value":"https://evil.example/../../etc/passwd"},{"value":"//evil.example\\..\\x"}]`,
		},
	})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	got := receivedOne(t, seen)
	if got.path != "/api/public/traces" {
		t.Errorf("path = %s, want /api/public/traces", got.path)
	}
	if strings.Contains(got.rawQuery, "://") || strings.Contains(got.rawQuery, "../") || strings.Contains(got.rawQuery, "//") {
		t.Errorf("raw query = %s, want the URL percent-encoded", got.rawQuery)
	}
	if got.query.Get("filter") != `[{"value":"https://evil.example/../../etc/passwd"},{"value":"//evil.example\\..\\x"}]` {
		t.Errorf("Langfuse received filter %q, want the text unchanged", got.query.Get("filter"))
	}
}

// Neither the refusal nor the audit line of a refused filter holds the filter,
// a part of it, or its hidden character.
func TestTheAuditLineOfARefusedFilterHoldsNeitherTheFilterNorItsHiddenCharacter(t *testing.T) {
	t.Parallel()
	for name, r := range hiddenCharacters {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
			var logs syncBuffer
			cs := connectAudited(t, fake, &logs)

			res := callExecuteRead(t, cs, map[string]any{
				"operationId": "trace_list", "parameters": map[string]any{"filter": `[{"value":"` + plantedText + jsonEscape(r) + `"}]`},
			})

			if !res.IsError {
				t.Fatalf("execute_read succeeded, want a refusal: %s", resultText(t, res))
			}
			if text := logs.String(); strings.Contains(text, plantedText) || r != '\n' && strings.ContainsRune(text, r) { // the log has line breaks of its own
				t.Errorf("the audit log repeats the filter or its hidden character:\n%s", text)
			}
			assertNoRequest(t, seen)
		})
	}
}

// A 400 from Langfuse that repeats the filter is sanitized and redacted like
// any upstream error text: a hidden character and a key are gone.
func TestA400ThatRepeatsTheFilterIsSanitizedAndRedactedLikeAnyUpstreamText(t *testing.T) {
	t.Parallel()
	hidden := string(hiddenCharacters["right-to-left override"]) + string(hiddenCharacters["zero width space"])
	body, err := json.Marshal(map[string]any{
		"message": "Invalid filter [{\"value\":\"" + plantedText + hidden + "\"}] with " + testSecretKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	fake, _ := fakeLangfuse(t, http.StatusBadRequest, string(body))
	cs := connect(t, fake)

	got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"filter": `[{"value":"x"}]`},
	})).Error

	if got.Code != "langfuse_bad_request" || !strings.Contains(got.Message, "Invalid filter") {
		t.Fatalf("tool error = %+v, want langfuse_bad_request with Langfuse's message", got)
	}
	if strings.ContainsAny(got.Message, hidden) || strings.Contains(got.Message, testSecretKey) {
		t.Errorf("message = %q, want the hidden characters and the key gone", got.Message)
	}
}
