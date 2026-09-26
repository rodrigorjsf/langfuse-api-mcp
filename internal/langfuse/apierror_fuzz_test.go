package langfuse

import (
	"net/http"
	"testing"
)

// FuzzNewAPIError parses arbitrary Langfuse error bodies (go.md: fuzz every
// parser of untrusted input). It must never panic, and an answer classified
// as unavailable must never carry the body's text.
func FuzzNewAPIError(f *testing.F) {
	f.Add(400, "application/json", `{"message":"Invalid request data","error":[{"path":["limit"],"message":"Too big"}]}`)
	f.Add(404, "application/json", `{"message":"This endpoint is not available on deployments running in Langfuse v4 events_only mode."}`)
	f.Add(404, "text/html", `<!DOCTYPE html><html lang="en"></html>`)
	f.Add(403, "application/json", `{"error":"Organization-scoped API key required"}`)
	f.Add(422, "application/scim+json", `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"detail":"bad"}`)
	f.Add(429, "application/json", `{"message":[1,{"a":null}],"error":{"x":1},"errors":"no"}`)
	f.Fuzz(func(t *testing.T, status int, contentType, body string) {
		header := http.Header{"Content-Type": {contentType}, "Retry-After": {body}}
		e := newAPIError(status, header, []byte(body))
		if e.Unavailable != "" && e.Message != "" {
			t.Fatalf("an unavailable answer carries the body's message %q", e.Message)
		}
		if e.RetryAfter < 0 {
			t.Fatalf("negative Retry-After %v from %q", e.RetryAfter, body)
		}
	})
}
