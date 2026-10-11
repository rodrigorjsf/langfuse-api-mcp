package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// A write operation's body schema is mostly prose: the `description` and
// `title` annotations of its Langfuse spec are about two thirds of the largest
// one. The discovery tools use this reduction to keep an operation description
// within the result size cap (#190): the annotations explain, they never decide
// whether a body is valid.

// SchemaWithoutAnnotations returns the body schema without the `description`
// and `title` annotations of the schema and of every subschema in it, compact
// and with its keys in the order Schema has them. Everything else stays: every
// keyword a body is checked against, a property that is itself named
// `description` or `title`, and whatever sits inside const, enum, default and
// examples, which are data. The reduced schema accepts and refuses exactly the
// bodies the full one does. Schema itself is not changed: CheckBody keeps
// validating against the full schema.
func (b *RequestBody) SchemaWithoutAnnotations() (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(b.Schema))
	dec.UseNumber() // numbers stay exactly as the spec wrote them
	var schema any
	if err := dec.Decode(&schema); err != nil {
		return nil, fmt.Errorf("body schema: %w", err)
	}
	dropAnnotations(schema)
	return json.Marshal(schema)
}

// dropAnnotations removes `description` and `title` from schema, if it is a
// schema object, and from every subschema it holds. It follows only the
// keywords of JSON Schema 2020-12 whose value is a schema, a list of schemas or
// an object of named schemas: the names of a `properties` object are not
// keywords, and any other keyword's value is data or an extension, left as is.
func dropAnnotations(schema any) {
	s, ok := schema.(map[string]any)
	if !ok {
		return // a boolean schema
	}
	delete(s, "description")
	delete(s, "title")
	for keyword, value := range s {
		switch keyword {
		case "additionalItems", "additionalProperties", "contains", "contentSchema", "else", "if", "items",
			"not", "propertyNames", "then", "unevaluatedItems", "unevaluatedProperties":
			dropAnnotations(value)
		case "allOf", "anyOf", "oneOf", "prefixItems":
			list, _ := value.([]any)
			for _, sub := range list {
				dropAnnotations(sub)
			}
		case "$defs", "definitions", "dependentSchemas", "patternProperties", "properties":
			named, _ := value.(map[string]any)
			for _, sub := range named {
				dropAnnotations(sub)
			}
		}
	}
}
