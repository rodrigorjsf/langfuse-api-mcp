---
paths:
  - "internal/**"
  - "cmd/**"
---
# Error handling — every failure reaches the agent as a handled, actionable error

Contract: ADR-0008. The agent must always receive a structured tool error it can act on — never a crashed transport, raw stack trace, secret, or unbounded upstream body.

## Layers
| Where it fails | Behavior |
|---|---|
| Startup (config, CA explicit sources, invalid host) | exit non-zero with one clear stderr message naming the variable; never start half-configured |
| Tool input invalid | tool result `isError: true`, code `invalid_argument`, which field and why |
| Unknown / excluded operation | `operation_not_found` + hint to call `search_operations` |
| Write requested with writes off | cannot happen via tools: `execute_write` is not registered, so the client gets the SDK's unknown-tool error (no `writes_disabled` code exists) |
| Destructive write (DELETE, PUT, PATCH), client offers no form elicitation (none, or URL mode only; read per request) | `confirmation_unavailable`, static hint (use a client with form elicitation, or the Langfuse UI), not retryable; nothing is sent (#113) |
| Destructive write the user declined or cancelled | `confirmation_declined`, static hint (do not call again unless the user asks again), not retryable; nothing is sent |
| Destructive write re-sent with missing, forged, corrupted, expired or mismatched confirmation data (an accept sent before the server asked, other arguments, another process's state) | `confirmation_invalid`, static hint (call again without confirmation data to be asked afresh), not retryable; nothing is sent |
| Langfuse HTTP error | map status (table below) |
| Network / TLS / timeout | classify with `errors.As` (e.g. `x509.UnknownAuthorityError`, `*tls.CertificateVerificationError`, `net.Error` timeout, `context.DeadlineExceeded`) |
| Bug (panic) | recover at the tool-handler boundary → `internal_error`; stack only to stderr |

Protocol-level JSON-RPC errors are reserved for malformed requests and unknown tools (handled by the SDK). Every other failure is a tool result with `isError: true`.

## Tool error shape (text JSON + `structuredContent`)
`{"error": {"code": "...", "message": "...", "hint": "...", "retryable": bool, "httpStatus": 0, "retryAfterSeconds": 0, "operationId": "..."}}` — `message` ≤ 500 chars, sanitized like any payload; `hint` tells the agent the next useful action.

## Langfuse status mapping
| Status | Code | Hint direction | Retry |
|---|---|---|---|
| 400 | `langfuse_bad_request` | include Langfuse's validation message (truncated, sanitized) | no |
| 401 | `langfuse_unauthorized` | check key pair and that `LANGFUSE_BASE_URL` is the key's region | no |
| 403 | `langfuse_forbidden` | key lacks access (org key required? Enterprise feature?) | no |
| 404 | `langfuse_not_found` | verify the id/name; use search/list operations | no |
| 404 meaning unavailable: HTML body, or a JSON `message`/`error` saying "Langfuse v4 events_only mode" / "Langfuse v4 write mode" | `operation_unavailable` | name the detected version and family; never echo the body (ADR-0012) | no |
| 404/400 on a call with a Folder name (`/` in a folder-capable name, #33) | same code as its row | static hint: verify the name; a proxy may decode `%2F` (langfuse/langfuse#12720), `prompts_list` `name` workaround; dataset runs routes fail upstream (langfuse/langfuse#13933). Replaces the not-found/bad-request hint; follows an `operation_unavailable` hint | no |
| 409/422 | `langfuse_conflict` / `langfuse_unprocessable` | state conflict; re-read before changing | no |
| 413 / body > cap | `response_too_large` | narrow the query: fields, time window, limit | no |
| 429 | `langfuse_rate_limited` | wait `retryAfterSeconds`; metrics budget is small | server retries once if `Retry-After` fits the request deadline, else returns |
| 5xx | `langfuse_unavailable` | transient | server retries **GET only**, exponential backoff + jitter, max 2, within deadline |
| TLS unknown authority, or another certificate verification failure (host name, validity, usage) | `tls_untrusted_certificate` | set `LANGFUSE_CA_CERT`/`LANGFUSE_CA_CERTS_PATH`; check startup log of CA sources; for a verification failure also check that `LANGFUSE_BASE_URL` names the certificate's host | no |
| redirect to another scheme, host or port | `redirect_refused` | the user points `LANGFUSE_BASE_URL` at the redirect target and restarts; never followed, so the keys stay on the configured host | no |
| DNS/connect/proxy, including a connection dropped before any answer (end-of-stream from `http.Client.Do`, #91; a body cut short after the answer started stays `internal_error`) and a proxy that refuses the connection or answers CONNECT with a non-200 status (#32) | `network_error` | check host, `HTTPS_PROXY`/`NO_PROXY`; the proxy's status reason and body are untrusted and never echoed into an error, hint or audit line | GET only |
| timeout / canceled | `timeout` / `canceled` | narrow the query | no |
| held by the server's own rate limit or concurrency cap past the deadline (never sent) | `timeout`, `retryable: true` | call again later, fewer calls at once; operator raises `LANGFUSE_MCP_RATE_LIMIT`/`LANGFUSE_MCP_MAX_CONCURRENCY` | no |

Writes are never retried automatically (not idempotent); a failed write's hint says it was not retried and may or may not have been applied.

## Go mechanics
- Internal errors: wrap with `fmt.Errorf("op %s: %w", id, err)`; one typed `*langfuse.APIError{Status, Code, Message, RetryAfter}` and sentinels for branchable cases; branch with `errors.Is/As`, never on strings.
- Translate to the tool error shape in exactly one place (`internal/server`); lower layers never build MCP results.
- Every error path is tested: one test per row of the mapping table through the tool interface against `httptest`.
- Never swallow an error; if intentionally ignored, `_ =` plus a `// why` comment.
