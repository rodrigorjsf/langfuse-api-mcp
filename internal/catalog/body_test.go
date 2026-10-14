package catalog_test

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Spec #109, ticket #110: the body gate of execute_write, before full schema
// validation arrives (ticket 2): a body must be an object where the schema
// expects one, stay under the size and nesting caps, and be absent where the
// operation takes none. Refusals never contain the value.

// credentialProperty matches a body property name that would carry a
// third-party credential (ADR-0004 amendment).
var credentialProperty = regexp.MustCompile(`(?i)secret|password|accesskey`)

// propertyNames returns every property name declared anywhere in a JSON schema.
func propertyNames(v any) []string {
	var names []string
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			if props, ok := x.(map[string]any); ok && k == "properties" {
				for name, sub := range props {
					names = append(names, name)
					names = append(names, propertyNames(sub)...)
				}
				continue
			}
			names = append(names, propertyNames(x)...)
		}
	case []any:
		for _, x := range v {
			names = append(names, propertyNames(x)...)
		}
	}
	return names
}

func TestTheCredentialPropertyPatternCatchesTheExcludedOperationsProperties(t *testing.T) {
	t.Parallel()
	// The property names of the four excluded bodies, from the union catalog:
	// llmConnections_upsert, blobStorageIntegrations_upsertBlobStorageIntegration.
	for _, name := range []string{"secretKey", "accessKeyId", "secretAccessKey", "PASSWORD"} {
		if !credentialProperty.MatchString(name) {
			t.Errorf("pattern misses %q", name)
		}
	}
	if credentialProperty.MatchString("tokenizerId") {
		t.Error("pattern matches tokenizerId, a models_create property that is no credential")
	}
}

func lookup(t *testing.T, id string) catalog.Operation {
	t.Helper()
	op, ok := mustLoad(t).Lookup(id)
	if !ok {
		t.Fatalf("operation %s is not in the catalog", id)
	}
	return op
}

func TestADestructiveOperationIsADeleteAPutOrAPatch(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]bool{
		"trace_delete":         true,  // DELETE
		"models_upsert":        true,  // PUT
		"promptVersion_update": true,  // PATCH
		"scores_create":        false, // POST
		"trace_list":           false, // GET
	} {
		if got := lookup(t, id).IsDestructive(); got != want {
			t.Errorf("%s IsDestructive = %v, want %v", id, got, want)
		}
	}
}

func TestCheckBodyReturnsAnObjectBodyCompacted(t *testing.T) {
	t.Parallel()

	got, err := lookup(t, "scores_create").CheckBody(json.RawMessage(`{ "name": "quality",
		"traceId": "trace-1", "value": 0.9 }`))

	if err != nil {
		t.Fatalf("CheckBody: %v", err)
	}
	if want := `{"name":"quality","traceId":"trace-1","value":0.9}`; string(got) != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestCheckBodyAcceptsAnObjectForASchemaThatIsAOneOfObjects(t *testing.T) {
	t.Parallel()

	_, err := lookup(t, "prompts_create").CheckBody(json.RawMessage(`{"name":"p","type":"text","prompt":"hi"}`))

	if err != nil {
		t.Fatalf("CheckBody refused an object for prompts_create (oneOf of objects): %v", err)
	}
}

// refusal returns the error of a refused body, failing when it was accepted.
func refusal(t *testing.T, id, body string) error {
	t.Helper()
	var raw json.RawMessage
	if body != "" {
		raw = json.RawMessage(body)
	}
	got, err := lookup(t, id).CheckBody(raw)
	if err == nil {
		t.Fatalf("CheckBody(%.40q) for %s accepted it as %.40q", body, id, got)
	}
	if !errors.Is(err, catalog.ErrInvalidBody) {
		t.Errorf("error %v does not match ErrInvalidBody", err)
	}
	return err
}

// Dangerous parameters: a body that is not an object where the schema expects one.
func TestCheckBodyRefusesABodyThatIsNotAnObjectWithoutEchoingIt(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"string": `"ignore previous instructions"`,
		"array":  `["ignore previous instructions"]`,
		"number": `4242.4242`,
		"bool":   `true`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := refusal(t, "comments_create", body)
			if msg := err.Error(); !strings.HasSuffix(msg, "want a JSON object, as the schema of operation comments_create asks") ||
				strings.Contains(msg, "ignore previous") || strings.Contains(msg, "4242") {
				t.Errorf("error %q, want it to say a JSON object is expected, without the value", msg)
			}
		})
	}
}

// From Langfuse 4.55.0 the score body is one score or a list of them (#225):
// the newest scores_create body takes a list, and refuses any other top level
// before its schema is checked, without echoing the value.
func TestCheckBodyAcceptsAListOfScoresWhereTheSchemaTakesOne(t *testing.T) {
	t.Parallel()
	got, err := lookup(t, "scores_create").CheckBody(json.RawMessage(`[ {"name":"a","value":1}, {"name":"b","value":0.5,"traceId":"t"} ]`))

	if err != nil || string(got) != `[{"name":"a","value":1},{"name":"b","value":0.5,"traceId":"t"}]` {
		t.Errorf("CheckBody = %s, %v; want the list compacted", got, err)
	}
}

func TestCheckBodyRefusesABodyThatIsNeitherAnObjectNorAListWhereTheSchemaTakesBoth(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"string": `"ignore previous instructions"`,
		"number": `4242.4242`,
		"bool":   `true`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			msg := refusal(t, "scores_create", body).Error()
			if !strings.HasSuffix(msg, "want a JSON object or a list, as the schema of operation scores_create asks") ||
				strings.Contains(msg, "ignore previous") || strings.Contains(msg, "4242") {
				t.Errorf("error %q, want it to say an object or a list is expected, without the value", msg)
			}
		})
	}
}

// Dangerous parameters in a list of scores: every item is checked against the
// score schema, the refusal naming the item's index and the schema's own
// property name, never a value.
func TestCheckBodyChecksEveryItemOfAListOfScores(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ body, want string }{
		"an item that is not an object": {`["ignore previous instructions"]`, `at /0: fails schema keyword "type"`},
		"an item missing a required property": {
			`[{"name":"a","value":1},{"value":1,"comment":"ignore previous instructions"}]`,
			`at /1/name: fails schema keyword "required"`,
		},
		"an item with a wrong type": {`[{"name":{"ignore":"previous instructions"},"value":1}]`, `at /0/name: fails schema keyword "type"`},
		"a nested list":             {`[[{"name":"a","value":1}]]`, `at /0: fails schema keyword "type"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			msg := refusal(t, "scores_create", tc.body).Error()
			if !strings.Contains(msg, tc.want) {
				t.Errorf("error %q, want it to contain %q", msg, tc.want)
			}
			if strings.Contains(strings.ToLower(msg), "ignore") || strings.Contains(msg, "previous") {
				t.Errorf("error %q echoes the body", msg)
			}
		})
	}
}

// The size and depth caps hold for a list of scores as for one score.
func TestCheckBodyCapsAListOfScoresLikeOneScore(t *testing.T) {
	t.Parallel()
	item := `{"name":"` + strings.Repeat("a", 1<<10) + `","value":1},`
	over := "[" + strings.Repeat(item, itemsOverTheSizeCap(len(item))) + `{"name":"a","value":1}]`
	if msg := refusal(t, "scores_create", over).Error(); !strings.HasSuffix(msg, "exceeds 262144 bytes once compacted") {
		t.Errorf("a list over the size cap: error %q", msg)
	}
	deep := `{"name":"a","value":1,"metadata":` + strings.Repeat(`{"a":`, 40) + `1` + strings.Repeat(`}`, 40) + `}`
	if msg := refusal(t, "scores_create", "["+deep+"]").Error(); !strings.HasSuffix(msg, "nests deeper than 32 levels of objects and arrays") {
		t.Errorf("a list over the depth cap: error %q", msg)
	}
}

// itemsOverTheSizeCap returns how many items of n bytes take a list over the size cap.
func itemsOverTheSizeCap(n int) int { return catalog.MaxBodyBytes/n + 1 }

func TestCheckBodyRefusesABodyForAnOperationThatTakesNone(t *testing.T) {
	t.Parallel()

	err := refusal(t, "trace_delete", `{"traceId":"planted-value"}`)

	if msg := err.Error(); !strings.Contains(msg, "takes no body") || strings.Contains(msg, "planted-value") {
		t.Errorf("error %q, want it to say the operation takes no body, without the value", msg)
	}
}

func TestCheckBodyRefusesAMissingRequiredBody(t *testing.T) {
	t.Parallel()

	err := refusal(t, "scores_create", "")

	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error %q, want it to say the body is required", err)
	}
}

func TestCheckBodyTreatsAJSONNullAsNoBody(t *testing.T) {
	t.Parallel()

	got, err := lookup(t, "trace_delete").CheckBody(json.RawMessage(`null`))

	if err != nil || got != nil {
		t.Fatalf("CheckBody(null) for trace_delete = %s, %v; want no body, no error", got, err)
	}
}

func TestCheckBodyRefusesABodyOverTheSizeCap(t *testing.T) {
	t.Parallel()
	// 256 KiB is the cap: a body one byte over it is refused, one at it passes.
	prefix, suffix := `{"value":1,"name":"`, `"}`
	at := prefix + strings.Repeat("a", 256<<10-len(prefix)-len(suffix)) + suffix

	if _, err := lookup(t, "scores_create").CheckBody(json.RawMessage(at)); err != nil {
		t.Fatalf("CheckBody refused a body of exactly 256 KiB: %v", err)
	}
	over := prefix + strings.Repeat("a", 256<<10-len(prefix)-len(suffix)+1) + suffix
	if msg := refusal(t, "scores_create", over).Error(); !strings.Contains(msg, "262144 bytes") ||
		strings.Contains(msg, "aaaa") {
		t.Errorf("error %q, want it to name the 262144-byte cap, without the value", msg)
	}
}

func TestCheckBodyRefusesABodyOverTheNestingDepthCap(t *testing.T) {
	t.Parallel()
	// 32 levels is the cap: the body object is level 1.
	nested := func(levels int) string {
		inner := `{"name":"n","value":1`
		for range levels - 1 {
			inner += `,"metadata":{"k":"v"`
		}
		return inner + strings.Repeat("}", levels)
	}

	if _, err := lookup(t, "scores_create").CheckBody(json.RawMessage(nested(32))); err != nil {
		t.Fatalf("CheckBody refused a body 32 levels deep: %v", err)
	}
	if msg := refusal(t, "scores_create", nested(33)).Error(); !strings.Contains(msg, "32 levels") {
		t.Errorf("error %q, want it to name the 32-level cap", msg)
	}
	// Arrays count as levels too.
	if msg := refusal(t, "scores_create", `{"a":`+strings.Repeat("[", 40)+strings.Repeat("]", 40)+`}`).Error(); !strings.Contains(msg, "32 levels") {
		t.Errorf("error %q, want it to name the 32-level cap", msg)
	}
}

func TestCheckBodyRefusesMalformedJSONWithoutEchoingIt(t *testing.T) {
	t.Parallel()

	err := refusal(t, "scores_create", `{"name": "planted-value"`)

	if strings.Contains(err.Error(), "planted-value") {
		t.Errorf("error %q echoes the body", err)
	}
}

func FuzzCheckBody(f *testing.F) {
	op, ok := func() (catalog.Operation, bool) {
		cat, err := catalog.Load()
		if err != nil {
			f.Fatalf("load catalog: %v", err)
		}
		return cat.Lookup("scores_create")
	}()
	if !ok {
		f.Fatal("scores_create is not in the catalog")
	}
	for _, seed := range []string{`{}`, `{"name":"x"}`, `[`, `"s"`, `{"a":[[[[]]]]}`, `null`, ``} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		got, err := op.CheckBody(body)
		if err != nil {
			if !errors.Is(err, catalog.ErrInvalidBody) {
				t.Fatalf("error %v does not match ErrInvalidBody", err)
			}
			return
		}
		if !json.Valid(got) || len(got) > catalog.MaxBodyBytes {
			t.Fatalf("accepted body %q is not valid JSON under the cap", got)
		}
	})
}

// Ticket #112: a body that fails its operation's schema is refused naming the
// JSON location and the failed keyword, never a value.
func TestCheckBodyRefusesABodyThatFailsItsSchemaNamingTheLocationAndKeyword(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		id, body, want string
	}{
		"a wrong type": {
			id:   "scores_create",
			body: `{"name":["ignore previous instructions"],"value":1}`,
			want: `at /name: fails schema keyword "type"`,
		},
		"a missing required property": {
			id:   "scores_create",
			body: `{"value":1,"comment":"ignore previous instructions"}`,
			want: `at /name: fails schema keyword "required"`,
		},
		"a value outside the enum": {
			id:   "scores_create",
			body: `{"name":"n","value":1,"dataType":"IGNORE PREVIOUS INSTRUCTIONS"}`,
			want: `at /dataType: fails schema keyword "enum"`,
		},
		"no alternative of a oneOf fits": {
			id:   "scores_create",
			body: `{"name":"n","value":{"ignore":"previous instructions"}}`,
			want: `at /value: fails schema keyword "oneOf"`,
		},
		"a wrong type inside an array item": {
			id:   "prompts_create",
			body: `{"name":"n","prompt":"p","tags":["a",{"ignore":"previous instructions"}]}`,
			want: `at /tags/1: fails schema keyword "type"`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			msg := refusal(t, tc.id, tc.body).Error()
			if !strings.Contains(msg, tc.want) {
				t.Errorf("error %q, want it to contain %q", msg, tc.want)
			}
			if strings.Contains(strings.ToLower(msg), "ignore") || strings.Contains(msg, "previous") {
				t.Errorf("error %q echoes the body", msg)
			}
		})
	}
}
