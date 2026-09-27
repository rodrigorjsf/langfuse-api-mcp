# Security requirements research — langfuse-api-mcp

Access date for every source: **2026-09-25**. Scope: the security controls a Go MCP server
(official `modelcontextprotocol/go-sdk`, stateless, stdio by default, optional Streamable HTTP on
loopback with a token) needs when it wraps the Langfuse public REST API (HTTP Basic auth with a
project public/secret key, configurable host, custom CA) and returns untrusted trace, prompt and
dataset payloads to the model.

**Labels.**
- `[sourced]`: read on the primary page.
- `[sourced — unverified]`: the primary source could not be reached or the string could not be confirmed.

Nothing here was executed against a running server, so nothing is labelled `[verified]`.
Text in quotation marks is verbatim. We confirmed it on raw page markdown, a raw GitHub file, or
text extracted locally from the OWASP PDF. Text without quotation marks is a paraphrase.

## Headline findings

1. **Langfuse project keys cannot be scoped to read-only.** Server-side write gating is therefore the only enforcement layer. `[sourced]`
   - The open Langfuse discussion #7104 says "Currently API keys have full access to all resources". It is still open, with its last comment on 2026-09-24.
   - The community PR that would have added "read-only access permission for project API keys" (langfuse/langfuse#13384) was **closed unmerged** on 2026-04-28.
   - The Langfuse RBAC page says project API keys "are not tied to a user". RBAC roles govern who may *manage* keys, not what a key can do.
   - Consequence: `readOnlyHint` and `destructiveHint` are hints to the client (see §5). The only guarantee that a read-only deployment cannot write is **not registering `execute_write`** plus a GET-only `execute_read`.
2. **Organization-scoped Langfuse keys can create and delete projects and API keys** (`POST/DELETE /api/public/projects…`, `…/apiKeys`, "requires organization-scoped API key"). `[sourced]` Source: Langfuse API reference, via the langfuse-docs MCP search.
   - The operation catalog must exclude every org-key-only operation.
3. **The Anthropic Directory "API ownership" rule** ("Your server must call your own first-party APIs, or APIs you legitimately proxy") may block a third-party Langfuse wrapper from being listed. `[sourced]`
   - **Open question**, not a verdict. It does not affect self-hosted or local use.
4. **Several premises in the brief do not apply as stated.**
   - The MCP SSRF section is about MCP *clients* fetching OAuth discovery URLs.
   - Confused deputy requires an OAuth proxy.
   - Protocol sessions no longer exist in 2026-07-28.
   - See §4 for what does apply.
5. **OWASP IDs were renumbered between 2025 and 2026.** For example, Excessive Agency moved from LLM06:2025 to LLM03:2026. Always cite IDs with the year. `[sourced]`
6. **The LLM Top 10 2026 publication date is inconsistent across sources.** `[sourced]`
   - The PDF title page says "[Publication date to be set]".
   - The genai.owasp.org resource page says August 3, 2026.
   - Press coverage (Help Net Security, 2026-08-06) says August 4.
7. **Correction to run 1.** The "unsettled" `SSL_CERT_DIR` claim is settled: it "can be a colon-separated list, or a semicolon-separated list on Windows". `[sourced]` pkg.go.dev/crypto/x509.
   - The same page adds that since Go 1.27, setting `SSL_CERT_FILE` or `SSL_CERT_DIR` stops Go from using the platform verifiers on macOS and Windows.

## Risk-to-control mapping

| Risk ID | Risk | Concrete control in this server | Source |
|---|---|---|---|
| LLM01:2025 / LLM01:2026 | Prompt Injection (indirect, via tool results) | Wrap every Langfuse payload in a delimited, labelled untrusted-data envelope (for example `structuredContent` plus a text block headed "untrusted Langfuse data"). Tool descriptions never tell the model to follow content. Strip invisible and bidi Unicode (tag chars U+E0000–E007F, zero-width, bidi overrides) from string fields before returning them. | OWASP LLM01 2025 page; LLM01:2026 PDF (invisible-character smuggling, Rule of Two) `[sourced]` |
| LLM01:2026 (Rule of Two) | Untrusted input, sensitive data and state change in one agent | Writes are **off by default**: `execute_write` is registered only when `LANGFUSE_MCP_ALLOW_WRITES=true`. The README states that enabling it creates an [A,B,C] configuration. Shipped (#110): creates (POST) rely on the client's own permission prompt (`destructiveHint:true`); every destructive operation (DELETE, PUT, PATCH) is refused with `confirmation_unavailable` and never sent until server-enforced confirmation ships (ADR-0003 amendment, Planned). | LLM01:2026 PDF `[sourced]` |
| LLM06:2025 / LLM03:2026 / ASI02:2026 | Excessive Agency / Tool Misuse and Exploitation | Hybrid surface with a strictly **GET-only** `execute_read`. `execute_write` accepts only operationIds from the bundled OpenAPI spec whose method is POST/PUT/PATCH/DELETE. An optional operation allowlist or denylist in config (Later: deferred at the M4 grilling, 2026-09-27). The catalog excludes org-key-only and project/API-key management operations. | LLM03:2026 PDF ("Minimize tool permissions", "Complete mediation"); ASI02 PDF ("minimal CRUD operations when exposing APIs") `[sourced]` |
| ASI03:2026 / MCP01:2025 | Identity and Privilege Abuse / Token Mismanagement and Secret Exposure | Keys come from env or file only, never from tool arguments. Redact `Authorization`, `sk-lf-…` and the loopback token from logs, errors and tool output. Tell users to set a Langfuse key **expiry** (the RBAC page says keys support expiration dates). Recommend one dedicated key per MCP deployment. | MCP01 README; Langfuse RBAC page; ASI03 PDF ("short-lived, narrowly scoped tokens") `[sourced]` |
| MCP02:2025 | Privilege Escalation via Scope Creep | Build the tool set once at startup from config. The spec forbids `tools/list` from varying per connection, and env-gated registration at process start complies with that. Server-side gates do not depend on annotations. | MCP02 README; MCP tools spec §Capabilities `[sourced]` |
| MCP03:2025 / ASI04:2026 | Tool Poisoning / Agentic Supply Chain | Tool names and descriptions are static strings compiled into the binary, never derived from API data. Release artifacts are signed and attested (see supply-chain rows). | MCP03 README; ASI04 PDF `[sourced]` |
| MCP04:2025 / LLM03:2025 / LLM04:2026 | Supply chain / dependency tampering | Run `govulncheck` in CI. Pin versions in `go.sum`. Syft SBOM per archive plus a separate image SBOM. Cosign keyless signing of `checksums.txt` and the image (by digest). `actions/attest` provenance for binaries and image. Base image pinned by digest and updated by Dependabot. | go.dev vuln; GoReleaser docs; Sigstore; Docker docs `[sourced]` |
| MCP05:2025 / ASI05:2026 | Command Injection / Unexpected Code Execution; path injection into the REST call | No shell and no `exec`. `execute_*` takes an **operationId** (or a templated path that matches the spec exactly) plus parameters. Path params are percent-encoded. Reject absolute URLs, `//`, `..`, and any scheme or host in model input. The base URL comes only from operator config. | MCP05 README; MCP tools spec "Servers MUST: Validate all tool inputs" `[sourced]` |
| MCP06:2025 / ASI01:2026 | Prompt Injection via Contextual Payloads / Agent Goal Hijack | Same envelope as LLM01. Keep tool descriptions to function only (Anthropic rules). Never echo payload text into error messages unescaped. | MCP06 README; ASI01 PDF ("Treat all natural-language inputs … as untrusted") `[sourced]` |
| MCP07:2025 | Insufficient Authentication and Authorization (HTTP mode) | Streamable HTTP binds to 127.0.0.1 by default. It requires a bearer token (random, ≥256 bits, constant-time compare). It **MUST** validate `Origin` and return 403 when it is invalid. It rejects `Host` values other than loopback (defence in depth against DNS rebinding). | MCP Streamable HTTP spec §Security; MCP security BP §Local server `[sourced]` |
| Token passthrough (MCP spec) | Forwarding client tokens downstream | The loopback token is checked and dropped, and is **never** forwarded to Langfuse. Langfuse Basic credentials come from the server environment. The stdio mode takes credentials from env, per the spec. | MCP auth spec; MCP security BP §Token Passthrough `[sourced]` |
| SSRF (adapted) | Model-steered requests to unintended hosts | The host is fixed at startup (`LANGFUSE_HOST`, https required except loopback). A custom `CheckRedirect` refuses any redirect that changes scheme, host or port. Go forwards `Authorization` to same-domain and **subdomain** redirects, and a scheme downgrade on the same host would leak Basic credentials. | Go net/http `Client` docs; MCP security BP §SSRF (client-side, adapted) `[sourced]` |
| State handle hijacking (MCP spec) | Guessable handles | Not applicable: stateless, and no handles are minted. If pagination cursors are passed through, they are Langfuse's own cursors, and the same key authorizes them. | MCP security BP §State Handle Hijacking `[sourced]` |
| LLM10:2025 / LLM06:2026 / ASI02 (budgeting) | Unbounded Consumption | Server-side limits: a per-process request rate and concurrency cap on Langfuse calls, an HTTP client timeout, a max response bytes read, and a max tool-result bytes returned (truncate with a marker plus a pagination hint). Force a `limit` default and cap on list operations. | MCP tools spec "Rate limit tool invocations"; LLM06:2026 PDF ("Rate Limiting & Input Size Validation"); Anthropic ("Keep responses reasonably sized") `[sourced]` |
| LLM05:2025 / LLM10:2026 | Improper Output Handling | Return JSON in `structuredContent` with an `outputSchema` where feasible. Never render HTML or Markdown from payloads. Sanitize control characters. | MCP tools spec ("Sanitize tool outputs"); LLM10:2026 `[sourced]` |
| LLM02:2025 / LLM02:2026 / MCP10:2025 | Sensitive Information Disclosure / Context Over-Sharing | Trace payloads may hold end-user PII. Offer field projection (return selected fields, not full I/O blobs, by default in dedicated trace tools). Keep no cross-call caching of payloads (stateless). Log metadata only, never payloads. | MCP10 README; LLM02:2026 PDF `[sourced]` |
| MCP08:2025 | Lack of Audit and Telemetry | Write a structured audit log to **stderr** (stdout is the stdio JSON-RPC channel) for each tool call: tool, operationId, method, status, latency, bytes. Redact secrets. Write calls (`execute_write`) are logged at `Warn` with a `confirmation` field (`not_required`, or `unavailable` for a refused destructive call), never the body. | MCP08 README; MCP tools spec (clients SHOULD log) `[sourced]` |
| MCP09:2025 | Shadow MCP Servers | Out of scope for the server. Mitigated by signed releases and a documented verification command. | MCP09 README `[sourced]` |
| ASI06–ASI10:2026 | Memory Poisoning, Inter-Agent Comms, Cascading Failures, Human-Agent Trust, Rogue Agents | Mostly the client or agent's responsibility. The server contributes statelessness (no memory), `isError` results with actionable text (limits cascades), and accurate descriptions and annotations (human trust). | ASI PDF `[sourced]` |
| Anthropic review | Directory rejection | `execute_read` is strictly GET-only. Split writes by create/update/delete, or document the deviation. Every tool has a `title` and a hint. Names are ≤64 characters. Freeform-path tools name the Langfuse API reference URL in the description. No behavioural instructions in descriptions. | claude.com review criteria `[sourced]` |
| TLS (corporate CA) | MITM or failing verification | Load `SystemCertPool()`, append the user CA file or dir via `AppendCertsFromPEM` in code, and set `MinVersion` TLS 1.2. Never expose an `insecureSkipVerify` flag without a loud warning. Setting `SSL_CERT_*` env vars disables the macOS and Windows platform verifiers in Go ≥1.27. | pkg.go.dev/crypto/x509 `[sourced]` |

## 1. OWASP Top 10 for LLM Applications 2025 and 2026

**2025 list** (genai.owasp.org/llm-top-10/). `[sourced]`
LLM01:2025 Prompt Injection · LLM02:2025 Sensitive Information Disclosure · LLM03:2025 Supply Chain ·
LLM04:2025 Data and Model Poisoning · LLM05:2025 Improper Output Handling · LLM06:2025 Excessive
Agency · LLM07:2025 System Prompt Leakage · LLM08:2025 Vector and Embedding Weaknesses ·
LLM09:2025 Misinformation · LLM10:2025 Unbounded Consumption.

LLM01:2025 mitigations (genai.owasp.org/llmrisk/llm01-prompt-injection/). `[sourced]`
- "Separate and clearly denote untrusted content to limit its influence on user prompts."
- "Provide the application with its own API tokens for extensible functionality, and handle these
  functions in code rather than providing them to the model."
- "Implement human-in-the-loop controls for privileged operations to prevent unauthorized actions."

**2026 list: it exists.** Sources: the resource page genai.owasp.org/resource/owasp-genai-llm-top-10-2026/, and the list taken from the PDF's table of contents (download id 56857). `[sourced]`
LLM01:2026 Prompt Injection · LLM02:2026 Sensitive Information Disclosure · LLM03:2026 Excessive
Agency · LLM04:2026 Supply Chain · LLM05:2026 Data and Model Poisoning · LLM06:2026 Unbounded
Consumption · LLM07:2026 Misinformation · LLM08:2026 Hidden Context Exposure (renamed from System
Prompt Leakage) · LLM09:2026 Vector and Embedding Weaknesses · LLM10:2026 Improper Output Handling.

**Excerpts from the 2026 PDF.** `[sourced]`
- On the Rule of Two:
  - "Treat simultaneous access to (A) untrusted input, (B) sensitive data, and (C) state change or external communication as high-risk: any [A,B,C] agent needs per-action human approval".
  - "Invisible-character smuggling can make the displayed action differ from the executed one".
- On LLM03:2026 complete mediation:
  - "Implement authorization in logic rather than relying on an LLM to decide if an action is allowed or not."
  - "Tools should define a strict schema for any input parameters, and validate contents prior to use."
- On LLM03:2026 rate limiting:
  - "Establish thresholds around the invocation of tools and implement circuit breakers that halt, rate-limit or escalate for human review if those thresholds are exceeded."

## 2. OWASP Top 10 for Agentic Applications 2026

The page genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/ gives the publication date as December 9, 2025. The PDF cover reads "Version 2026 December 2025". The resource page itself does **not** list the items; the list comes from the PDF's table of contents. `[sourced]`
ASI01 Agent Goal Hijack · ASI02 Tool Misuse and Exploitation · ASI03 Identity and Privilege Abuse ·
ASI04 Agentic Supply Chain Vulnerabilities · ASI05 Unexpected Code Execution (RCE) · ASI06 Memory &
Context Poisoning · ASI07 Insecure Inter-Agent Communication · ASI08 Cascading Failures · ASI09
Human-Agent Trust Exploitation · ASI10 Rogue Agents.

**Excerpts.** `[sourced]`
- ASI02: "Define per-tool least-privilege profiles (scopes, maximum rate, and egress allowlists) … e.g., read-only queries for databases, no send/delete rights for email summarizers, and minimal CRUD operations when exposing APIs."
- ASI02: "human confirmation for high-impact or destructive actions (delete, transfer, publish)".
- ASI02: "Apply usage ceilings (cost, rate, or token budgets)".
- ASI04: "Sign and attest manifests, prompts, and tool definitions"; "Allowlist and pin"; "require reproducible builds."
- ASI04 cites the malicious npm `postmark-mcp` server, which "secretly BCC’d emails to the attacker".

## 3. OWASP MCP Top 10 (github.com/OWASP/www-project-mcp-top-10)

The version is "2025". The project describes itself as a living document. `[sourced]`
MCP01:2025 Token Mismanagement & Secret Exposure · MCP02:2025 Privilege Escalation via Scope Creep ·
MCP03:2025 Tool Poisoning · MCP04:2025 Software Supply Chain Attacks & Dependency Tampering ·
MCP05:2025 Command Injection & Execution · MCP06:2025 Prompt Injection via Contextual Payloads ·
MCP07:2025 Insufficient Authentication & Authorization · MCP08:2025 Lack of Audit and Telemetry ·
MCP09:2025 Shadow MCP Servers · MCP10:2025 Context Injection & Over-Sharing.

**Excerpts.** `[sourced]`
- MCP01: "Hard-coded credentials, long-lived tokens, and secrets stored in model memory or protocol logs can expose sensitive environments to unauthorized access."
- MCP06: "the 'interpreter' is the model and the 'payload' is text".
- MCP08: "Maintain detailed logs of tool invocations, context changes, and user-agent interactions with immutable audit trails."

## 4. MCP spec 2026-07-28: security best practices and transports

Source: modelcontextprotocol.io/docs/2026-07-28/tutorials/security/security_best_practices. `[sourced]`

**Token passthrough.**
- "MCP servers **MUST NOT** accept any tokens that were not explicitly issued for the MCP server."
- The authorization spec adds: "MCP servers **MUST NOT** accept or transit any other tokens."
- It also says: "Implementations using an STDIO transport **SHOULD NOT** follow this specification, and instead retrieve credentials from the environment."
- Authorization is "**OPTIONAL**".
- **Applies:** the loopback HTTP token is a local shared secret, not an OAuth token, and it must never be forwarded.

**Confused deputy.**
- The attack needs an "MCP proxy server [that] uses a **static client ID** with a third-party authorization server" and "allows MCP clients to **dynamically register**".
- **Not applicable:** this server does no OAuth. It becomes applicable only if OAuth to Langfuse is ever added.

**SSRF.**
- Scope in the spec: "During OAuth metadata discovery, MCP clients fetch URLs … that could be controlled by a malicious MCP server".
- **The premise is misapplied to this server.** `LANGFUSE_HOST` is operator configuration, not attacker input.
- The transferable guidance is still useful:
  - "Do not blindly follow redirects to internal resources".
  - "Consider disabling automatic redirect following and validating each hop".
  - "Avoid implementing IP validation manually".
- The real surface here is the model-supplied path and parameters (see the MCP05 row).
- Go net/http `Client`: sensitive headers "will be ignored when following a redirect to a domain that is not a subdomain match or exact match of the initial domain". So subdomain and same-host scheme-downgrade redirects **do** carry `Authorization`. A custom `CheckRedirect` is warranted. `[sourced]`

**Sessions and handles.**
- "MCP is stateless and has no protocol-level sessions". The former "Session Hijacking" section is now "State Handle Hijacking".
- "MCP servers **MUST NOT** treat possession of a state handle as authentication."
- Streamable HTTP 2026-07-28 lists the "Removal of protocol-level sessions" and says:
  - "An `Mcp-Session-Id` header on a request: ignore it, and do not mint or echo session IDs".
  - "HTTP GET or DELETE to the MCP endpoint: respond with `405 Method Not Allowed`".

**Local servers.** "MCP servers intending for their servers to be run locally **SHOULD** implement measures to prevent unauthorized usage from malicious processes":
- "Use the `stdio` transport to limit access to just the MCP client".
- "Require an authorization token".

**Streamable HTTP §Security & Endpoint.**
- "Servers **MUST** validate the `Origin` header on all incoming connections to prevent DNS rebinding attacks."
- "If the `Origin` header is present and invalid, servers **MUST** respond with HTTP 403 Forbidden."
- "When running locally, servers **SHOULD** bind only to localhost (127.0.0.1) rather than all network interfaces (0.0.0.0)."

**Tools spec §Security Considerations.** "Servers **MUST**: Validate all tool inputs · Implement proper access controls · Rate limit tool invocations · Sanitize tool outputs".

**tools/list stability.** It "**MUST NOT** vary per-connection or as a side effect of other requests on the connection". Deciding at startup from env complies.

## 5. MCP tool annotations

Sources:
- `schema/2026-07-28/schema.ts` in the spec repository.
- The tools spec page.
- blog.modelcontextprotocol.io/posts/2026-03-16-tool-annotations/.

All `[sourced]`.

**Defaults.**

| Hint | Default |
|---|---|
| `readOnlyHint` | false |
| `destructiveHint` | true (meaningful only when `readOnlyHint == false`) |
| `idempotentHint` | false |
| `openWorldHint` | true |

**Trust.**
- Schema: "all properties in `ToolAnnotations` are **hints**" and "Clients should never make tool use decisions based on `ToolAnnotations` received from untrusted servers."
- Tools spec: "clients **MUST** consider tool annotations to be untrusted unless they come from trusted servers."
- Blog: annotations are no defence against prompt injection. "nothing in them tells the model to ignore malicious instructions it reads from a calendar event"; "If you need a guarantee that a tool can't exfiltrate data, that's a job for network controls or sandboxing, not a boolean hint."

**Server settings.** Set every hint explicitly; never rely on the defaults.

| Tool | Annotations |
|---|---|
| `search_operations` | `readOnlyHint:true`, `openWorldHint:false` (local catalog) |
| `execute_read` and dedicated read tools | `readOnlyHint:true`, `openWorldHint:true` (the host is external; the data is untrusted) |
| `execute_write` | `readOnlyHint:false`, `destructiveHint:true`, `idempotentHint:false`, `openWorldHint:true` |

**Naming.** MCP says tool names "SHOULD be between 1 and 128 characters" and uses `[A-Za-z0-9_.-]`. Anthropic's limit is stricter (§6).

## 6. Anthropic connector review criteria (claude.com/docs/connectors/building/review-criteria)

All `[sourced]`.

**Read/write split.** "A single tool that accepts both safe HTTP methods (GET, HEAD, OPTIONS) and unsafe methods (POST, PUT, PATCH, DELETE) is rejected." Also: "Ideally, split write operations further by action type (create, update, delete)."
- A single `execute_write` meets the hard rule but not the "ideally" rule. Record this as a known deviation, or split it into `execute_create`, `execute_update` and `execute_delete`.

**Custom query tools.** Such a tool's "description must include a link to or explicit name of the target API". Point to `https://api.reference.langfuse.com`.

**Annotations.** "Every tool must include a `title` and the applicable hint—`readOnlyHint: true` for read-only tools, `destructiveHint: true` for tools that modify or delete data." "read-only tools can run without per-call confirmation; destructive tools always prompt."

**Names.** "Tool names must be 64 characters or fewer."

**Prompt injection.** Descriptions are rejected if they "Direct Claude to pull behavioral instructions from external sources" or "Contain hidden, obfuscated, or encoded instructions". "Describe what the tool does. Do not tell Claude how to behave."

**Quality.**
- "Generic errors ("Internal Server Error", "Bad Request" with no detail) fail review."
- "Keep responses reasonably sized for the task."

**API ownership.** "Your server must call your own first-party APIs, or APIs you legitimately proxy. The MCP server domain should match your service."
- **Open question:** does this block a community Langfuse wrapper from the Directory?

## 7. Supply chain (Go binary and Docker image)

**govulncheck** (go.dev/doc/security/vuln/). `[sourced]`
- It surfaces vulnerabilities "based on which functions in your code are transitively calling vulnerable functions" and is described as "a low-noise, reliable way to find known vulnerabilities".
- CI: `golang/govulncheck-action@v1` (the README says it "is currently experimental").

**SBOM** (GoReleaser `sbom.md`). `[sourced]`
- The default command is Syft ("Default: 'syft'"), with one SBOM per archive.
- Limitation: "Container images generated by GoReleaser are not available to be cataloged by the SBOM tool."
- A separate image SBOM step is needed. Options are `syft <image@digest>` or `docker buildx --sbom`. That tooling choice is `[sourced — unverified]`, because its docs were not fetched.
- CycloneDX output is available via `args: [..., "--output", "cyclonedx-json=$document"]`, per the GoReleaser example.

**Cosign keyless** (Sigstore overview). `[sourced]`
- "Keyless signing associates identities, rather than keys, with an artifact signature."
- "Fulcio issues short-lived certificates binding an ephemeral key to an OpenID Connect identity."

**Blobs with GoReleaser** (`sign.md`). `[sourced]`
- The `signs:` config uses `cosign sign-blob --bundle=${signature} ${artifact} --yes` on `checksum`.
- Verify with `cosign verify-blob --certificate-identity '<workflow>@refs/tags/vX' --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' --bundle checksums.txt.sigstore.json checksums.txt`.

**Images with GoReleaser** (`docker_sign.md`). `[sourced]`
- The **default is key-based**: "Default: ["sign", "--key=cosign.key", "${artifact}@${digest}", "--yes"]". The common example uses `COSIGN_PWD` and `cosign verify --key cosign.pub`.
- Keyless requires overriding `args` to drop `--key`. The doc notes "cosign only issues a certificate when it signs keylessly".
- Signing by digest: "Using the digest helps making sure you're signing the right image and avoid concurrency issues."

**Provenance** (GoReleaser `attestations.md`). `[sourced]`
- Permissions: `id-token: write`, `attestations: write`.
- Steps: `actions/attest@v4` with `subject-checksums: ./dist/checksums.txt` and `./dist/digests.txt`.
- Verify with `gh attestation verify --owner <user-or-org> <filename|image>`.
- **The SLSA level is not claimed.** The page names no level, and L3 claims usually go through `slsa-github-generator`, which was not researched here, so any level is `[sourced — unverified]`.

**Base image** (distroless README). `[sourced]`
- `gcr.io/distroless/static` contains "ca-certificates", "A /etc/passwd entry for a root user" and "tzdata".
- `static-debian13` is "around 2 MiB" and has `nonroot` tags.
- Distroless images are cosign-signed: `cosign verify $IMAGE_NAME --certificate-oidc-issuer https://accounts.google.com --certificate-identity keyless@distroless.iam.gserviceaccount.com`.
- Use `static-debian13:nonroot` and build with `CGO_ENABLED=0`.
- For a corporate CA, mount the PEM and pass it through the server's CA-file option. The image's bundled CAs stay as the base.

**Digest pinning** (Docker build best practices). `[sourced]`
- "By pinning your images to a digest, you're guaranteed to …" always use the same image version, even if the tag is replaced.
- The page recommends Dependabot (`package-ecosystem: "docker"`) to raise PRs updating tags and digests.
- Pin `FROM gcr.io/distroless/static-debian13:nonroot@sha256:…` and the Go builder image the same way.

**TLS trust** (pkg.go.dev/crypto/x509). `[sourced]`
- "The environment variables SSL_CERT_FILE and SSL_CERT_DIR can be used to override the system default locations".
- "setting SSL_CERT_FILE or SSL_CERT_DIR will prevent those APIs from being used, unless the x509sslcertoverrideplatform=0 GODEBUG setting is used. (This changed in Go 1.27.)"
- Therefore the CA option should **append** to `SystemCertPool()` in code, not tell users to set env vars.

## Not found or not applicable

- **The agentic resource page does not list the items.** The list exists only in the PDF. `[sourced]`
- **Read-only or scoped Langfuse project API keys:** none exist (§Headline 1). `[sourced]`
- **An SLSA level for `actions/attest`:** not stated on the GoReleaser page. `[sourced — unverified]`
- **Confused deputy and OAuth consent controls:** not applicable (no OAuth).
- **Protocol session-ID controls:** not applicable (sessions removed in 2026-07-28).
