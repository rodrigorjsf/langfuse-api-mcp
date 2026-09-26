package server

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// Tool error codes (ADR-0008). They are part of the public interface: renaming
// one is a breaking change.
const (
	errorInvalidArgument      = "invalid_argument"
	errorOperationNotFound    = "operation_not_found"
	errorInternal             = "internal_error"
	errorBadRequest           = "langfuse_bad_request"
	errorUnauthorized         = "langfuse_unauthorized"
	errorForbidden            = "langfuse_forbidden"
	errorNotFound             = "langfuse_not_found"
	errorConflict             = "langfuse_conflict"
	errorUnprocessable        = "langfuse_unprocessable"
	errorUnavailableOperation = "operation_unavailable"
	errorRateLimited          = "langfuse_rate_limited"
	errorLangfuseUnavailable  = "langfuse_unavailable"
)

// toolErrorBody is the ADR-0008 tool error shape.
type toolErrorBody struct {
	Error toolErrorFields `json:"error"`
}

type toolErrorFields struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	Hint              string `json:"hint"`
	Retryable         bool   `json:"retryable"`
	HTTPStatus        int    `json:"httpStatus"`
	RetryAfterSeconds int    `json:"retryAfterSeconds"`
	OperationID       string `json:"operationId"`
}

// toolError returns a tool result with isError set and the ADR-0008 shape.
func toolError(code, message, hint, operationID string) (*mcp.CallToolResult, error) {
	return errorResult(toolErrorFields{Code: code, Message: message, Hint: hint, OperationID: operationID})
}

// errorResult returns f as a tool error; the message is sanitized and cut to
// the ADR-0008 limit on the way out.
func errorResult(f toolErrorFields) (*mcp.CallToolResult, error) {
	f.Message = sanitize.Message(f.Message)
	return jsonResult(toolErrorBody{Error: f}, true)
}

// statusError is the tool error for one Langfuse status (.claude/rules/errors.md).
type statusError struct {
	code, hint string
	retryable  bool
}

// statusErrorFor returns the tool error for a Langfuse status. A 4xx the
// mapping table does not name (e.g. 405) is a refused request, reported as
// langfuse_bad_request.
func statusErrorFor(status int) statusError {
	switch {
	case status == 401:
		return statusError{errorUnauthorized, "the key pair was rejected: check LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY, " +
			"and that LANGFUSE_BASE_URL is the host of the keys' region (keys only work in their own region)", false}
	case status == 403:
		return statusError{errorForbidden, "the key lacks access to this operation: it may need an organization-scoped key " +
			"or an Enterprise feature; retrying will not help", false}
	case status == 404:
		return statusError{errorNotFound, "verify the ID or name; list operations (e.g. trace_list) find existing ones", false}
	case status == 409:
		return statusError{errorConflict, "the request conflicts with the current state: read the resource again before changing it", false}
	case status == 422:
		return statusError{errorUnprocessable, "Langfuse could not process the request: read the resource again and check the parameters", false}
	case status == 429:
		return statusError{errorRateLimited, "Langfuse's rate limit was hit: wait retryAfterSeconds before calling again " +
			"(0 means Langfuse named no wait: back off before retrying); metrics operations have a small daily budget on some plans", true}
	case status >= 500:
		// The server already retried a GET within the deadline.
		return statusError{errorLangfuseUnavailable,
			"Langfuse is temporarily unavailable and the server already retried; call again later", true}
	case status == 400:
		return statusError{errorBadRequest, "fix the parameters named in the message and call again", false}
	default:
		return statusError{errorBadRequest, "Langfuse refused the request as sent: check the operation and its parameters", false}
	}
}

// secondsRoundedUp rounds d up to whole seconds, so that waiting
// retryAfterSeconds is never too short.
func secondsRoundedUp(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

// unavailableHint names, per unavailable flavour, the version or family the
// deployment lacks and where the replacement is. The detected version comes
// with the deployment profile of ADR-0012 (M3); see #34.
func unavailableHint(why langfuse.Unavailability) string {
	switch why {
	case langfuse.EventsOnly:
		return "the deployment runs Langfuse v4 in events_only mode, which turns the legacy family off; " +
			"use the replacement operation, e.g. observations_getMany (/v2/observations), metrics_metrics (/v2/metrics) " +
			"or scoresV3_getManyV3 (/v3/scores) (see https://langfuse.com/faq/all/deprecated-api-migration)"
	case langfuse.V4WriteModeOff:
		return "the v4 read family is off on this deployment (it is not in a Langfuse v4 write mode: " +
			"Langfuse v3, or v4 in legacy mode); use the legacy operation instead, e.g. trace_list or legacy_observationsV1_getMany"
	default: // langfuse.RouteMissing
		return "this route does not exist on the deployment: it runs an older Langfuse version " +
			"that predates the operation; use the older operation it replaces " +
			"(see https://langfuse.com/faq/all/deprecated-api-migration)"
	}
}

// langfuseError translates a failed Langfuse request into a tool error. It
// returns a nil result when err is not a Langfuse answer.
func langfuseError(err error, operationID string) (*mcp.CallToolResult, error) {
	var apiErr *langfuse.APIError
	if !errors.As(err, &apiErr) {
		return nil, nil
	}
	f := toolErrorFields{HTTPStatus: apiErr.Status, OperationID: operationID}
	if errors.Is(apiErr, langfuse.ErrOperationUnavailable) {
		f.Code = errorUnavailableOperation
		f.Message = "operation " + operationID + " is not served by the connected Langfuse deployment (HTTP 404)"
		f.Hint = unavailableHint(apiErr.Unavailable)
		return errorResult(f)
	}
	se := statusErrorFor(apiErr.Status)
	f.Code, f.Hint, f.Retryable = se.code, se.hint, se.retryable
	f.RetryAfterSeconds = secondsRoundedUp(apiErr.RetryAfter)
	f.Message = "Langfuse answered HTTP " + strconv.Itoa(apiErr.Status)
	if apiErr.Message != "" {
		f.Message += ": " + apiErr.Message
	}
	return errorResult(f)
}

// jsonResult returns v as JSON text plus structuredContent.
func jsonResult(v any, isError bool) (*mcp.CallToolResult, error) {
	text, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
		StructuredContent: json.RawMessage(text),
		IsError:           isError,
	}, nil
}
