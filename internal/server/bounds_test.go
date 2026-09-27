package server_test

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// boundCase is one out-of-bound value for one spec-given bound of a read
// operation's parameter.
type boundCase struct {
	name        string
	operationID string
	param       string
	args        map[string]any
	// value is the out-of-bound value when it is a string, which neither the
	// refusal nor the audit line may repeat; "" for a number.
	value string
}

// outOfBoundValues returns one value just outside each bound s gives: a
// number below Minimum and above Maximum, a string shorter than MinLength and
// longer than MaxLength. An integer schema gets whole numbers; a schema
// without a type gets a number for a numeric bound and a string for a length
// bound (#83).
func outOfBoundValues(s catalog.Schema) map[string]any {
	whole := s.Type == "integer"
	out := map[string]any{}
	if s.Minimum != nil {
		n := *s.Minimum - 1
		if whole {
			n = math.Ceil(*s.Minimum) - 1
		}
		out["below minimum"] = n
	}
	if s.Maximum != nil {
		n := *s.Maximum + 1
		if whole {
			n = math.Floor(*s.Maximum) + 1
		}
		out["above maximum"] = n
	}
	if s.MinLength != nil && *s.MinLength > 0 {
		out["below minLength"] = strings.Repeat("Q", *s.MinLength-1)
	}
	if s.MaxLength != nil {
		out["above maxLength"] = strings.Repeat("Q", *s.MaxLength+1)
	}
	return out
}

// specBoundCases returns a case for every bound the spec gives a parameter of
// a read operation of cat, items included. The list limit is left out: the
// catalog bounds it itself and it keeps its own refusal
// (TestALimitOutsideOneToTheCapIsRefusedWithAHintWithoutCallingLangfuse).
func specBoundCases(cat catalog.Catalog, now time.Time) []boundCase {
	var cases []boundCase
	for _, op := range readsOf(cat) {
		for _, p := range op.Params {
			if p.In == "query" && p.Name == "limit" {
				continue
			}
			s := p.Schema
			if s.Type == "array" && s.Items != nil {
				s = *s.Items
			}
			for bound, v := range outOfBoundValues(s) {
				args := sampleRead(op, now)
				value := v
				if p.Schema.Type == "array" {
					value = []any{v}
				}
				args["parameters"].(map[string]any)[p.Name] = value
				str, _ := v.(string)
				cases = append(cases, boundCase{
					name:        fmt.Sprintf("%s %s %s", op.ID, p.Name, bound),
					operationID: op.ID, param: p.Name, args: args, value: str,
				})
			}
		}
	}
	return cases
}

// #83: a value outside a bound the spec gives a parameter is refused through
// execute_read with invalid_argument before any request reaches Langfuse,
// and neither the refusal nor the audit line repeats the value. The cases
// come from the embedded union catalog, so the test covers the first bounded
// parameter a union-catalog regeneration brings, with no edit; until then it
// has none and skips (the bound checks are proven at the catalog seam,
// internal/catalog/bounds_test.go).
func TestExecuteReadRefusesAValueOutsideASpecGivenBoundOfTheCatalogWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	cases := specBoundCases(cat, time.Now())
	if len(cases) == 0 {
		t.Skip("no read operation of the embedded union catalog has a spec-given parameter bound yet (#83)")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake, seen := fakeLangfuse(t, http.StatusOK, `{}`)
			var logs syncBuffer
			cs := startCatalog(t, cat, langfuse.New(clientOptions(t, fake)), slog.New(slog.NewJSONHandler(&logs, nil)),
				server.Secrets{Keys: testKeys()}, langfuse.UnknownProfile())

			got := toolErrorOf(t, callExecuteRead(t, cs, tc.args)).Error

			if got.Code != "invalid_argument" || !strings.Contains(got.Message, "parameter "+tc.param+": ") ||
				!strings.Contains(got.Message, "want a ") {
				t.Errorf("error = %+v, want invalid_argument naming parameter %s and its bound", got, tc.param)
			}
			assertNoRequest(t, seen)
			lines := auditLines(t, &logs)
			if len(lines) != 1 || lines[0]["code"] != "invalid_argument" {
				t.Errorf("audit lines = %v, want one with code invalid_argument", lines)
			}
			if tc.value != "" && (strings.Contains(got.Message+got.Hint, tc.value) || strings.Contains(logs.String(), tc.value)) {
				t.Errorf("the refusal or the audit line repeats the value:\n%+v\n%s", got, logs.String())
			}
		})
	}
}
