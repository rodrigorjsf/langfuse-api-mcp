# Stack & MCP SDK

> Decision: [ADR-0001](../adr/0001-go-with-official-go-sdk.md). Labels: `[verified 3-0]` = passed the deep-research 3-vote adversarial check against the primary source; `[sourced]` = read on a primary source, not independently re-checked; `[sourced — unverified]` = secondary/blog or extraction-only.

## MCP spec state (2026-09-25)

| Fact | Label | Source |
|---|---|---|
| Latest spec revision is **2026-07-28**; it is stateless: no `initialize` handshake, no `Mcp-Session-Id`; protocol version + client capabilities travel in `_meta` of every request; servers MUST implement `server/discover` | `[verified 3-0]` | https://blog.modelcontextprotocol.io/posts/2026-07-28/, https://modelcontextprotocol.io/specification/2026-07-28/changelog |
| Streamable HTTP requests carry `Mcp-Method` / `Mcp-Name` headers; Roots, Sampling, Logging and legacy HTTP+SSE deprecated (≥12-month off-ramp); Multi Round-Trip Requests replace server-initiated requests (stateless user confirmation mid-call) | `[sourced — unverified]` | same blog post |
| Stateful servers mint explicit handles; holding a handle MUST NOT be treated as authentication | `[sourced — unverified]` | https://modelcontextprotocol.io/docs/2026-07-28/tutorials/security/security_best_practices |

## SDK comparison

| SDK | Tier | 2026-07-28 | Distribution | Idle RSS / startup* | Label |
|---|---|---|---|---|---|
| **Go** `modelcontextprotocol/go-sdk` | 1 | yes (v1.7.0+) | static binary, cross-compile | ~21 MB / 33 ms (mcp-go) | `[verified 3-0]` tier+spec |
| TypeScript `@modelcontextprotocol/sdk` | 1 | yes | Node runtime | n/a | `[verified 3-0]` |
| Python (`mcp` / FastMCP 3) | 1 | yes | Python runtime | ~97 MB | `[verified 3-0]` tier |
| C# | 1 | yes | .NET runtime / AOT | n/a | `[verified 3-0]` |
| Rust `rmcp` | 1 per a blog, **beta** for 2026-07-28 per the official post | beta | static binary | ~7 MB / 38 ms | conflicting |
| Java | 2 | not mentioned | JVM | n/a | `[sourced — unverified]` |

\* Community benchmark https://github.com/desty2k/mcp-benchmark — Go number is `mark3labs/mcp-go` 0.47.0, **not** the official SDK. All frameworks < 3 ms p50 proxy latency: for an API wrapper the upstream network dominates. `[sourced — unverified]`

Tier rules `[verified 3-0]` (https://modelcontextprotocol.io/community/sdk-tiers): Tier 1 = 100 % conformance, all non-experimental features, new features before each spec release, triage in 2 business days, P0 (incl. CVSS ≥ 7) fixed in 7 days. Tiers can be relegated after 4 weeks of failing conformance — re-check before upgrades.

## go-sdk specifics `[sourced]` (https://github.com/modelcontextprotocol/go-sdk)

- Typed tool handlers; input JSON schema generated from Go struct tags.
- Separate packages: core `mcp`, `jsonrpc`, `oauthex`/auth.
- License: new contributions Apache-2.0, existing code MIT.

## What Anthropic's `mcp-server-dev` skills say (read locally, plugin `mcp-server-dev`, 2026-09-25)

| Guidance | Where | Our position |
|---|---|---|
| Default SDK: TypeScript; alt FastMCP; "if the user already has a stack in mind, go with it — identical wire protocol" | `build-mcp-server/SKILL.md:152-155` | Go chosen (ADR-0001) |
| 30+ actions → search + execute; promote top 3–5 to dedicated tools | `SKILL.md:128-140`, `references/tool-design.md` | Hybrid with split execute (ADR-0002) |
| SaaS API → remote HTTP; local stdio "not recommended for distribution"; MCPB for local | `SKILL.md:25-33,101` | Local-first + MCPB (ADR-0005) — deliberate deviation |
| MCPB manifest `server.type` accepts `"binary"` | `build-mcpb/references/manifest-schema.md:52` | Go binary ships as MCPB |
| Directory review: every tool needs `readOnlyHint`, `destructiveHint`, `title`; names ≤ 64 chars; read/write in separate tools; descriptions must not instruct the model; free-form API tools must link the API docs | `references/tool-design.md` | Enforced by `.claude/rules/mcp-tool-design.md` |
