---
status: accepted
---
# Generic catalog-driven executor, typed functions only for workflow tools

`execute_read`/`execute_write` run any in-scope operation through one generic code path: the catalog (embedded OpenAPI spec) supplies method, path template and parameter schema; the executor validates params, builds the request and hands it to the Langfuse HTTP client. Every operation is tested through that path against `httptest`, with no mocks. Workflow tools get small typed functions only for the few operations they call.

## Considered Options

- **Generated client (`oapi-codegen`)** — fully typed, but thousands of generated lines, a new dependency, and the executor would still need an `operationId → function` dispatch table, doubling the surface.
- **Hand-written SDK-style client** (the `/tdd` mocking guidance) — infeasible for ~100 operations; that guidance targets mock ergonomics, and this design has no mocks.

## Consequences

- Parameters are not compile-time typed; the catalog's schema validation is the safety net and must be exhaustively tested (one table-driven case per parameter style: path, query, repeated query, JSON body).
- A spec update changes the executable surface without code changes — guarded by the ADR-0004 catalog test.
