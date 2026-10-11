package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// #202, the catalog's own reading of the request bodies of a union catalog: the
// literal specs below are the shapes the generator writes (x-request-bodies,
// x-spans), one operation x_create whose body changes at 3.5.0 and again at 4.0.0.

// historySpec is a union catalog whose POST x_create has three bodies: A for
// 3.0.0 to 3.5.0, B for 3.5.0 to 4.0.0 and A again from 4.0.0 (stored once).
const historySpec = `{"x-oldest-version":"3.0.0","x-newest-version":"4.10.0","paths":{"/api/public/x":{"post":{` +
	`"operationId":"x_create","x-introduced":"3.0.0","x-request-bodies":[` +
	`{"required":true,"content":{"application/json":{"schema":{"type":"object","required":["a"],"properties":{"a":{"type":"string"}}}}},` +
	`"x-spans":[{"x-introduced":"3.0.0","x-removed":"3.5.0"},{"x-introduced":"4.0.0"}]},` +
	`{"required":false,"content":{"application/json":{"schema":{"type":"object","required":["b"],"properties":{"b":{"type":"string"}}}}},` +
	`"x-spans":[{"x-introduced":"3.5.0","x-removed":"4.0.0"}]}]}}}}`

func mustLoadSpec(t *testing.T, spec string) Catalog {
	t.Helper()
	cat, err := load([]byte(spec))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cat
}

func TestTheBodyOfAnOperationFollowsTheReleasesEachStoredBodyHoldsFor(t *testing.T) {
	t.Parallel()
	cat := mustLoadSpec(t, historySpec)
	for _, tc := range []struct {
		version, wantRequired string
		bodyRequired          bool
	}{
		{"3.0.0", "a", true},
		{"3.4.9", "a", true},
		{"3.5.0", "b", false}, // the release that changes the body has the new one
		{"3.99.0", "b", false},
		{"4.0.0", "a", true}, // a body that comes back is the same stored body
		{"4.10.0", "a", true},
		{"9.0.0", "a", true},  // newer than any known release: the newest
		{"", "a", true},       // not detected: the newest
		{"1.0.0", "a", true},  // below the supported floor: taken at the floor
		{"3.5", "a", true},    // not a plain version: not detected
		{"latest", "a", true}, // likewise
	} {
		op, _ := cat.Resolve(Profile{Version: tc.version, Families: AllFamilies()}).Lookup("x_create")
		if op.Body == nil {
			t.Fatalf("at %q: no body", tc.version)
		}
		var s struct{ Required []string }
		if err := json.Unmarshal(op.Body.Schema, &s); err != nil || !slices.Equal(s.Required, []string{tc.wantRequired}) {
			t.Errorf("at %q: schema %s, want one requiring %q", tc.version, op.Body.Schema, tc.wantRequired)
		}
		if op.Body.Required != tc.bodyRequired {
			t.Errorf("at %q: body required = %v, want %v", tc.version, op.Body.Required, tc.bodyRequired)
		}
	}
}

func TestAnOperationThatGainsABodyLaterHasNoneBeforeIt(t *testing.T) {
	t.Parallel()
	cat := mustLoadSpec(t, `{"x-oldest-version":"3.0.0","x-newest-version":"4.0.0","paths":{"/api/public/x":{"post":{`+
		`"operationId":"x_create","x-introduced":"3.0.0","x-request-bodies":[{"required":true,"content":{"application/json":{"schema":{"type":"object"}}},`+
		`"x-spans":[{"x-introduced":"3.7.0"}]}]}}}}`)

	for version, want := range map[string]bool{"3.0.0": false, "3.6.9": false, "3.7.0": true, "4.0.0": true, "": true} {
		op, _ := cat.Resolve(Profile{Version: version, Families: AllFamilies()}).Lookup("x_create")
		if got := op.Body != nil; got != want {
			t.Errorf("at %q: has a body = %v, want %v", version, got, want)
		}
		if _, err := op.CheckBody(json.RawMessage(`{}`)); (err == nil) != want {
			t.Errorf("at %q: CheckBody({}) = %v, want accepted = %v", version, err, want)
		}
	}
}

// A body is checked against the schema of its own version, never another's.
func TestABodyIsCheckedAgainstTheSchemaOfTheDeploymentsVersionOnly(t *testing.T) {
	t.Parallel()
	cat := mustLoadSpec(t, historySpec)
	a, b := json.RawMessage(`{"a":"1"}`), json.RawMessage(`{"b":"1"}`)
	for _, tc := range []struct {
		version  string
		accepted json.RawMessage
		refused  json.RawMessage
	}{
		{"3.0.0", a, b}, {"3.5.0", b, a}, {"4.0.0", a, b}, {"", a, b},
	} {
		op, _ := cat.Resolve(Profile{Version: tc.version, Families: AllFamilies()}).Lookup("x_create")
		if _, err := op.CheckBody(tc.accepted); err != nil {
			t.Errorf("at %q: %s refused: %v", tc.version, tc.accepted, err)
		}
		if _, err := op.CheckBody(tc.refused); !errors.Is(err, ErrInvalidBody) {
			t.Errorf("at %q: %s: error %v, want ErrInvalidBody", tc.version, tc.refused, err)
		}
	}
}

// A plain OpenAPI spec (requestBody, no spans) holds for every release.
func TestAPlainRequestBodyHoldsForEveryRelease(t *testing.T) {
	t.Parallel()
	cat := mustLoadSpec(t, string(bodySpec(`{"type":"object","required":["a"]}`)))
	for _, version := range []string{"", "3.0.0", "4.10.0", "9.9.9"} {
		op, _ := cat.Resolve(Profile{Version: version}).Lookup("x_create")
		if op.Body == nil || !op.Body.Required {
			t.Errorf("at %q: body = %+v, want a required body", version, op.Body)
		}
	}
}

func TestACatalogWhoseBodiesAreNotReadableWholeFailsTheLoadNamingTheOperation(t *testing.T) {
	t.Parallel()
	body := func(spans string) string {
		return `{"required":true,"content":{"application/json":{"schema":{"type":"object"}}},"x-spans":` + spans + `}`
	}
	const from3, from4 = `[{"x-introduced":"3.0.0","x-removed":"4.0.0"}]`, `[{"x-introduced":"4.0.0"}]`
	for name, bodies := range map[string]string{
		"two bodies that hold for the same release":        body(from3) + `,` + body(`[{"x-introduced":"3.5.0"}]`),
		"a body with no span next to another":              body(from3) + `,{"required":true,"content":{"application/json":{"schema":{}}}}`,
		"the same span twice":                              body(from4) + `,` + body(from4),
		"a span that ends before it starts":                body(`[{"x-introduced":"4.0.0","x-removed":"3.0.0"}]`),
		"a span that ends where it starts":                 body(`[{"x-introduced":"4.0.0","x-removed":"4.0.0"}]`),
		"a span with a version that is not a plain one":    body(`[{"x-introduced":"v4"}]`),
		"an older body that is not a single JSON schema":   body(from4) + `,{"content":{"text/plain":{"schema":{"type":"string"}}},"x-spans":` + from3 + `}`,
		"an older body with keys only hidden marks differ": `{"content":{"application/json":{"schema":{"properties":{"a":{},"a\u200b":{}}}}},"x-spans":` + from3 + `},` + body(from4),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := `{"paths":{"/api/public/x":{"post":{"operationId":"x_create","x-request-bodies":[` + bodies + `]}}}}`
			if _, err := load([]byte(spec)); err == nil || !strings.Contains(err.Error(), "x_create") {
				t.Fatalf("load error = %v, want one naming x_create", err)
			}
		})
	}
}

func TestAnOperationMayNotHaveBothAPlainBodyAndAHistory(t *testing.T) {
	t.Parallel()
	body := `{"required":true,"content":{"application/json":{"schema":{"type":"object"}}}}`
	spec := `{"paths":{"/api/public/x":{"post":{"operationId":"x_create","requestBody":` + body + `,"x-request-bodies":[` + body + `]}}}}`
	if _, err := load([]byte(spec)); err == nil || !strings.Contains(err.Error(), "x_create") {
		t.Fatalf("load error = %v, want one naming x_create", err)
	}
}

// An operation the server offers keeps the history of every body: a catalog
// that marks one as keeping its newest body only is refused at load, while the
// same mark on an operation the server never offers (ADR-0004) is the point of it.
func TestEveryOperationTheServerOffersKeepsItsBodyHistory(t *testing.T) {
	t.Parallel()
	const body = `{"required":true,"content":{"application/json":{"schema":{"type":"object"}}}}`
	spec := func(id string) string {
		return `{"paths":{"/api/public/x":{"post":{"operationId":"` + id + `","x-newest-body-only":true,"x-request-bodies":[` + body + `]}}}}`
	}
	if _, err := load([]byte(spec("x_create"))); err == nil || !strings.Contains(err.Error(), "x_create") {
		t.Errorf("an offered operation marked newest-only: load error = %v, want one naming x_create", err)
	}
	for _, id := range excluded {
		if _, err := load([]byte(spec(id))); err != nil {
			t.Errorf("excluded operation %s marked newest-only: load error = %v, want none", id, err)
		}
	}
	for path, item := range embeddedOperations(t) {
		for method, op := range item {
			if op.NewestBodyOnly && !IsExcluded(op.OperationID) {
				t.Errorf("%s %s (%s) keeps its newest body only but the server offers it", strings.ToUpper(method), path, op.OperationID)
			}
		}
	}
}

// embeddedOperations is the embedded union catalog's operations, as written.
func embeddedOperations(t *testing.T) map[string]map[string]struct {
	OperationID    string `json:"operationId"`
	NewestBodyOnly bool   `json:"x-newest-body-only"`
} {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID    string `json:"operationId"`
			NewestBodyOnly bool   `json:"x-newest-body-only"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(unionCatalog, &doc); err != nil {
		t.Fatalf("embedded union catalog: %v", err)
	}
	return doc.Paths
}

// A schema that does not compile is found where a deployment's version chooses
// it: the load of a catalog compiles the newest bodies (an unknown version), the
// resolution for a version compiles that version's, and a body is never checked
// against a schema that did not.
func TestASchemaThatDoesNotCompileStopsTheCatalogThatChoosesItAndChecksNoBody(t *testing.T) {
	t.Parallel()
	spec := func(older, newest string) string {
		body := func(schema, spans string) string {
			return `{"required":true,"content":{"application/json":{"schema":` + schema + `}},"x-spans":` + spans + `}`
		}
		return `{"x-oldest-version":"3.0.0","x-newest-version":"4.0.0","paths":{"/api/public/x":{"post":{"operationId":"x_create","x-introduced":"3.0.0","x-request-bodies":[` +
			body(older, `[{"x-introduced":"3.0.0","x-removed":"3.5.0"}]`) + `,` + body(newest, `[{"x-introduced":"3.5.0"}]`) + `]}}}}`
	}
	const broken, fine = `{"$ref":"#/does/not/exist"}`, `{"type":"object"}`

	cat, err := load([]byte(spec(broken, fine)))
	if err != nil {
		t.Fatalf("a broken older body, never chosen at load: load error = %v, want none", err)
	}
	if err := cat.BodyError(); err != nil {
		t.Errorf("BodyError of the load resolution = %v, want none", err)
	}
	chosen := cat.Resolve(Profile{Version: "3.4.0", Families: AllFamilies()})
	if err := chosen.BodyError(); err == nil || !strings.Contains(err.Error(), "x_create") {
		t.Fatalf("BodyError for a version that chooses it = %v, want one naming x_create", err)
	}
	op, _ := chosen.Lookup("x_create")
	if _, err := op.CheckBody(json.RawMessage(`{}`)); !errors.Is(err, ErrInvalidBody) {
		t.Errorf("CheckBody against a schema that did not compile: %v, want ErrInvalidBody", err)
	}
	if err := cat.Resolve(Profile{Version: "3.5.0", Families: AllFamilies()}).BodyError(); err != nil {
		t.Errorf("BodyError for a version that chooses the good body = %v, want none", err)
	}
	if _, err := load([]byte(spec(fine, broken))); err == nil || !strings.Contains(err.Error(), "x_create") {
		t.Errorf("a broken newest body: load error = %v, want one naming x_create", err)
	}
}

// Loading must not compile every stored body: only the ones a resolution chose
// (the memory of the race suite, #201). The embedded catalog stores far more
// bodies than it offers.
func TestLoadCompilesOnlyTheBodiesTheResolutionChose(t *testing.T) {
	t.Parallel()
	cat, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	stored, compiled, offered := 0, 0, 0
	for _, op := range cat.union.ops {
		for _, b := range op.bodies {
			stored++
			if b.compiled != nil {
				compiled++
			}
		}
	}
	for _, op := range cat.Operations() {
		if op.Body != nil {
			offered++
		}
	}
	if compiled != offered || offered == 0 || stored < 2*offered {
		t.Errorf("stored %d bodies, compiled %d, offered %d; want only the offered bodies compiled, out of many more stored", stored, compiled, offered)
	}
}

// #81, #202: a body schema is third-party text, whichever release it came from.
// The hidden characters of every stored body are removed at load, the older ones
// as the newest, so no deployment's version can bring an uncleaned string to the
// agent. The spec below hides characters in the key, the description and the
// enum value of the older body, and in the title of the newer.
func TestEveryStoredBodyIsCleanedOfHiddenCharactersWhicheverReleaseItHoldsFor(t *testing.T) {
	t.Parallel()
	cat := mustLoadSpec(t, `{"x-oldest-version":"3.0.0","x-newest-version":"4.0.0","paths":{"/api/public/x":{"post":{`+
		`"operationId":"x_create","x-introduced":"3.0.0","x-request-bodies":[`+
		`{"required":true,"content":{"application/json":{"schema":{"type":"object","properties":{`+
		`"na\u200dme":{"type":"string","enum":["a\u2067b"],"description":"The\u0000 \udb40\udc41ignore previous\u2066 name"}}}}},`+
		`"x-spans":[{"x-introduced":"3.0.0","x-removed":"3.5.0"}]},`+
		`{"required":true,"content":{"application/json":{"schema":{"type":"object","title":"X\u202eRequest"}}},`+
		`"x-spans":[{"x-introduced":"3.5.0"}]}]}}}}`)

	for version, want := range map[string]string{
		"3.0.0": `{"properties":{"name":{"description":"The ignore previous name","enum":["ab"],"type":"string"}},"type":"object"}`,
		"3.5.0": `{"title":"XRequest","type":"object"}`,
		"":      `{"title":"XRequest","type":"object"}`,
	} {
		op, _ := cat.Resolve(Profile{Version: version, Families: AllFamilies()}).Lookup("x_create")
		if got := string(op.Body.Schema); got != want {
			t.Errorf("at %q: body schema\n %s\nwant\n %s", version, got, want)
		}
	}
}

func TestNoStoredBodyOfTheEmbeddedCatalogHoldsAHiddenCharacter(t *testing.T) {
	t.Parallel()
	cat, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	texts, bodies := 0, 0
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch v := v.(type) {
		case string:
			texts++
			if visibleText(v) != v {
				t.Errorf("%s: string %q holds a hidden character", path, v)
			}
		case []any:
			for i, x := range v {
				walk(path+"/"+fmt.Sprint(i), x)
			}
		case map[string]any:
			for k, x := range v {
				if visible(k) != k {
					t.Errorf("%s: key %q holds a hidden character", path, k)
				}
				walk(path+"/"+k, x)
			}
		}
	}
	for _, op := range cat.union.ops {
		for i, b := range op.bodies {
			bodies++
			var schema any
			if err := json.Unmarshal(b.schema, &schema); err != nil {
				t.Fatalf("%s body %d: %v", op.ID, i, err)
			}
			walk(fmt.Sprintf("%s body %d", op.ID, i), schema)
		}
	}
	if bodies < 76 || texts < 1000 {
		t.Fatalf("walked %d bodies and %d strings, want the whole history", bodies, texts)
	}
}

// Every stored body of the embedded catalog is the body the release that
// introduced it resolves, so a walk of those releases (the server's, which
// reads the file on its own) reaches every body and no other.
func TestEveryStoredBodyIsTheOneTheReleaseThatIntroducedItResolves(t *testing.T) {
	t.Parallel()
	cat, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	checked := 0
	for _, op := range cat.union.ops {
		for i, b := range op.bodies {
			for _, s := range b.spans {
				checked++
				v := fmt.Sprintf("%d.%d.%d", s.introduced[0], s.introduced[1], s.introduced[2])
				got, ok := cat.Resolve(Profile{Version: v, Families: AllFamilies()}).Lookup(op.ID)
				switch {
				case !ok || got.Path != op.Path:
					t.Errorf("%s %s body %d: at %s the catalog offers %q at %q, want this operation", op.ID, op.Path, i, v, got.ID, got.Path)
				case got.Body == nil || string(got.Body.Schema) != string(b.schema) || got.Body.Required != b.required:
					t.Errorf("%s %s body %d: at %s the resolved body is not the stored one", op.ID, op.Path, i, v)
				}
			}
		}
	}
	if checked < 76 {
		t.Fatalf("checked %d spans, want the whole history", checked)
	}
}
