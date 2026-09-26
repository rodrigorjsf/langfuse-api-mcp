package langfuse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"
)

// ErrOperationUnavailable marks a 404 meaning that the connected deployment
// does not serve the operation at all, as opposed to a missing resource
// (ADR-0012). An *APIError with a non-empty Unavailable matches it.
var ErrOperationUnavailable = errors.New("operation unavailable on this deployment")

// Unavailability says why a deployment does not serve an operation.
type Unavailability string

const (
	// RouteMissing: a 404 with an HTML body; the route does not exist in the
	// deployment's Langfuse version.
	RouteMissing Unavailability = "route_missing"
	// EventsOnly: a 404 JSON naming events_only; a Langfuse v4 deployment in
	// events_only mode has the legacy family off.
	EventsOnly Unavailability = "events_only"
	// V4WriteModeOff: a 404 JSON naming "v4 write mode"; the v4 read family is
	// off (Langfuse v3, or v4 in legacy mode).
	V4WriteModeOff Unavailability = "v4_write_mode_off"
)

// APIError is a Langfuse answer with a non-2xx status.
type APIError struct {
	Status int
	// Message is Langfuse's own explanation, taken from a JSON error body
	// (message, error name or validation issues). It is untrusted text: the
	// caller truncates and sanitizes it before anyone sees it. Empty when the
	// body is not JSON, and always empty for an unavailable operation.
	Message string
	// Unavailable is set when the answer means the deployment does not serve
	// the operation; the error then matches ErrOperationUnavailable.
	Unavailable Unavailability
	// RetryAfter is the wait Langfuse asked for in Retry-After (429, 503);
	// zero when it sent none.
	RetryAfter time.Duration
}

// Is reports whether target is ErrOperationUnavailable and e is an
// unavailable-operation answer.
func (e *APIError) Is(target error) bool {
	return target == ErrOperationUnavailable && e.Unavailable != ""
}

// newAPIError classifies a non-2xx answer from its status, Content-Type and
// (bounded) body.
func newAPIError(status int, header http.Header, body []byte) *APIError {
	e := &APIError{Status: status, Message: errorMessage(body), RetryAfter: retryAfter(header, time.Now())}
	if status == http.StatusNotFound {
		e.Unavailable = unavailability(header, body, e.Message)
	}
	if e.Unavailable != "" {
		e.Message = "" // the body of an unavailable answer is never echoed
	}
	return e
}

// unavailability tells the three 404 flavours that mean "not served here"
// (docs/research/langfuse-api-versions.md §2) from a plain not-found.
func unavailability(header http.Header, body []byte, message string) Unavailability {
	mediaType, _, _ := mime.ParseMediaType(header.Get("Content-Type")) // an unparsable type falls back to the body check
	if mediaType == "text/html" || bytes.HasPrefix(bytes.TrimSpace(body), []byte("<")) {
		return RouteMissing
	}
	switch {
	case strings.Contains(message, "events_only"):
		return EventsOnly
	case strings.Contains(message, "v4 write mode"):
		return V4WriteModeOff
	}
	return ""
}

// Error never includes the body: it names the status only, so logging the
// error never logs a payload.
func (e *APIError) Error() string { return fmt.Sprintf("langfuse answered HTTP %d", e.Status) }

// maxErrorBodyBytes caps how much of an error body the client reads: enough
// for any Langfuse error message, small enough to never matter.
const maxErrorBodyBytes = 64 << 10

// errorBody holds the fields of the Langfuse error bodies seen in practice:
// {"message","error":"<Name>Error"}, {"message","code"}, {"message",
// "error":[zod issues]}, {"message","errors":[...]}, {"error":"..."} and the
// SCIM {"detail"}.
type errorBody struct {
	Message string          `json:"message"`
	Error   json.RawMessage `json:"error"`
	Errors  []string        `json:"errors"`
	Detail  string          `json:"detail"`
}

// zodIssue is one validation issue of a Langfuse 400 body.
type zodIssue struct {
	Path    []any  `json:"path"`
	Message string `json:"message"`
}

// errorMessage extracts Langfuse's explanation from an error body. A body
// that is not a JSON object yields "": it is never echoed.
func errorMessage(body []byte) string {
	var b errorBody
	if err := json.Unmarshal(bytes.TrimSpace(body), &b); err != nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if b.Message != "" {
		parts = append(parts, b.Message)
	}
	parts = append(parts, errorField(b.Error, b.Message == "")...)
	parts = append(parts, b.Errors...)
	if b.Detail != "" {
		parts = append(parts, b.Detail)
	}
	return strings.Join(parts, ": ")
}

// errorField returns the useful text of the "error" field: every validation
// issue as "path: message", or the error string when no message was given
// (an error name such as "LangfuseNotFoundError" adds nothing to a message).
func errorField(raw json.RawMessage, noMessage bool) []string {
	var issues []zodIssue
	if err := json.Unmarshal(raw, &issues); err == nil {
		out := make([]string, 0, len(issues))
		for _, is := range issues {
			out = append(out, issueText(is))
		}
		return out
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" && noMessage {
		return []string{s}
	}
	return nil
}

func issueText(is zodIssue) string {
	if len(is.Path) == 0 {
		return is.Message
	}
	path := make([]string, 0, len(is.Path))
	for _, p := range is.Path {
		path = append(path, fmt.Sprint(p))
	}
	return strings.Join(path, ".") + ": " + is.Message
}
