package server

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
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
	errorRedirectRefused      = "redirect_refused"
	errorResponseTooLarge     = "response_too_large"
	// errorConfirmationUnavailable refuses a destructive write the user
	// could not confirm; nothing is sent to Langfuse.
	errorConfirmationUnavailable = "confirmation_unavailable"
	// errorConfirmationDeclined refuses a destructive write the user
	// declined or cancelled; nothing is sent to Langfuse.
	errorConfirmationDeclined = "confirmation_declined"
	// errorConfirmationInvalid refuses a destructive write re-sent with
	// missing, forged, expired or mismatched confirmation data; nothing is
	// sent to Langfuse.
	errorConfirmationInvalid = "confirmation_invalid"
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
// The operation ID may be the caller's raw input: it is cut to maxEchoed and
// cleaned like a message, so no control, invisible or bidi character comes back.
func toolError(code, message, hint, operationID string) (*mcp.CallToolResult, error) {
	return errorResult(toolErrorFields{
		Code: code, Message: message, Hint: hint, OperationID: sanitize.Message(truncate(operationID)),
	})
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
	case status == 413:
		return statusError{errorResponseTooLarge, tooLargeHint, false}
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

// tooLargeHint tells the agent how to get a smaller answer.
const tooLargeHint = "narrow the query: request fewer fields, a shorter time window or a lower limit, " +
	"and page through the rest"

// secondsRoundedUp rounds d up to whole seconds, so that waiting
// retryAfterSeconds is never too short.
func secondsRoundedUp(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

// searchServedOperations points an unavailable operation's hint at discovery,
// never at an external page (mcp-tool-design.md).
const searchServedOperations = "search_operations lists the operations this deployment serves"

// unavailableHint names, per unavailable flavour, the family the operation
// is in or the version the deployment lacks, and where the replacement is;
// then the detected deployment profile: the version and the families on and
// off (ADR-0012 §7). family is the catalog's family of the called operation,
// "" when no family gates it. The Langfuse body is never part of it.
func unavailableHint(why langfuse.Unavailability, family catalog.Family, profile langfuse.DeploymentProfile) string {
	var flavour string
	switch why {
	case langfuse.EventsOnly:
		flavour = "the operation is in the legacy family, which Langfuse v4 in events_only mode turns off; " +
			"use the replacement operation, e.g. observations_getMany (/v2/observations), metrics_metrics (/v2/metrics) " +
			"or scoresV3_getManyV3 (/v3/scores); " + searchServedOperations
	case langfuse.V4WriteModeOff:
		flavour = "the operation is in the v4 read family, which is off on this deployment (it is not in a Langfuse v4 write mode: " +
			"Langfuse v3, or v4 in legacy mode); use the legacy operation instead, e.g. trace_list or legacy_observationsV1_getMany; " +
			searchServedOperations
	default: // langfuse.RouteMissing
		flavour = routeMissingFlavour(family, profile)
	}
	return flavour + "; " + profileSummary(profile)
}

// routeMissingFlavour explains an HTML 404: the route is missing, and the
// called operation's family, when it has one, says whether the deployment
// turns it off. A family outside the fixed list (none, or one the embedded
// catalog should never hold) is not named, so only fixed names are shown.
func routeMissingFlavour(family catalog.Family, profile langfuse.DeploymentProfile) string {
	const (
		missing     = "this route does not exist on the deployment: "
		replacement = "; use the older operation it replaces: " + searchServedOperations
	)
	switch {
	case !slices.Contains(catalog.AllFamilies(), family):
		return missing + "it runs an older Langfuse version that predates the operation, " +
			"or the operation is in a family listed as off below" + replacement
	case !profile.On(family):
		return missing + "the operation is in the " + string(family) + " family, which is off on this deployment" + replacement
	default:
		return missing + "the operation is in the " + string(family) + " family, which is not listed as off, " +
			"so the deployment most likely runs an older Langfuse version that predates the operation" + replacement
	}
}

// profileSummary names the detected Langfuse version and the families on and
// off. The version is untrusted: it appears only when it is a plain
// major.minor.patch version. Family names come from the fixed list, never
// from the profile's own strings.
func profileSummary(profile langfuse.DeploymentProfile) string {
	version := "Langfuse version unknown"
	if v, ok := profile.KnownVersion(); ok {
		version = "Langfuse " + v
	}
	var on, off []string
	for _, f := range catalog.AllFamilies() {
		if profile.On(f) {
			on = append(on, string(f))
		} else {
			off = append(off, string(f))
		}
	}
	return "deployment: " + version + "; families on: " + listOrNone(on) + "; families off: " + listOrNone(off)
}

// listOrNone joins names with ", ", or returns "none" when there are none.
func listOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// langfuseErrorFields translates a Langfuse error answer into the tool error
// fields; ok is false when err is not a Langfuse answer. Langfuse's message is redacted by r before errorResult cuts it
// to length: a secret cut short would no longer match.
func langfuseErrorFields(err error, op catalog.Operation, r sanitize.Redactor, profile langfuse.DeploymentProfile) (toolErrorFields, bool) {
	var apiErr *langfuse.APIError
	if !errors.As(err, &apiErr) {
		return toolErrorFields{}, false
	}
	f := toolErrorFields{HTTPStatus: apiErr.Status, OperationID: op.ID}
	if errors.Is(apiErr, langfuse.ErrOperationUnavailable) {
		f.Code = errorUnavailableOperation
		f.Message = "operation " + op.ID + " is not served by the connected Langfuse deployment (HTTP 404)"
		f.Hint = unavailableHint(apiErr.Unavailable, op.Family, profile)
		return f, true
	}
	se := statusErrorFor(apiErr.Status)
	f.Code, f.Hint, f.Retryable = se.code, se.hint, se.retryable
	f.RetryAfterSeconds = secondsRoundedUp(apiErr.RetryAfter)
	f.Message = "Langfuse answered HTTP " + strconv.Itoa(apiErr.Status)
	if apiErr.Message != "" {
		f.Message += ": " + r.Redact(apiErr.Message)
	}
	return f, true
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
