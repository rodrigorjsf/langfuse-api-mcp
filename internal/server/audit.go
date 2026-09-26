package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// audit is what one tool call records for its audit line: metadata only,
// never a payload.
type audit struct {
	// method is the HTTP method of the operation; empty before the operation
	// is known.
	method string
	// status is the HTTP status Langfuse answered; zero without an answer.
	status int
	// bytes is the size of the response body read from Langfuse.
	bytes int
	// cause is why a request that got no usable answer failed; never a payload.
	cause string
	// requests is the number of Langfuse requests the call made.
	requests int
}

// auditedHandler is a tool handler that records its call in a.
type auditedHandler func(ctx context.Context, req *mcp.CallToolRequest, a *audit) (*mcp.CallToolResult, error)

// audited turns handler into a tool handler that logs exactly one structured
// line per call to log (stderr): tool, operationId, method, status, latency,
// bytes and the tool error code, never a payload. A panic in handler becomes an
// internal_error tool result, so that a bug never breaks the session; the
// stack goes to the audit line only. It is the last stop of every result and
// log line, so it redacts the secrets of r from both.
func audited(log *slog.Logger, r sanitize.Redactor, handler auditedHandler) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (res *mcp.CallToolResult, err error) {
		start := time.Now()
		// Redacted before any truncation: a secret cut short no longer matches.
		operationID := r.Redact(sanitize.Text(operationIDOf(req)))
		var a audit
		var panicAttrs []any
		defer func() {
			if p := recover(); p != nil {
				panicAttrs = []any{"panic", r.Redact(fmt.Sprint(p)), "stack", string(debug.Stack())}
				res, err = toolError(errorInternal, "the server hit an internal error",
					"this is a bug in the server, not in the call; report it with the server's stderr log",
					operationID)
			}
			res = redacted(res, r)
			code := errorCodeOf(res)
			level := slog.LevelInfo
			if code == errorInternal {
				level = slog.LevelError
			}
			attrs := []any{
				"tool", req.Params.Name,
				"operationId", truncate(operationID),
				"method", a.method,
				"status", a.status,
				"latencyMs", time.Since(start).Milliseconds(),
				"bytes", a.bytes,
				"requests", a.requests,
				"code", code,
			}
			if a.cause != "" {
				attrs = append(attrs, "cause", r.Redact(a.cause))
			}
			log.Log(context.WithoutCancel(ctx), level, "tool call", append(attrs, panicAttrs...)...)
		}()
		return handler(ctx, req, &a)
	}
}

// errorCodeOf returns the tool error code of res, or "" for a successful result.
func errorCodeOf(res *mcp.CallToolResult) string {
	if res == nil || !res.IsError {
		return ""
	}
	raw, ok := res.StructuredContent.(json.RawMessage)
	if !ok {
		return ""
	}
	var body toolErrorBody
	_ = json.Unmarshal(raw, &body) // best effort: the code only labels the audit line
	return body.Error.Code
}

// redacted returns res with the secrets of r replaced in its JSON text and
// structuredContent (which are the same JSON). The replacement contains no
// character JSON escapes, so the JSON stays valid.
func redacted(res *mcp.CallToolResult, r sanitize.Redactor) *mcp.CallToolResult {
	if res == nil || len(res.Content) != 1 {
		return res
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return res
	}
	clean := r.Redact(text.Text)
	if clean == text.Text {
		return res
	}
	out := *res
	out.Content = []mcp.Content{&mcp.TextContent{Text: clean}}
	out.StructuredContent = json.RawMessage(clean)
	return &out
}
