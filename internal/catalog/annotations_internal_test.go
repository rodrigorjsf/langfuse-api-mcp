package catalog

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Seam (spec #172, ticket #190): the catalog reduces a write operation's body
// schema for the discovery tools by leaving out its `description` and `title`
// annotations, and nothing else. It is pure data: the tests build a
// RequestBody around a schema literal, as the catalog does at load.

// annotatedSchema holds both annotations on every kind of subschema, a
// property that is itself named description and one named title, and the
// words description and title as data inside enum, const, default and examples.
const annotatedSchema = `{
  "$defs": {"unit": {"title": "Unit", "description": "a unit", "type": "string"}},
  "additionalProperties": false,
  "description": "Root description",
  "properties": {
    "choice": {"oneOf": [
      {"description": "first", "title": "One", "type": "string"},
      {"title": "Two", "type": "integer"}
    ]},
    "description": {"description": "a property that is itself named description", "type": "string"},
    "fixed": {"const": {"description": "inside const", "title": "inside const"}, "title": "Fixed"},
    "mode": {
      "description": "the mode",
      "enum": ["description", "title", {"description": "inside enum", "title": "inside enum"}],
      "title": "Mode"
    },
    "size": {"default": {"description": "inside default"}, "description": "the size", "type": ["object", "null"]},
    "tags": {
      "examples": [[{"description": "inside examples"}]],
      "items": {"description": "a tag", "title": "Tag", "type": "string"},
      "type": "array"
    },
    "title": {"title": "a property that is itself named title", "type": "integer"}
  },
  "required": ["description", "mode"],
  "title": "Root",
  "type": "object"
}`

// reducedSchema is annotatedSchema without its annotations: written by hand,
// keys in the order the catalog stores a schema (sorted), compact.
const reducedSchema = `{"$defs":{"unit":{"type":"string"}},"additionalProperties":false,` +
	`"properties":{"choice":{"oneOf":[{"type":"string"},{"type":"integer"}]},` +
	`"description":{"type":"string"},` +
	`"fixed":{"const":{"description":"inside const","title":"inside const"}},` +
	`"mode":{"enum":["description","title",{"description":"inside enum","title":"inside enum"}]},` +
	`"size":{"default":{"description":"inside default"},"type":["object","null"]},` +
	`"tags":{"examples":[[{"description":"inside examples"}]],"items":{"type":"string"},"type":"array"},` +
	`"title":{"type":"integer"}},"required":["description","mode"],"type":"object"}`

func bodyOf(t *testing.T, schema string) *RequestBody {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(schema)); err != nil {
		t.Fatalf("schema literal: %v", err)
	}
	cleaned, err := cleanSchema(compact.Bytes())
	if err != nil {
		t.Fatalf("clean schema: %v", err)
	}
	compiled, err := compileSchema(cleaned)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return &RequestBody{Required: true, Schema: cleaned, compiled: compiled}
}

func TestABodySchemaWithoutAnnotationsLeavesOutTheDescriptionAndTitleOfEverySubschemaOnly(t *testing.T) {
	t.Parallel()
	body := bodyOf(t, annotatedSchema)

	got, err := body.SchemaWithoutAnnotations()

	if err != nil {
		t.Fatalf("SchemaWithoutAnnotations: %v", err)
	}
	if string(got) != reducedSchema {
		t.Fatalf("reduced schema =\n%s\nwant\n%s", got, reducedSchema)
	}
}

func TestReducingABodySchemaDoesNotChangeTheSchemaExecuteWriteChecksBodiesAgainst(t *testing.T) {
	t.Parallel()
	body := bodyOf(t, annotatedSchema)
	before := bytes.Clone(body.Schema)

	if _, err := body.SchemaWithoutAnnotations(); err != nil {
		t.Fatalf("SchemaWithoutAnnotations: %v", err)
	}

	if !bytes.Equal(body.Schema, before) {
		t.Fatalf("Schema changed:\n%s\nwas\n%s", body.Schema, before)
	}
}

// The reduced schema and the full one accept and refuse the same bodies.
func TestTheReducedBodySchemaAcceptsAndRefusesTheSameBodiesAsTheFullOne(t *testing.T) {
	t.Parallel()
	full := bodyOf(t, annotatedSchema)
	reducedRaw, err := full.SchemaWithoutAnnotations()
	if err != nil {
		t.Fatalf("SchemaWithoutAnnotations: %v", err)
	}
	reduced, err := compileSchema(reducedRaw)
	if err != nil {
		t.Fatalf("the reduced schema does not compile as JSON Schema 2020-12: %v", err)
	}
	tests := map[string]struct {
		body     string
		accepted bool
	}{
		"a valid body": {`{"description":"x","mode":"title"}`, true},
		"an enum member that is the word description":   {`{"description":"x","mode":"description"}`, true},
		"the enum member that is an object":             {`{"description":"x","mode":{"description":"inside enum","title":"inside enum"}}`, true},
		"a value that is no enum member":                {`{"description":"x","mode":"other"}`, false},
		"a body missing a required property":            {`{"mode":"title"}`, false},
		"a body with a wrong type":                      {`{"description":1,"mode":"title"}`, false},
		"a body with an unknown key the schema forbids": {`{"description":"x","mode":"title","extra":1}`, false},
		"the const value":                               {`{"description":"x","mode":"title","fixed":{"description":"inside const","title":"inside const"}}`, true},
		"another value where a const is":                {`{"description":"x","mode":"title","fixed":{"description":"other"}}`, false},
		"the property named title, an integer":          {`{"description":"x","mode":"title","title":7}`, true},
		"the property named title, a string":            {`{"description":"x","mode":"title","title":"seven"}`, false},
		"a oneOf alternative":                           {`{"description":"x","mode":"title","choice":"text"}`, true},
		"no oneOf alternative":                          {`{"description":"x","mode":"title","choice":true}`, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var instance any
			if err := json.Unmarshal([]byte(tc.body), &instance); err != nil {
				t.Fatalf("body literal: %v", err)
			}

			fullAccepts := full.compiled.Validate(instance) == nil
			reducedAccepts := reduced.Validate(instance) == nil

			if fullAccepts != tc.accepted || reducedAccepts != tc.accepted {
				t.Fatalf("body %s: full schema accepts %v, reduced schema accepts %v, want both %v",
					tc.body, fullAccepts, reducedAccepts, tc.accepted)
			}
		})
	}
}
