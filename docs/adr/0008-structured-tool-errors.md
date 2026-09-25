---
status: accepted
---
# Every failure is a structured, actionable tool error

All failures after startup — invalid input, Langfuse HTTP errors, TLS/network/timeouts, and recovered panics — are returned as MCP tool results with `isError: true` and a stable `code`, sanitized `message`, `hint`, `retryable` and (when relevant) `httpStatus`/`retryAfterSeconds`. JSON-RPC protocol errors are reserved for malformed requests. The codes are part of the public interface agents and user skills depend on, so renaming one is a breaking change. The server retries only GETs on 5xx/network errors and 429 within the request deadline; writes are never auto-retried. Mapping table: `.claude/rules/errors.md`.
