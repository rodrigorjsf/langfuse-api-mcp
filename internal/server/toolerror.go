package server

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool error codes (ADR-0008). They are part of the public interface: renaming
// one is a breaking change.
const (
	errorInvalidArgument   = "invalid_argument"
	errorOperationNotFound = "operation_not_found"
	errorInternal          = "internal_error"
	errorRedirectRefused   = "redirect_refused"
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
	return jsonResult(toolErrorBody{Error: toolErrorFields{
		Code: code, Message: message, Hint: hint, OperationID: truncate(operationID), // the ID may be the caller's raw input
	}}, true)
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
