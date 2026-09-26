# Security (non-negotiable)

Full risk→control mapping with sources: `docs/research/security.md`. Cite OWASP IDs **with the year** (IDs were renumbered: Excessive Agency = LLM06:2025 = LLM03:2026). Run `/security-review` before merging anything touching the areas below.

**Credentials**
- Langfuse keys come only from the server's environment/config — never from tool arguments, never forwarded from the MCP client (no token passthrough). Langfuse keys cannot be read-only, so the server is the only write gate.
- The config file (ADR-0011) must never hold keys: reject `LANGFUSE_PUBLIC_KEY`/`LANGFUSE_SECRET_KEY` found there with a startup error; tests prove it.
- Redact `Authorization`, `sk-lf-…`, `pk-lf-…` and the HTTP bearer token from logs, errors and tool output.

**Write gating** (ADR-0003)
- `execute_read` accepts GET operation IDs only. `execute_write` is not registered unless `LANGFUSE_MCP_ALLOW_WRITES=true`. Tests must prove both.
- The tool set is fixed at startup; it never changes per request or per client.

**Requests to Langfuse** (SSRF / injection)
- Base URL comes only from operator config; `https` required except loopback hosts.
- Model input selects an operation ID + params; reject absolute URLs, schemes, hosts, `//`, `.`/`..` segments, `/` and `\` in path params; percent-encode path params.
- Custom `CheckRedirect`: refuse any redirect that changes scheme, host, or port (Go forwards `Authorization` on same-host and subdomain redirects).
- Timeouts, concurrency cap, rate limit, max response bytes read, max tool-result bytes returned (truncate with a marker + pagination hint); default and cap `limit` on list operations.

**Tool results** (LLM01:2025/2026, MCP06:2025)
- Langfuse payloads are untrusted data: wrap them in a labelled envelope; strip invisible/bidi Unicode (U+E0000–E007F, zero-width, bidi overrides) and control chars; never render HTML/Markdown from them; never echo them unescaped into error text.
- Tool names/descriptions are static strings compiled into the binary — never built from API data.

**Transport**
- stdio: logs to stderr only. HTTP (opt-in): bind 127.0.0.1, require a random ≥256-bit bearer token compared in constant time, validate `Origin` (403 on invalid) and `Host` (loopback only), answer session GET/DELETE with 405.

**TLS** (ADR-0006): system pool + explicit/ambient CAs appended in code; `MinVersion` TLS 1.2; no skip-verify option exists.

**Supply chain**: `govulncheck` in CI; pinned `go.sum`; base image pinned by digest; SBOM for archives *and* image; cosign keyless signing; `actions/attest` provenance; no new dependency without justification.

**Audit**: one structured stderr log line per tool call (tool, operationId, method, status, latency, bytes) — metadata only, never payloads.
