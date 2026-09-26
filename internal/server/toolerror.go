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
	errorInvalidArgument   = "invalid_argument"
	errorOperationNotFound = "operation_not_found"
	errorInternal          = "internal_error"
	errorBadRequest        = "langfuse_bad_request"
	errorUnauthorized      = "langfuse_unauthorized"
	errorForbidden         = "langfuse_forbidden"
	errorNotFound          = "langfuse_not_found"
	errorConflict          = "langfuse_conflict"
	errorUnprocessable     = "langfuse_unprocessable"
	errorUnavailableOp     = "operation_unavailable"
	errorRateLimited       = "langfuse_rate_limited"
	errorUnavailable       = "langfuse_unavailable"
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

var statusErrors = map[int]statusError{
	400: {errorBadRequest, "fix the parameters named in the message and call again", false},
	401: {errorUnauthorized, "the key pair was rejected: check LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY, " +
		"and that LANGFUSE_BASE_URL is the host of the keys' region (keys only work in their own region)", false},
	403: {errorForbidden, "the key lacks access to this operation: it may need an organization-scoped key " +
		"or an Enterprise feature; retrying will not help", false},
	404: {errorNotFound, "verify the ID or name; list operations (e.g. trace_list) find existing ones", false},
	409: {errorConflict, "the request conflicts with the current state: read the resource again before changing it", false},
	422: {errorUnprocessable, "Langfuse could not process the request: read the resource again and check the parameters", false},
	429: {errorRateLimited, "Langfuse's rate limit was hit: wait retryAfterSeconds " +
		"before calling again; metrics operations have a small daily budget on some plans", true},
}

// serverError is the tool error for any 5xx: the server already retried a GET
// within the deadline, so what is left is transient but persistent.
var serverError = statusError{errorUnavailable,
	"Langfuse is temporarily unavailable and the server already retried; call again later", true}

// otherClientError is the tool error for a 4xx the mapping table does not
// name (e.g. 405): Langfuse refused the request as sent.
var otherClientError = statusError{errorBadRequest, "Langfuse refused the request as sent: check the operation and its parameters", false}

// statusErrorFor returns the tool error for a Langfuse status.
func statusErrorFor(status int) statusError {
	if se, ok := statusErrors[status]; ok {
		return se
	}
	if status >= 500 {
		return serverError
	}
	return otherClientError
}

// seconds rounds d up to whole seconds, so that waiting retryAfterSeconds is
// never too short.
func seconds(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

// unavailableHints name, per unavailable flavour, the version or family the
// deployment lacks and where the replacement is. The deployment profile of
// ADR-0012 (M3) will add the detected version.
var unavailableHints = map[langfuse.Unavailability]string{
	langfuse.RouteMissing: "this route does not exist on the deployment: it runs an older Langfuse version " +
		"that predates the operation; use the older operation it replaces " +
		"(see https://langfuse.com/faq/all/deprecated-api-migration)",
	langfuse.EventsOnly: "the deployment runs Langfuse v4 in events_only mode, which turns the legacy family off; " +
		"use the replacement operation, e.g. observations_getMany (/v2/observations), metrics_metrics (/v2/metrics) " +
		"or scoresV3_getManyV3 (/v3/scores) " +
		"(see https://langfuse.com/faq/all/deprecated-api-migration)",
	langfuse.V4WriteModeOff: "the v4 read family is off on this deployment (it is not in a Langfuse v4 write mode: " +
		"Langfuse v3, or v4 in legacy mode); use the legacy operation instead, e.g. trace_list or legacy_observationsV1_getMany",
}

// langfuseError translates a failed Langfuse request into a tool error. It
// reports false when err is not a Langfuse answer.
func langfuseError(err error, operationID string) (*mcp.CallToolResult, bool, error) {
	var apiErr *langfuse.APIError
	if !errors.As(err, &apiErr) {
		return nil, false, nil
	}
	if errors.Is(apiErr, langfuse.ErrOperationUnavailable) {
		res, rerr := errorResult(toolErrorFields{
			Code:        errorUnavailableOp,
			Message:     "operation " + operationID + " is not served by the connected Langfuse deployment (HTTP 404)",
			Hint:        unavailableHints[apiErr.Unavailable],
			HTTPStatus:  apiErr.Status,
			OperationID: operationID,
		})
		return res, true, rerr
	}
	se := statusErrorFor(apiErr.Status)
	message := "Langfuse answered HTTP " + strconv.Itoa(apiErr.Status)
	if apiErr.Message != "" {
		message += ": " + apiErr.Message
	}
	res, rerr := errorResult(toolErrorFields{
		Code: se.code, Message: message, Hint: se.hint, Retryable: se.retryable,
		HTTPStatus: apiErr.Status, RetryAfterSeconds: seconds(apiErr.RetryAfter), OperationID: operationID,
	})
	return res, true, rerr
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
