package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

// A write operation's request body depends on the Langfuse release (#202): the
// union catalog keeps every distinct body the release specs gave the operation,
// each with the releases it holds for, and Resolve picks the body of the
// deployment's version, by the rule that picks the served parameters (#173): the
// body of the newest release that is not newer than the deployment. A body is
// never checked against a schema of another version.

// bodyDocument is a request body as the union catalog (x-request-bodies) or a
// plain OpenAPI spec (requestBody) writes it. A plain body has no spans and
// holds for every release.
type bodyDocument struct {
	Required bool `json:"required"`
	Content  map[string]struct {
		Schema json.RawMessage `json:"schema"`
	} `json:"content"`
	// Spans are the runs of releases the body holds for, each from x-introduced
	// up to, not including, x-removed; a body with no span holds for every one.
	Spans []struct {
		Introduced string `json:"x-introduced"`
		Removed    string `json:"x-removed"`
	} `json:"x-spans"`
}

// storedBody is one distinct request body of an operation and the releases it
// holds for. It is shared read-only by every resolution of the catalog, so the
// schema is compiled once, when a resolution first chooses the body: loading the
// catalog does not compile the bodies no deployment chose (the memory of the
// race suite, #201).
type storedBody struct {
	required bool
	// schema is the body's JSON Schema 2020-12 document, cleaned of hidden
	// characters when the catalog loads (the schema of every stored body, not
	// only the newest).
	schema json.RawMessage
	// spans are the releases the body holds for; never empty.
	spans []span

	once     sync.Once
	compiled *jsonschema.Resolved
	err      error
}

// compile compiles the schema, once.
func (b *storedBody) compile() (*jsonschema.Resolved, error) {
	b.once.Do(func() { b.compiled, b.err = compileSchema(b.schema) })
	return b.compiled, b.err
}

// holdsAt reports whether the body holds for version v. The zero version is a
// deployment whose version was not detected, which has the body no release
// replaced.
func (b *storedBody) holdsAt(v version) bool {
	return slices.ContainsFunc(b.spans, func(s span) bool { return s.serves(v) })
}

// overlaps reports whether the two bodies hold for a release in common.
func (b *storedBody) overlaps(other *storedBody) bool {
	return slices.ContainsFunc(b.spans, func(s span) bool {
		return slices.ContainsFunc(other.spans, s.overlaps)
	})
}

// overlaps reports whether the two ranges have a version in common.
func (s span) overlaps(t span) bool {
	return (t.removed.isZero() || s.introduced.less(t.removed)) && (s.removed.isZero() || t.introduced.less(s.removed))
}

// storeBodies reads the request bodies of operation id: the one body of a plain
// OpenAPI spec, or the bodies of the union catalog, each cleaned of hidden
// characters. newestOnly is the union catalog's mark of an operation that keeps
// its newest body only: the server never offers such an operation. It stops at
// a body it cannot read whole, at spans that overlap, and at an offered
// operation with no history.
func storeBodies(id string, plain *bodyDocument, history []bodyDocument, newestOnly bool) ([]*storedBody, error) {
	switch {
	case plain != nil && len(history) > 0:
		return nil, fmt.Errorf("%s: requestBody and x-request-bodies are both given", id)
	case newestOnly:
		return nil, fmt.Errorf("%s: the catalog keeps only the newest body of this operation, which the server offers", id)
	case plain != nil:
		history = []bodyDocument{*plain}
	}
	stored := make([]*storedBody, 0, len(history))
	for _, d := range history {
		b, err := d.store(id)
		if err != nil {
			return nil, err
		}
		if i := slices.IndexFunc(stored, b.overlaps); i >= 0 {
			return nil, fmt.Errorf("%s: two request bodies hold for the same release", id)
		}
		stored = append(stored, b)
	}
	return stored, nil
}

// store reads the body d of operation id.
func (d bodyDocument) store(id string) (*storedBody, error) {
	content := d.Content["application/json"]
	if len(d.Content) != 1 || len(content.Schema) == 0 {
		return nil, fmt.Errorf("%s: request body is not a single JSON schema", id)
	}
	schema, err := cleanSchema(content.Schema)
	if err != nil {
		return nil, fmt.Errorf("%s request body: %w", id, err)
	}
	b := &storedBody{required: d.Required, schema: schema}
	for _, s := range d.Spans {
		r, err := parseSpan(s.Introduced, s.Removed)
		if err != nil {
			return nil, fmt.Errorf("%s request body: %w", id, err)
		}
		if !r.removed.isZero() && !r.introduced.less(r.removed) {
			return nil, fmt.Errorf("%s request body: span %s to %s ends before it starts", id, s.Introduced, s.Removed)
		}
		b.spans = append(b.spans, r)
	}
	if len(b.spans) == 0 {
		b.spans = []span{{}}
	}
	return b, nil
}

// bodyAt returns the operation's body for version v: the stored body that holds
// for it, with its schema compiled, nil when the release has none. A schema that
// does not compile is an error, and the body comes back without a compiled
// schema, so CheckBody refuses every body instead of checking it against
// nothing.
func (o rangedOperation) bodyAt(v version) (*RequestBody, error) {
	for _, b := range o.bodies {
		if !b.holdsAt(v) {
			continue
		}
		compiled, err := b.compile()
		if err != nil {
			err = fmt.Errorf("%s request body: %w", o.ID, err)
		}
		return &RequestBody{Required: b.required, Schema: b.schema, compiled: compiled}, err
	}
	return nil, nil
}

// BodyError reports why the request body of an operation this catalog offers
// cannot be checked: its schema does not compile as JSON Schema 2020-12. It is
// nil for a catalog whose bodies all compile, which the embedded one is (a test
// compiles every stored body); startup stops on an error, so no body is ever
// checked against a schema that did not compile.
func (c Catalog) BodyError() error { return c.bodyErr }

// joinBodyErrors joins the errors by operation ID, so the message does not
// depend on map order.
func joinBodyErrors(byID map[string]error) error {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	errs := make([]error, len(ids))
	for i, id := range ids {
		errs[i] = byID[id]
	}
	return errors.Join(errs...)
}
