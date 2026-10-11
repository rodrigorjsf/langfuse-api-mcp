package catalog

import (
	"strings"
	"testing"
)

// scanBoundsSpec is a small union catalog whose scores_get-many serves traceId
// only from 3.133.0, and bounds it to 2 to 8 characters: the real spec gives
// the filter no length bound, so the length check of a scanned value is shown
// on this one.
var scanBoundsSpec = []byte(`{"x-oldest-version":"3.0.0","x-newest-version":"4.10.0","paths":{
 "/api/public/v2/scores":{"get":{"operationId":"scores_get-many","tags":["Scores"],"description":"d","x-introduced":"3.53.0","parameters":[
  {"name":"page","in":"query","required":false,"x-introduced":"3.53.0","schema":{"type":"integer"}},
  {"name":"limit","in":"query","required":false,"x-introduced":"3.53.0","schema":{"type":"integer"}},
  {"name":"traceId","in":"query","required":false,"x-introduced":"3.133.0","schema":{"type":"string","minLength":2,"maxLength":8}}]}}}}`)

// The scanned value is checked against the bounds the spec gives the filter,
// in characters, like a value that is sent; the message names the bound and
// never the value.
func TestAScannedValueIsCheckedAgainstTheLengthBoundsOfItsParameter(t *testing.T) {
	t.Parallel()
	cat, err := load(scanBoundsSpec)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	op, ok := cat.Resolve(Profile{Version: "3.80.0", Families: AllFamilies()}).Lookup("scores_get-many")
	if !ok {
		t.Fatal("scores_get-many is not offered on 3.80.0")
	}

	for _, tc := range []struct {
		value, wantErr string
	}{
		{"q", "want a length from 2 to 8 characters"},
		{"123456789", "want a length from 2 to 8 characters"},
		{"é23456é", ""}, // 7 characters, 9 bytes: bounds count characters
		{"12345678", ""},
	} {
		scan, scanned, err := op.Scan(map[string]any{"traceId": tc.value})

		switch {
		case tc.wantErr == "" && (err != nil || !scanned || scan.Value != tc.value):
			t.Errorf("%q: scan = %+v, scanned = %v, err = %v, want a scan of that value", tc.value, scan, scanned, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr) || strings.Contains(err.Error(), tc.value)):
			t.Errorf("%q: err = %v, want it to say %q without the value", tc.value, err, tc.wantErr)
		}
	}
}
