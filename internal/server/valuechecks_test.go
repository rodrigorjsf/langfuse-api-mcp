package server_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Seam S1 (spec #172, ticket #179): the values execute_read forwards to
// Langfuse are checked by the server before any request. Every refusal here is
// an invalid_argument that names the parameter and the rule, never the value,
// and the fake Langfuse receives nothing.

// plantedText is a value that carries instructions: no refusal, hint or audit
// line may repeat it.
const plantedText = "PLANTED-IGNORE-PREVIOUS-INSTRUCTIONS"

// readsTakingQuery returns the read operations of the embedded catalog that
// take a query parameter called name, accepted by pick.
func readsTakingQuery(t *testing.T, name string, pick func(catalog.Param) bool) []catalog.Operation {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	var ops []catalog.Operation
	for _, op := range readsOf(cat) {
		for _, p := range op.Params {
			if p.In == "query" && p.Name == name && pick(p) {
				ops = append(ops, op)
				break
			}
		}
	}
	return ops
}

// withParameter is the sample call of op with one more parameter.
func withParameter(op catalog.Operation, name string, value any) map[string]any {
	args := sampleRead(op, time.Now())
	args["parameters"].(map[string]any)[name] = value
	return args
}

// A page number starts at 1: 0 and a negative number are refused for every
// read operation that pages, with no request.
func TestExecuteReadRefusesAPageBelowOneWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	ops := readsTakingQuery(t, "page", func(catalog.Param) bool { return true })
	if len(ops) == 0 {
		t.Fatal("no read operation of the catalog takes a page")
	}
	for _, op := range ops {
		t.Run(op.ID, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
			cs := connect(t, fake)
			for _, page := range []any{0, -1, -7654321} {
				got := toolErrorOf(t, callExecuteRead(t, cs, withParameter(op, "page", page))).Error

				if got.Code != "invalid_argument" || got.OperationID != op.ID ||
					!strings.Contains(got.Message, "parameter page: ") || !strings.Contains(got.Message, "at least 1") {
					t.Errorf("page %v: error = %+v, want invalid_argument naming parameter page and its minimum 1", page, got)
				}
				if got.Hint == "" {
					t.Errorf("page %v: error = %+v, want a hint", page, got)
				}
				if strings.Contains(got.Message+got.Hint, "7654321") {
					t.Errorf("page %v: the refusal repeats the value: %+v", page, got)
				}
				assertNoRequest(t, seen)
			}
		})
	}
}

// Page 1 and a later page are sent as they are.
func TestExecuteReadSendsPageOneAndLaterPages(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)
	for _, page := range []struct {
		sent any
		want string
	}{{1, "1"}, {2, "2"}, {1000, "1000"}} {
		res := callExecuteRead(t, cs, map[string]any{
			"operationId": "trace_list", "parameters": map[string]any{"page": page.sent},
		})

		if res.IsError {
			t.Fatalf("page %v: execute_read returned a tool error: %s", page.sent, resultText(t, res))
		}
		if got := receivedOne(t, seen).query.Get("page"); got != page.want {
			t.Errorf("Langfuse received page %q, want %q", got, page.want)
		}
	}
}

// orderBy stays Langfuse's to check: the server forwards what it is given.
func TestExecuteReadForwardsOrderByUncheckedForLangfuseToJudge(t *testing.T) {
	t.Parallel()
	const orderBy = "not.a.valid.order,and more"
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"orderBy": orderBy},
	})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	if got := receivedOne(t, seen).query.Get("orderBy"); got != orderBy {
		t.Errorf("Langfuse received orderBy %q, want %q unchanged", got, orderBy)
	}
}

// A parameter whose schema says date-time must be an RFC 3339 timestamp: the
// refusal names the parameter and the format, never the value.
func TestExecuteReadRefusesATimestampThatIsNotRFC3339WithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	// Each value is distinctive (the date differs from the example the message
	// gives), so the test can tell that none is echoed.
	badValues := []string{
		"yesterday-IGNORE-PREVIOUS-INSTRUCTIONS",
		"2031-04-05",
		"2031-04-05 06:07:08Z",
		"2031-04-05T06:07:08",
		"2031-13-05T06:07:08Z",
		"2031-04-05T25:07:08Z",
		"1760097600",
		"2031-04-05T06:07:08Z; DROP",
		"2031-04-05T06:07:08,123Z", // a comma before the fraction: time.Parse takes it, RFC 3339 does not
		"2031-04-05T6:07:08Z",      // a one-digit hour
		"2031-4-5T06:07:08Z",
		" 2031-04-05T06:07:08Z",
		"",
	}
	for _, op := range readsOf(cat) {
		for _, p := range op.Params {
			if p.In != "query" || p.Schema.Format != "date-time" {
				continue
			}
			t.Run(op.ID+" "+p.Name, func(t *testing.T) {
				t.Parallel()
				fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
				cs := connect(t, fake)
				for _, bad := range badValues {
					got := toolErrorOf(t, callExecuteRead(t, cs, withParameter(op, p.Name, bad))).Error

					if got.Code != "invalid_argument" || got.OperationID != op.ID ||
						!strings.Contains(got.Message, "parameter "+p.Name+": ") || !strings.Contains(got.Message, "RFC 3339") {
						t.Errorf("%q: error = %+v, want invalid_argument naming parameter %s and RFC 3339", bad, got, p.Name)
					}
					if bad != "" && (strings.Contains(got.Message+got.Hint, bad) || strings.Contains(got.Message+got.Hint, "IGNORE")) {
						t.Errorf("%q: the refusal repeats the value: %+v", bad, got)
					}
					assertNoRequest(t, seen)
				}
			})
		}
	}
}

// An RFC 3339 timestamp, with a zone offset or fractional seconds, is sent as
// it is.
func TestExecuteReadSendsAnRFC3339TimestampUnchanged(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)
	for _, stamp := range []string{
		"2026-10-10T12:00:00Z",
		"2026-10-10T12:00:00.123Z",
		"2026-10-10T12:00:00+02:00",
		"2026-10-10T12:00:00.123456789-03:00",
	} {
		res := callExecuteRead(t, cs, map[string]any{
			"operationId": "trace_list", "parameters": map[string]any{"fromTimestamp": stamp, "toTimestamp": stamp},
		})

		if res.IsError {
			t.Fatalf("%s: execute_read returned a tool error: %s", stamp, resultText(t, res))
		}
		got := receivedOne(t, seen).query
		if got.Get("fromTimestamp") != stamp || got.Get("toTimestamp") != stamp {
			t.Errorf("Langfuse received %v, want both timestamps %q unchanged", got, stamp)
		}
	}
}

// hiddenCharacters holds one of each kind of character that hides or reorders
// text, by code point (built at run time, so this file holds none of them):
// control characters, zero-width and other format characters, bidi marks,
// overrides and isolates, tag characters, variation selectors and the other
// default-ignorable code points.
var hiddenCharacters = map[string]rune{
	"NUL": 0x00, "SOH": 0x01, "backspace": 0x08, "tab": 0x09, "line feed": 0x0a, "carriage return": 0x0d, "vertical tab": 0x0b, "form feed": 0x0c, "ESC": 0x1b, "DEL": 0x7f,
	"NEL, a C1 control": 0x85, "soft hyphen": 0xad, "combining grapheme joiner": 0x34f, "Hangul filler": 0x3164,
	"zero width space": 0x200b, "zero width joiner": 0x200d, "left-to-right mark": 0x200e,
	"right-to-left override": 0x202e, "word joiner": 0x2060, "left-to-right isolate": 0x2066,
	"variation selector 16": 0xfe0f, "byte order mark": 0xfeff, "tag character": 0xe0041,
	"variation selector supplement": 0xe0100,
}

// connectAudited starts the server against the fake Langfuse with its audit
// log in logs.
func connectAudited(t *testing.T, fake *httptest.Server, logs *syncBuffer) *mcp.ClientSession {
	t.Helper()
	return connectClient(t, langfuse.New(clientOptions(t, fake)), slog.New(slog.NewJSONHandler(logs, nil)))
}

// A control or invisible character in a query parameter value is refused, as
// it is in a path parameter: in a plain value and in a list item, with no
// request, and neither the refusal nor the audit line repeats the value.
func TestExecuteReadRefusesAControlOrInvisibleCharacterInAQueryParameterValueWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	for name, r := range hiddenCharacters {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := "a" + string(r) + plantedText
			fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
			var logs syncBuffer
			cs := connectAudited(t, fake, &logs)
			cases := []struct {
				param string
				value any
				want  string
			}{
				{"userId", value, "parameter userId: must not contain control or invisible characters"},
				{"name", value, "parameter name: must not contain control or invisible characters"},
				{"tags", []any{"ok", value}, "parameter tags: item 1: must not contain control or invisible characters"},
				{"tags", value, "parameter tags: item 0: must not contain control or invisible characters"},
			}
			if r != '\t' && r != '\n' && r != '\r' { // a JSON filter may span lines: there they are refused as malformed JSON, not by the rule
				cases = append(cases, struct {
					param string
					value any
					want  string
				}{"filter", value, "parameter filter: must not contain control or invisible characters"})
			}
			for _, tc := range cases {
				got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
					"operationId": "trace_list", "parameters": map[string]any{tc.param: tc.value},
				})).Error

				if got.Code != "invalid_argument" || !strings.Contains(got.Message, tc.want) {
					t.Errorf("%s: error = %+v, want invalid_argument with %q", tc.param, got, tc.want)
				}
				if all := got.Message + got.Hint; strings.Contains(all, plantedText) || strings.ContainsRune(all, r) {
					t.Errorf("%s: the refusal repeats the value or its hidden character: %+v", tc.param, got)
				}
				assertNoRequest(t, seen)
			}
			if text := logs.String(); strings.Contains(text, plantedText) || r != '\n' && strings.ContainsRune(text, r) { // the log has line breaks of its own
				t.Errorf("the audit log repeats the value or its hidden character:\n%s", text)
			}
		})
	}
}

// A filter is a JSON text that Langfuse parses, so a pretty-printed one holds
// tab, line feed and carriage return between its tokens: it is accepted and
// Langfuse receives the same JSON value.
func TestExecuteReadSendsAPrettyPrintedFilter(t *testing.T) {
	t.Parallel()
	const filter = "[\r\n\t{\"column\": \"name\",\n\t \"value\": \"x\"}\r\n]"
	fake, seen := fakeLangfuse(t, http.StatusOK, `{"data":[]}`)
	cs := connect(t, fake)

	res := callExecuteRead(t, cs, map[string]any{
		"operationId": "trace_list", "parameters": map[string]any{"filter": filter},
	})

	if res.IsError {
		t.Fatalf("execute_read returned a tool error: %s", resultText(t, res))
	}
	want := []any{map[string]any{"column": "name", "value": "x"}}
	if got := decodedFilter(t, receivedOne(t, seen)); !reflect.DeepEqual(got, want) {
		t.Errorf("Langfuse received filter %v, want %v", got, want)
	}
}

// The line breaks and tabs between the tokens of a metrics query are not
// strings the query holds: a pretty-printed query is not refused.
func TestExecuteReadSendsAPrettyPrintedMetricsQuery(t *testing.T) {
	t.Parallel()
	const query = "{\n  \"view\": \"observations\",\n\t\"metrics\": [{\"measure\": \"count\", \"aggregation\": \"count\"}],\r\n" +
		"  \"fromTimestamp\": \"2026-10-09T00:00:00Z\",\n  \"toTimestamp\": \"2026-10-10T00:00:00Z\"\n}"

	sent := sentQuery(t, "metrics_metrics", query)

	if sent["view"] != "observations" {
		t.Errorf("Langfuse received %v, want the pretty-printed query's view observations", sent)
	}
}

// A hidden character in a path parameter is refused, never removed: removing it
// would look up an ID the caller did not send (campaign M7). The fake Langfuse
// receives nothing, so neither the ID with the character nor the one without
// is looked up.
func TestExecuteReadRefusesAControlOrInvisibleCharacterInAPathParameterInsteadOfRemovingIt(t *testing.T) {
	t.Parallel()
	for name, r := range hiddenCharacters {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
			var logs syncBuffer
			cs := connectAudited(t, fake, &logs)
			for _, tc := range []struct{ operationID, param string }{
				{"trace_get", "traceId"}, {"sessions_get", "sessionId"}, {"datasets_get", "datasetName"},
			} {
				for _, value := range []string{"a" + string(r) + "b" + plantedText, string(r) + plantedText, plantedText + string(r)} {
					got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
						"operationId": tc.operationID, "parameters": map[string]any{tc.param: value},
					})).Error

					if want := "parameter " + tc.param + ": must not contain control or invisible characters"; got.Code != "invalid_argument" ||
						!strings.Contains(got.Message, want) {
						t.Errorf("%s: error = %+v, want invalid_argument with %q", tc.operationID, got, want)
					}
					if all := got.Message + got.Hint; strings.Contains(all, plantedText) || strings.ContainsRune(all, r) {
						t.Errorf("%s: the refusal repeats the value or its hidden character: %+v", tc.operationID, got)
					}
					assertNoRequest(t, seen)
				}
			}
			if text := logs.String(); strings.Contains(text, plantedText) || r != '\n' && strings.ContainsRune(text, r) { // the log has line breaks of its own
				t.Errorf("the audit log repeats the value or its hidden character:\n%s", text)
			}
		})
	}
}

// folderNameParameters are Folder-name parameters of the allow-list, with the
// path a Folder name reaches Langfuse at.
var folderNameParameters = []struct{ operationID, param, wantPath string }{
	{"prompts_get", "promptName", "/api/public/v2/prompts/support%2Fbilling"},
	{"datasets_get", "datasetName", "/api/public/v2/datasets/support%2Fbilling"},
}

// A Folder name on the allow-list still takes "/".
func TestAFolderNameStillTakesASlash(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
	cs := connect(t, fake)
	for _, tc := range folderNameParameters {
		res := callExecuteRead(t, cs, map[string]any{
			"operationId": tc.operationID, "parameters": map[string]any{tc.param: "support/billing"},
		})

		if res.IsError {
			t.Fatalf("%s: a Folder name: execute_read returned a tool error: %s", tc.operationID, resultText(t, res))
		}
		if got := receivedOne(t, seen).path; got != tc.wantPath {
			t.Errorf("%s: path = %s, want %s", tc.operationID, got, tc.wantPath)
		}
	}
}

// A Folder name on the allow-list still refuses a hidden character.
func TestAFolderNameStillRefusesAHiddenCharacter(t *testing.T) {
	t.Parallel()
	fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
	cs := connect(t, fake)
	for _, tc := range folderNameParameters {
		for name, r := range hiddenCharacters {
			got := toolErrorOf(t, callExecuteRead(t, cs, map[string]any{
				"operationId": tc.operationID, "parameters": map[string]any{tc.param: "support/" + string(r) + "billing"},
			})).Error

			if want := "parameter " + tc.param + ": must not contain control or invisible characters"; got.Code != "invalid_argument" ||
				!strings.Contains(got.Message, want) {
				t.Errorf("%s with %s: error = %+v, want invalid_argument with %q", tc.operationID, name, got, want)
			}
			assertNoRequest(t, seen)
		}
	}
}

// The server decodes a metrics query, so a hidden character is refused wherever
// the query holds one in a string or a key, written as itself or as a JSON
// escape: the escape is what Langfuse would decode. The refusal names the rule,
// never the query.
func TestAMetricsQueryHoldingAControlOrInvisibleCharacterIsRefusedWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	base := strings.TrimSuffix(metricsQuery, "}")
	places := map[string]func(text string) string{
		"in a filter value": func(text string) string {
			return base + `,"filters":[{"column":"name","operator":"=","type":"string","value":"` + text + `"}]}`
		},
		"in a column": func(text string) string {
			return base + `,"filters":[{"column":"` + text + `","operator":"=","type":"string","value":"x"}]}`
		},
		"in a key": func(text string) string { return base + `,"filters":[{"` + text + `":1}]}` },
	}
	for name, r := range hiddenCharacters {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			queries := map[string]string{}
			for where, build := range places {
				if r >= 0x20 { // a raw control character is not valid JSON: the query is refused as malformed
					queries[where+", written as itself"] = build(plantedText + string(r))
				}
				queries[where+", written as a JSON escape"] = build(plantedText + jsonEscape(r))
			}
			for where, query := range queries {
				got := refusedMetricsQuery(t, query)

				if want := "parameter query: must not contain control or invisible characters"; !strings.Contains(got.Message, want) {
					t.Errorf("%s: message = %q, want it to contain %q", where, got.Message, want)
				}
				if all := got.Message + got.Hint; strings.Contains(all, plantedText) || strings.ContainsRune(all, r) {
					t.Errorf("%s: the refusal repeats the query or its hidden character: %+v", where, got)
				}
			}
		})
	}
}

// jsonEscape writes r as the JSON escape Langfuse would decode: \u and four
// hexadecimal digits, a surrogate pair for a character above U+FFFF.
func jsonEscape(r rune) string {
	if r > 0xffff {
		high, low := utf16.EncodeRune(r)
		return fmt.Sprintf(`\u%04x\u%04x`, high, low)
	}
	return fmt.Sprintf(`\u%04x`, r)
}

// The catalog, which may not import sanitize (ADR-0009), keeps its own copy of
// the predicate for a character that hides or reorders text. This pins the
// copy to the sanitizer's over every code point, so the rule "the set
// sanitize.Text strips" cannot drift: a path parameter refuses exactly the
// characters the sanitizer removes from a payload, and tab, line feed and
// carriage return too, which the sanitizer keeps as visible text.
func TestAPathParameterRefusesExactlyTheCharactersTheSanitizerStrips(t *testing.T) {
	t.Parallel()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	op, ok := cat.Lookup("sessions_get")
	if !ok {
		t.Fatal("sessions_get missing from the catalog")
	}
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff || r == '/' || r == '\\' {
			continue // a surrogate is no character; "/" and "\" are refused by their own rules
		}
		value := "a" + string(r) + "b"

		_, err := op.Request(map[string]any{"sessionId": value})

		stripped := sanitize.Text(value) != value || r == '\t' || r == '\n' || r == '\r'
		if refused := err != nil; refused != stripped {
			t.Fatalf("U+%04X: path parameter refused = %v, sanitizer strips it = %v: the two predicates drifted", r, refused, stripped)
		}
	}
}
