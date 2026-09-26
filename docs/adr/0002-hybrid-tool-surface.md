---
status: accepted
---
# Hybrid tool surface: search + split execute + dedicated workflow tools

The Langfuse public API has ~100 in-scope operations; one tool per operation would spend thousands of context tokens every turn, and a single `execute` tool cannot carry truthful `readOnlyHint`/`destructiveHint` annotations and is the open-ended tool OWASP LLM06 (Excessive Agency) says to narrow. So the server exposes: `search_operations` (catalog lookup), `execute_read` (GET operations only, `readOnlyHint: true`), `execute_write` (mutating operations only, `destructiveHint: true`, see ADR-0003), and a small set of dedicated read tools for the highest-traffic workflows (trace investigation, error/latency/cost triage).

## Consequences

- Every operation stays reachable; the catalog is generated from `internal/catalog/spec/langfuse-openapi.json`, so "implements all endpoints" is a testable property (catalog ⊇ in-scope operations, see ADR-0004).
- Satisfies the Anthropic connector review criterion that read and write live in separate tools.
- `execute_*` accepts operation IDs from the catalog only — never a free-form URL or path.

## Amendment: operation discovery in two tools (2026-09-26, M3 grilling)

`search_operations` is split into two read-only, closed-world tools, so an agent — including a small model that does not know the Langfuse API — can find and call any operation using only this server:

- `search_operations` returns the **operation index**: one line per catalog operation (`operationId` — first line of its description), grouped by OpenAPI tag, v4 family before legacy (ADR-0012 §5). An optional `query` keeps only operations where every whitespace-separated term appears, case-insensitively, in the `operationId`, the tag or that first line. No match is a normal result that names the tags and says to call again without `query`; the query is never echoed. The result carries text plus `structuredContent` under an `outputSchema`.
- `describe_operation` takes one `operationId` and returns its **operation description**: every parameter with location, type, required flag, enum and bounds, from the catalog's own schema.
- Write operations are left out of both unless write mode is on; then each index line says which tool runs it. Results come from the same catalog `execute_read` uses; filtering by the deployment profile lands with the version-aware catalog (ADR-0012), and until then a listed operation the deployment lacks answers `operation_unavailable`.
- `execute_read` hints point here: `operation_not_found` names `search_operations` (#36); `invalid_parameter` lists the operation's valid parameter names and names `describe_operation`.
- The `query` is model input: bounded (128 runes), refused with `invalid_argument` when it holds a control or invisible character, never echoed. Descriptions are third-party text fixed at build time: sanitized (invisible, bidi and control characters stripped) and reviewed in the spec-regeneration diff, not wrapped in the untrusted-data envelope; a test asserts the index holds no forbidden character.

Rejected: **semantic search over embeddings** — an external embedding API breaks local-first (ADR-0005), a bundled model is a heavy dependency, it adds vector/embedding risk (LLM08:2025 / LLM09:2026), and the in-scope read index is ~1.2k tokens (58 GET operations; ~2.7k for all 117), small enough for the calling model to match intent itself. **A skill as the primary discovery path** — the server cannot make a client load a skill, and a static skill cannot know which operations the connected deployment serves (ADR-0012); skills stay the workflow layer of M6 and may point to `search_operations`. **One tool with a search mode and a detail mode** — mutually exclusive arguments are what small models mix up; the second tool costs ~150 tokens of definition.

Evidence that small models benefit comes from a scripted eval in `scripts/` (about 10 natural-language intents; Haiku 4.5 must reach the right `operationId` and parameters through these tools alone), run by hand at the end of M3 and again in M6, its result recorded in the PR — not a CI gate.

## Amendment: M3 ships one workflow tool, `get_trace_tree` (2026-09-26, M3 grilling)

M3 ships a single workflow tool, the trace tree. Error, latency and cost triage are deferred: they are mostly a choice of filters and a reading of Metrics v2, which an M6 skill can teach over `execute_read`, and they come back only if the small-model eval shows agents fail at them. The payload-query guard (#42) moves with them.

`get_trace_tree` is registered only when the v4 read family answers (ADR-0012 §6):

- Input: `traceId` (1–128 runes; control or invisible characters refused with `invalid_argument`; sent as a query parameter, never echoed unbounded) and an optional `include`, a closed subset of `["io","metadata"]`. Unknown arguments are refused.
- It calls `observations_getMany` with `traceId`, `limit=1000` and field groups `core,basic,time,usage,model,metrics`, plus `io`/`metadata` only when asked; it follows `meta.cursor` for at most 5 pages. Past that, the result is marked truncated and returns the cursor, with a hint to continue through `execute_read`.
- Output: a flat list in depth-first pre-order with `depth` and `parentObservationId`, siblings by `startTime` ascending, so the existing row truncation of the untrusted-data envelope applies unchanged at the 100 KiB cap. An observation whose parent is absent becomes an extra root with `orphan: true`; several roots are ordered by `startTime`. A trace with no observations is an empty result with a hint, not `langfuse_not_found`, because Observations v2 cannot tell "missing" from "not yet ingested".
- No scores: the agent reads them with `execute_read` `scoresV3_getManyV3`.
- The payload is wrapped and sanitized like any `execute_read` result; the audit line names `get_trace_tree` and counts the Langfuse requests it made.
