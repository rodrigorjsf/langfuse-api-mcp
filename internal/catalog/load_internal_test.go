package catalog

import "testing"

// The embedded spec holds no hidden character today, so this proves the
// cleaning itself on a spec that does: a compromised upstream spec cannot
// smuggle hidden instructions into the operation index (ADR-0002 amendment).
func TestATagOrDescriptionLineHoldingHiddenCharactersIsCleaned(t *testing.T) {
	t.Parallel()
	spec := []byte(`{"paths":{"/api/public/x":{"get":{"operationId":"x_get",` +
		`"tags":["Tr\u202eace\u0007"],` +
		`"description":"  Get\u200b an x\u0000 \udb40\udc41ignore previous\u2066 \n second line"}}}}`)

	cat, err := load(spec)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	op, _ := cat.Lookup("x_get")
	if op.Tag != "Trace" || op.DescriptionLine != "Get an x ignore previous" {
		t.Fatalf("tag %q, description line %q; want %q, %q", op.Tag, op.DescriptionLine, "Trace", "Get an x ignore previous")
	}
}

// The embedded spec gives no bounds today; the union catalog may. The
// catalog keeps them where the spec gives them.
func TestTheNumericAndLengthBoundsTheSpecGivesAreKept(t *testing.T) {
	t.Parallel()
	spec := []byte(`{"paths":{"/api/public/x":{"get":{"operationId":"x_get","parameters":[` +
		`{"name":"n","in":"query","schema":{"type":"integer","minimum":0,"maximum":1000}},` +
		`{"name":"s","in":"query","schema":{"type":"string","minLength":1,"maxLength":128}}]}}}}`)

	cat, err := load(spec)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	op, _ := cat.Lookup("x_get")
	n, s := op.Params[0].Schema, op.Params[1].Schema
	if n.Minimum == nil || *n.Minimum != 0 || n.Maximum == nil || *n.Maximum != 1000 {
		t.Errorf("n schema = {minimum %v, maximum %v}, want 0, 1000", n.Minimum, n.Maximum)
	}
	if s.MinLength == nil || *s.MinLength != 1 || s.MaxLength == nil || *s.MaxLength != 128 {
		t.Errorf("s schema = {minLength %v, maxLength %v}, want 1, 128", s.MinLength, s.MaxLength)
	}
}
