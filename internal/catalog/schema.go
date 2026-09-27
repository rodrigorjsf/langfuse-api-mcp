package catalog

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// A write operation's body is checked against the operation's own JSON Schema
// 2020-12 (#112). jsonschema-go decides whether a body fits; its errors quote
// the value, so a refusal is rebuilt here from the schema alone: the JSON
// location of the failure and the keyword that failed, never a value.

// compileSchema compiles a body schema for validation.
func compileSchema(raw json.RawMessage) (*jsonschema.Resolved, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("not a JSON Schema: %w", err)
	}
	compiled, err := s.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("does not compile as JSON Schema 2020-12: %w", err)
	}
	return compiled, nil
}

// validate checks the valid JSON body against the compiled schema. A body
// that fails it is refused naming the location and keyword (explain).
func (b *RequestBody) validate(body []byte) error {
	var instance any
	if err := json.Unmarshal(body, &instance); err != nil {
		return invalidBodyf("not valid JSON")
	}
	if b.compiled.Validate(instance) == nil {
		return nil
	}
	f := explainer{}.explain(b.compiled.Schema(), instance, "")
	location := f.location
	if location == "" {
		location = "the body root"
	}
	return invalidBodyf("at %s: fails schema keyword %q", location, f.keyword)
}

// failure is where a body fails its schema: a JSON Pointer (RFC 6901) built
// only from schema property names and array indexes, and the keyword.
type failure struct {
	location, keyword string
}

// rank orders the failures of a combinator's alternatives: a deeper location
// is closer to the mistake, and at one depth a value that is present but wrong
// says more than a missing property.
func (f failure) rank() int {
	r := 2 * strings.Count(f.location, "/")
	if f.keyword != "required" {
		r++
	}
	return r
}

// leafKeywords are the keywords checked on the instance itself, in the order
// a failure is reported; each keeps only its own part of a schema.
var leafKeywords = []struct {
	name string
	only func(s *jsonschema.Schema) *jsonschema.Schema
}{
	{"type", func(s *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{Type: s.Type, Types: s.Types}
	}},
	{"const", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{Const: s.Const} }},
	{"enum", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{Enum: s.Enum} }},
	{"minimum", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{Minimum: s.Minimum} }},
	{"maximum", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{Maximum: s.Maximum} }},
	{"exclusiveMinimum", func(s *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{ExclusiveMinimum: s.ExclusiveMinimum}
	}},
	{"exclusiveMaximum", func(s *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{ExclusiveMaximum: s.ExclusiveMaximum}
	}},
	{"multipleOf", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{MultipleOf: s.MultipleOf} }},
	{"minLength", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{MinLength: s.MinLength} }},
	{"maxLength", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{MaxLength: s.MaxLength} }},
	{"pattern", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{Pattern: s.Pattern} }},
	{"minItems", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{MinItems: s.MinItems} }},
	{"maxItems", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{MaxItems: s.MaxItems} }},
	{"uniqueItems", func(s *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{UniqueItems: s.UniqueItems}
	}},
	{"minProperties", func(s *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{MinProperties: s.MinProperties}
	}},
	{"maxProperties", func(s *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{MaxProperties: s.MaxProperties}
	}},
	{"dependentRequired", func(s *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{DependentRequired: s.DependentRequired}
	}},
	{"propertyNames", func(s *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{PropertyNames: s.PropertyNames}
	}},
	{"not", func(s *jsonschema.Schema) *jsonschema.Schema { return &jsonschema.Schema{Not: s.Not} }},
}

// explainer explains a body's failure. It compiles each subschema it checks
// once per refusal, so a long array costs one compilation, not one per item.
type explainer map[*jsonschema.Schema]*jsonschema.Resolved

// explain returns where instance fails schema s, which it is known to fail,
// at location at. It reports the first failing leaf keyword of s, else
// descends into the property, item or subschema that fails, so the location
// is as deep as the schema lets it be. A key of the body that the schema does
// not name never becomes part of a location: it is the caller's text.
func (e explainer) explain(s *jsonschema.Schema, instance any, at string) failure {
	for _, k := range leafKeywords {
		if only := k.only(s); !e.fits(only, instance) {
			return failure{at, k.name}
		}
	}
	if object, ok := instance.(map[string]any); ok {
		for _, name := range s.Required {
			if _, present := object[name]; !present {
				return failure{at + "/" + pointerToken(name), "required"}
			}
		}
		for _, name := range sortedKeys(s.Properties) {
			if v, present := object[name]; present && !e.fits(s.Properties[name], v) {
				return e.explain(s.Properties[name], v, at+"/"+pointerToken(name))
			}
		}
		// additionalProperties applies to the keys properties does not name,
		// so it is checked with them; the failing key stays unnamed.
		if s.AdditionalProperties != nil && !e.fits(&jsonschema.Schema{
			Properties: s.Properties, PatternProperties: s.PatternProperties, AdditionalProperties: s.AdditionalProperties,
		}, instance) {
			return failure{at, "additionalProperties"}
		}
	}
	if items, ok := instance.([]any); ok && s.Items != nil {
		for i, v := range items {
			if !e.fits(s.Items, v) {
				return e.explain(s.Items, v, at+"/"+strconv.Itoa(i))
			}
		}
	}
	for _, sub := range s.AllOf {
		if !e.fits(sub, instance) {
			return e.explain(sub, instance, at)
		}
	}
	for _, c := range []struct {
		name string
		list []*jsonschema.Schema
	}{{"anyOf", s.AnyOf}, {"oneOf", s.OneOf}} {
		if len(c.list) > 0 && !e.fits(&jsonschema.Schema{AnyOf: c.list}, instance) {
			return e.closestAlternative(c.name, c.list, instance, at)
		}
	}
	if len(s.OneOf) > 0 && !e.fits(&jsonschema.Schema{OneOf: s.OneOf}, instance) {
		return failure{at, "oneOf"} // more than one alternative fits
	}
	return failure{at, "schema"}
}

// closestAlternative explains a body that fits no alternative of an anyOf or
// oneOf: the failure of the one alternative whose type fits the instance, or
// the failure that ranks strictly highest among them; otherwise the
// combinator itself, at its own location.
func (e explainer) closestAlternative(keyword string, alternatives []*jsonschema.Schema, instance any, at string) failure {
	var typed []*jsonschema.Schema
	for _, a := range alternatives {
		if e.fits(&jsonschema.Schema{Type: a.Type, Types: a.Types}, instance) {
			typed = append(typed, a)
		}
	}
	if len(typed) == 1 {
		return e.explain(typed[0], instance, at)
	}
	best, tie := failure{at, keyword}, true
	for _, a := range typed {
		f := e.explain(a, instance, at)
		switch {
		case f.rank() > best.rank():
			best, tie = f, false
		case f.rank() == best.rank():
			tie = true
		}
	}
	if tie {
		return failure{at, keyword}
	}
	return best
}

// fits reports whether instance validates against the subschema s. The
// subschema is compiled on its own, which jsonschema-go allows without
// changing it: body schemas hold no $ref (the generator inlines them).
func (e explainer) fits(s *jsonschema.Schema, instance any) bool {
	compiled, ok := e[s]
	if !ok {
		var err error
		if compiled, err = s.Resolve(nil); err != nil {
			return false
		}
		e[s] = compiled
	}
	return compiled.Validate(instance) == nil
}

// pointerToken escapes a property name as a JSON Pointer reference token.
func pointerToken(name string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
}

// sortedKeys returns the keys of m in order, so a refusal is deterministic.
func sortedKeys(m map[string]*jsonschema.Schema) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
