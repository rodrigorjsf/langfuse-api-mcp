package catalog

import "encoding/json"

// StoredBody is a request body of the catalog's history, for the walks of the
// tests in package catalog_test.
type StoredBody struct {
	Required bool
	Schema   json.RawMessage
}

// StoredBodies returns every request body the catalog stores, by the
// "METHOD path" of the operation: not only the one a resolution chose, but each
// distinct body of every release (#202).
func (c Catalog) StoredBodies() map[string][]StoredBody {
	out := map[string][]StoredBody{}
	for _, op := range c.union.ops {
		for _, b := range op.bodies {
			out[op.Method+" "+op.Path] = append(out[op.Method+" "+op.Path], StoredBody{Required: b.required, Schema: b.schema})
		}
	}
	return out
}
