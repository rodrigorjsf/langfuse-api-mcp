package catalog

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// servedSpec is a small union catalog (3.0.0 to 4.10.0) with one list
// operation whose parameters have their own ranges: page for ever, traceId
// from 3.133.0, a legacy one up to, not including, 3.100.0, and level since
// 3.82.0; and an operation introduced later, whose parameter starts with it.
var servedSpec = []byte(`{"x-oldest-version":"3.0.0","x-newest-version":"4.10.0","paths":{
 "/api/public/things":{"get":{"operationId":"things_list","tags":["T"],"description":"d","x-introduced":"3.0.0","parameters":[
  {"name":"page","in":"query","required":false,"x-introduced":"3.0.0","schema":{"type":"integer"}},
  {"name":"traceId","in":"query","required":false,"x-introduced":"3.133.0","schema":{"type":"string"}},
  {"name":"level","in":"query","required":false,"x-introduced":"3.82.0","schema":{"type":"string","enum":["DEBUG","ERROR"]}},
  {"name":"legacy","in":"query","required":false,"x-introduced":"3.0.0","x-removed":"3.100.0","schema":{"type":"string"}}]}},
 "/api/public/late":{"get":{"operationId":"late_list","tags":["T"],"description":"d","x-introduced":"3.150.0","parameters":[
  {"name":"page","in":"query","required":false,"x-introduced":"3.150.0","schema":{"type":"integer"}}]}},
 "/api/public/plain":{"get":{"operationId":"plain_list","tags":["T"],"description":"d","parameters":[
  {"name":"page","in":"query","required":false,"schema":{"type":"integer"}}]}}}}`)

func servedNames(t *testing.T, version, operationID string) []string {
	t.Helper()
	cat, err := load(servedSpec)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	op, ok := cat.Resolve(Profile{Version: version, Families: AllFamilies()}).Lookup(operationID)
	if !ok {
		t.Fatalf("%s not offered at %q", operationID, version)
	}
	names := []string{}
	for _, p := range op.Params {
		names = append(names, p.Name)
	}
	return names
}

func TestAnOperationOffersTheParametersTheVersionServes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version string
		want    []string
	}{
		{"3.80.0", []string{"page", "legacy"}},
		{"3.82.0", []string{"page", "level", "legacy"}},
		{"3.99.9", []string{"page", "level", "legacy"}},
		{"3.100.0", []string{"page", "level"}},
		{"3.133.0", []string{"page", "traceId", "level"}},
		{"4.10.0", []string{"page", "traceId", "level"}},
		{"9.0.0", []string{"page", "traceId", "level"}},
		{"2.95.0", []string{"page", "legacy"}},
		{"", []string{"page", "traceId", "level"}},
		{"not a version", []string{"page", "traceId", "level"}},
	} {
		if got := servedNames(t, tc.version, "things_list"); !slices.Equal(got, tc.want) {
			t.Errorf("version %q serves %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestAParameterWithoutARangeIsServedEverywhere(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.0", "3.80.0", "4.10.0", ""} {
		if got := servedNames(t, version, "plain_list"); !slices.Equal(got, []string{"page"}) {
			t.Errorf("version %q serves %v, want [page]", version, got)
		}
	}
}

func TestAParameterOfAnOperationIntroducedLaterStartsWithTheOperation(t *testing.T) {
	t.Parallel()
	if got := servedNames(t, "3.150.0", "late_list"); !slices.Equal(got, []string{"page"}) {
		t.Errorf("3.150.0 serves %v, want [page]", got)
	}
}

func requestAt(t *testing.T, version, operationID string, params map[string]any) error {
	t.Helper()
	cat, err := load(servedSpec)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	op, _ := cat.Resolve(Profile{Version: version, Families: AllFamilies()}).Lookup(operationID)
	_, err = op.Request(params)
	return err
}

func TestACallWithAParameterTheVersionDoesNotServeIsRefusedNamingTheReleaseThatServesIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version, param string
		want           []string
	}{
		{"3.80.0", "traceId", []string{`"traceId"`, "things_list", "3.80.0", "from 3.133.0"}},
		{"3.80.0", "level", []string{`"level"`, "3.80.0", "from 3.82.0"}},
		{"3.110.0", "legacy", []string{`"legacy"`, "3.110.0", "up to before 3.100.0"}},
		{"", "legacy", []string{`"legacy"`, "not detected", "up to before 3.100.0"}},
	} {
		err := requestAt(t, tc.version, "things_list", map[string]any{tc.param: "sk-lf-00000000-0000-0000-0000-000000000000"})
		if !errors.Is(err, ErrParameterNotServed) || !errors.Is(err, ErrInvalidParameter) {
			t.Errorf("%s at %q: error %v, want ErrParameterNotServed wrapping ErrInvalidParameter", tc.param, tc.version, err)
			continue
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s at %q: error %q does not contain %q", tc.param, tc.version, err, want)
			}
		}
		if strings.Contains(err.Error(), "sk-lf") {
			t.Errorf("%s at %q: error %q echoes the value", tc.param, tc.version, err)
		}
	}
}

func TestAServedParameterOfTheVersionStillGetsEveryCheck(t *testing.T) {
	t.Parallel()
	if err := requestAt(t, "3.80.0", "things_list", map[string]any{"page": "one"}); !errors.Is(err, ErrInvalidParameter) ||
		errors.Is(err, ErrParameterNotServed) {
		t.Errorf("page %q: error %v, want a plain ErrInvalidParameter", "one", err)
	}
	if err := requestAt(t, "3.90.0", "things_list", map[string]any{"level": "FATAL"}); !errors.Is(err, ErrInvalidParameter) ||
		errors.Is(err, ErrParameterNotServed) || strings.Contains(err.Error(), "FATAL") {
		t.Errorf("level FATAL: error %v, want an enum refusal that does not echo the value", err)
	}
	if err := requestAt(t, "3.80.0", "things_list", map[string]any{"page": float64(2), "legacy": "x"}); err != nil {
		t.Errorf("served parameters: error %v, want none", err)
	}
}

func TestAnUnknownParameterKeepsTheUnknownParameterRefusal(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("n", 100)
	for _, name := range []string{"nope", long} {
		err := requestAt(t, "3.80.0", "things_list", map[string]any{name: "v"})
		if !errors.Is(err, ErrInvalidParameter) || errors.Is(err, ErrParameterNotServed) ||
			!strings.Contains(err.Error(), "unknown parameter for operation things_list") ||
			strings.Contains(err.Error(), long) {
			t.Errorf("name %q: error %v, want the unknown-parameter refusal with the name cut", name, err)
		}
	}
}
