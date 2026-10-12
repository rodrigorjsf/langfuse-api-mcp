package sanitize_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// FuzzPayload checks the invariants of a payload shown to the agent, whatever
// JSON Langfuse sent: the result is valid JSON, no string in it, key or value,
// keeps a hidden character or the planted secret, at the top level and at every
// level a string of the result decodes at as JSON text (#210, down to the
// bound), and a second pass changes nothing.
func FuzzPayload(f *testing.F) {
	f.Add(`{"name":"ignore\u202Eprevious\u200Binstructions\u0007","n":12345678901234567890}`)
	f.Add(`["pr` + "\U000E0041\u2066" + `od",null,true,1.5e3,{"a\u200Bb":"line 1\nline 2"}]`)
	f.Add(`"\xff\xfe broken UTF-8"`)
	f.Add(`{"sec\u200Bret":"a secret, sec\u202Eret"}`)
	f.Add(`{"c\u001b[31molour":"\u001b[1;32mgreen\u001b[m \u001b]8;;https://x.example\u0007link\u001b]8;;\u0007 sec\u001b[0mret \u009b2J"}`)
	// JSON text (the Observations v2 input and output), one to five times
	// stringified, with hidden characters, a terminal sequence, the planted
	// secret and a Langfuse key split by escapes.
	inner := `{"a":"x` + uEsc("0001") + uEsc("200b") + `y` + uEsc("001b") + `[31m","sec` + uEsc("200b") + `ret":["a secret, sec` + uEsc("202e") + `ret",1.50],` +
		`"k":"token` + uEsc("200b") + `sk-lf-abc` + uEsc("0007") + `def","Authorization":"Basic dXNlcjpwYXNzd29yZDEyMzQ1Njc4"}`
	for n := 1; n <= 5; n++ {
		text := inner
		for range n {
			quoted, err := json.Marshal(text)
			if err != nil {
				f.Fatal(err)
			}
			text = string(quoted)
		}
		f.Add(`{"output":` + text + `}`)
	}
	f.Add(`{"output":"[\"a\\u200bb\",{\"k\":\"sec\\u0007ret\"}]","n":1}`)
	f.Add(`["{\"a\":\"x\"} {\"b\":\"\\u200b\"}", "\"x\\u001b[31my\"", "{\"a\":"]`)
	f.Fuzz(func(t *testing.T, s string) {
		if !json.Valid([]byte(s)) {
			return // the client passes only valid JSON
		}
		r := sanitize.NewRedactor("secret")
		got, err := sanitize.Payload(json.RawMessage(s), r)
		if err != nil {
			t.Fatalf("Payload(%q) failed: %v", s, err)
		}
		var v any
		dec := json.NewDecoder(bytes.NewReader(got))
		dec.UseNumber() // a number too large for float64 is still JSON
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("Payload(%q) = %q, not JSON: %v", s, got, err)
		}
		assertNoHiddenCharacter(t, v)
		assertCleanAtEveryLevel(t, v, sanitize.MaxJSONTextLevels)
		if strings.Contains(string(got), "secret") {
			t.Fatalf("Payload(%q) = %q keeps the planted secret", s, got)
		}
		again, err := sanitize.Payload(got, r)
		if err != nil || !bytes.Equal(again, got) {
			t.Fatalf("Payload(%q) = %q, but a second pass gives %q (err %v)", s, got, again, err)
		}
	})
}

// assertCleanAtEveryLevel checks a decoded result the way the agent could read
// it: no string, key or value, keeps a hidden character or the planted secret,
// and a string that is JSON text (exactly one object, list or string, as the
// sanitizer reads it) is decoded and checked the same way, for as many levels
// as the sanitizer decodes.
func assertCleanAtEveryLevel(t *testing.T, v any, levels int) {
	t.Helper()
	check := func(s string) {
		t.Helper()
		for _, r := range s {
			if unicode.Is(unicode.Cf, r) || (r >= 0xE0000 && r <= 0xE007F) ||
				unicode.In(r, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point) ||
				(unicode.IsControl(r) && !strings.ContainsRune("\t\n\r", r)) {
				t.Fatalf("a string of the result keeps the hidden character %U: %q", r, s)
			}
		}
		if strings.Contains(s, "secret") {
			t.Fatalf("a string of the result keeps the planted secret: %q", s)
		}
	}
	switch v := v.(type) {
	case string:
		check(v)
		if levels == 0 {
			return
		}
		dec := json.NewDecoder(strings.NewReader(v))
		dec.UseNumber()
		var inner any
		if dec.Decode(&inner) != nil {
			return
		}
		if _, err := dec.Token(); err != io.EOF {
			return
		}
		switch inner.(type) {
		case map[string]any, []any, string:
			assertCleanAtEveryLevel(t, inner, levels-1)
		}
	case []any:
		for _, item := range v {
			assertCleanAtEveryLevel(t, item, levels)
		}
	case map[string]any:
		for k, item := range v {
			check(k)
			assertCleanAtEveryLevel(t, item, levels)
		}
	}
}

func assertNoHiddenCharacter(t *testing.T, v any) {
	t.Helper()
	check := func(s string) {
		for _, r := range s {
			if unicode.Is(unicode.Cf, r) || (r >= 0xE0000 && r <= 0xE007F) ||
				unicode.In(r, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point) ||
				(unicode.IsControl(r) && !strings.ContainsRune("\t\n\r", r)) {
				t.Fatalf("payload string %q keeps the hidden character %U", s, r)
			}
		}
	}
	switch v := v.(type) {
	case string:
		check(v)
	case []any:
		for _, item := range v {
			assertNoHiddenCharacter(t, item)
		}
	case map[string]any:
		for k, item := range v {
			check(k)
			assertNoHiddenCharacter(t, item)
		}
	}
}

// FuzzWrap checks that an envelope never exceeds its limit, that its data is
// valid JSON, and that a truncated page still holds the payload's start,
// whatever the payload.
func FuzzWrap(f *testing.F) {
	f.Add(`"<p>\"quoted\" & é</p>"`, 30000, 65536)
	f.Add(`{"a":"`+strings.Repeat("\\u0001", 10)+`"}`, 20000, 16384)
	f.Add(`"é€😀 <&>"`, 5000, 16384)
	f.Fuzz(func(t *testing.T, item string, repeat, limit int) {
		repeat = min(max(repeat, 1), 40000)
		limit = min(max(limit, 16384), 1<<20) // the range config accepts
		payload := `[` + strings.Repeat(strconv.Quote(item)+",", repeat) + `0]`
		clean, err := sanitize.Payload(json.RawMessage(payload), sanitize.Redactor{})
		if err != nil {
			return // strconv.Quote is not always JSON
		}
		page := json.RawMessage(`{"data":` + string(clean) + `,"meta":{"cursor":"c"}}`)
		oneValue := json.RawMessage(`{"data":[{"id":"r1","input":` + strconv.Quote(strings.Repeat(item, repeat)) +
			`},{"id":"r2"}],"meta":{"cursor":"c"}}`)
		for _, p := range []json.RawMessage{clean, page, oneValue} {
			if !json.Valid(p) {
				continue // strconv.Quote is not always JSON
			}
			env := sanitize.Wrap("trace_list", p, limit, sanitize.Remedies{})
			got, err := json.Marshal(env)
			if err != nil || len(got) > limit || !json.Valid(env.Data) {
				t.Fatalf("envelope is %d bytes (err %v, valid data %v), want valid and at most %d", len(got), err, json.Valid(env.Data), limit)
			}
		}
		if env := sanitize.Wrap("trace_list", page, limit, sanitize.Remedies{}); env.Truncated && !strings.Contains(string(env.Data), `"cursor":"c"`) {
			t.Fatalf("a truncated page lost its meta: %.200s", env.Data)
		}
	})
}

// #156: variation selectors (U+FE00–FE0F, U+E0100–E01EF) encode any byte
// invisibly after a visible character ("variation-selector smuggling"), and
// the Hangul fillers and the combining grapheme joiner render as nothing:
// all are dropped like the zero-width and tag characters.
func TestPayloadDropsVariationSelectorsAndInvisibleFillers(t *testing.T) {
	t.Parallel()
	in := `{"name":"ok\uFE00\uFE0F\uDB40\uDD00\uDB40\uDDEFay\u3164\u115F\uFFA0\u034F"}`

	got, err := sanitize.Payload(json.RawMessage(in), sanitize.Redactor{})
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != `{"name":"okay"}` {
		t.Fatalf("Payload = %s, want {\"name\":\"okay\"}", got)
	}
}

func TestAKeyThatCollidesAfterCleaningNeverShadowsTheCleanKey(t *testing.T) {
	t.Parallel()
	for range 20 { // map iteration order varies between runs
		got, err := sanitize.Payload(json.RawMessage(`{"a\u200Bb":"hidden","ab":"real","z\u202Ey":"first","z\u200By":"second"}`), sanitize.Redactor{})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != `{"ab":"real","zy":"second"}` { // "z\u200By" sorts before "z\u202Ey"
			t.Fatalf("Payload = %s, want the clean key's value kept", got)
		}
	}
}

func TestRedactReplacesLangfuseKeysButNotWordsThatContainTheirPrefix(t *testing.T) {
	t.Parallel()
	got := sanitize.Redactor{}.Redact("key pk-lf-abc123 and a task-lf-name")
	if got != "key [REDACTED] and a task-lf-name" {
		t.Fatalf("Redact = %q", got)
	}
}
