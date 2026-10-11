package catalog_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Spec #172, ticket #188: an unknown key in a write body is refused only where
// the operation's own schema forbids it. The docs (tools reference, security
// model) say that no in-scope write operation's schema does today, so a
// misspelled optional key reaches Langfuse. These tests keep that sentence
// true: a regenerated union catalog that forbids unknown keys somewhere fails
// the walk below, naming the operation, and the docs are then updated with it.
// (The refusal itself, where a schema does forbid, is proven by
// TestCheckBodyRefusesWhatALiteralSchemaForbidsNamingTheLocationAndKeyword.)

// unknownKeyBans returns, sorted, the JSON pointers of every schema inside v
// that forbids unknown keys: additionalProperties or unevaluatedProperties set
// to false. It reads only schema positions, so a property that is itself named
// additionalProperties, and the data held by const, enum, default and
// examples, are not mistaken for the keyword.
func unknownKeyBans(v any) []string {
	found := walkForBans(v, "")
	slices.Sort(found)
	return found
}

func walkForBans(v any, at string) []string {
	var found []string
	switch v := v.(type) {
	case map[string]any:
		for key, x := range v {
			switch key {
			case "enum", "const", "default", "examples":
				continue
			case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
				if subs, ok := x.(map[string]any); ok {
					for name, sub := range subs {
						found = append(found, walkForBans(sub, at+"/"+key+"/"+name)...)
					}
				}
				continue
			case "additionalProperties", "unevaluatedProperties":
				if allowed, ok := x.(bool); ok && !allowed {
					found = append(found, at+"/"+key)
					continue
				}
			}
			found = append(found, walkForBans(x, at+"/"+key)...)
		}
	case []any:
		for i, x := range v {
			found = append(found, walkForBans(x, fmt.Sprintf("%s/%d", at, i))...)
		}
	}
	return found
}

// The walk covers every stored body of every in-scope write operation, those
// of older releases too (#202): a deployment of that release is checked against
// them.
func TestNoInScopeWriteBodySchemaForbidsAnUnknownKey(t *testing.T) {
	t.Parallel()
	bodies := 0
	for key, stored := range mustLoad(t).StoredBodies() {
		for i, body := range stored {
			bodies++
			var schema any
			if err := json.Unmarshal(body.Schema, &schema); err != nil {
				t.Fatalf("%s body %d schema: %v", key, i, err)
			}
			for _, at := range unknownKeyBans(schema) {
				t.Errorf("operation %s forbids unknown body keys at %q (body %d of %d): a misspelled key is now refused there, "+
					"so update the known limit in docs/reference/tools.md and docs/reference/security-model.md, "+
					"then this test", key, at, i+1, len(stored))
			}
		}
	}
	if bodies < catalog.StoredBodyFloor {
		t.Fatalf("the walk saw %d stored write bodies, want the whole catalog's history (at least %d)", bodies, catalog.StoredBodyFloor)
	}
}

func TestTheUnknownKeyWalkFindsABanAtAnyDepthAndIgnoresNamesAndData(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		schema string
		want   []string
	}{
		"a ban on the root":         {`{"type":"object","additionalProperties":false}`, []string{"/additionalProperties"}},
		"unevaluatedProperties":     {`{"unevaluatedProperties":false}`, []string{"/unevaluatedProperties"}},
		"a nested property":         {`{"properties":{"a":{"properties":{"b":{"additionalProperties":false}}}}}`, []string{"/properties/a/properties/b/additionalProperties"}},
		"an array item and a oneOf": {`{"properties":{"l":{"items":{"additionalProperties":false}}},"oneOf":[{"type":"null"},{"additionalProperties":false}]}`, []string{"/oneOf/1/additionalProperties", "/properties/l/items/additionalProperties"}},
		"extra properties allowed":  {`{"additionalProperties":true,"properties":{"a":{"additionalProperties":{"type":"string"}}}}`, nil},
		"a property named like it":  {`{"properties":{"additionalProperties":{"type":"boolean"}}}`, nil},
		"data in default and enum":  {`{"default":{"additionalProperties":false},"enum":[{"additionalProperties":false}]}`, nil},
		"no object keyword at all":  {`{"type":"string"}`, nil},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var schema any
			if err := json.Unmarshal([]byte(tc.schema), &schema); err != nil {
				t.Fatalf("bad schema literal: %v", err)
			}

			got := unknownKeyBans(schema)

			if !slices.Equal(got, tc.want) {
				t.Errorf("bans = %v, want %v", got, tc.want)
			}
		})
	}
}
