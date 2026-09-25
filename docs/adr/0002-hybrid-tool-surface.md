---
status: accepted
---
# Hybrid tool surface: search + split execute + dedicated workflow tools

The Langfuse public API has ~100 in-scope operations; one tool per operation would spend thousands of context tokens every turn, and a single `execute` tool cannot carry truthful `readOnlyHint`/`destructiveHint` annotations and is the open-ended tool OWASP LLM06 (Excessive Agency) says to narrow. So the server exposes: `search_operations` (catalog lookup), `execute_read` (GET operations only, `readOnlyHint: true`), `execute_write` (mutating operations only, `destructiveHint: true`, see ADR-0003), and a small set of dedicated read tools for the highest-traffic workflows (trace investigation, error/latency/cost triage).

## Consequences

- Every operation stays reachable; the catalog is generated from `internal/catalog/spec/langfuse-openapi.json`, so "implements all endpoints" is a testable property (catalog ⊇ in-scope operations, see ADR-0004).
- Satisfies the Anthropic connector review criterion that read and write live in separate tools.
- `execute_*` accepts operation IDs from the catalog only — never a free-form URL or path.
