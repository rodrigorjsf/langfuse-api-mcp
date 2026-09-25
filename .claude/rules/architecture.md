# Architecture decisions

- **Create, recreate or review any architecture decision with the `archify` skills**: `archify` to draw/redraw the architecture, workflow, sequence or data-flow diagram from repo evidence; `archify-review` to review an architecture change. Commit the diagram next to the ADR it illustrates (`docs/architecture/`).
- Hard-to-reverse, surprising, trade-off decisions get an ADR in `docs/adr/` (`/domain-modeling` ADR format). Contradicting an accepted ADR requires a superseding ADR, never a silent change.
- Before any architecture/spec decision, run an adversarial pass (advisor, or the `rubber-duck` agent if advisor is off).
- Accepted decisions so far: [ADR index](../../docs/adr/). Read the ADRs touching an area before changing it.

## Target module shape (proposal — confirm with `archify` at the tracer-bullet slice)

Deep modules with small interfaces; each boundary below is a **seam** tested through its interface:

| Module | Owns | Seam for tests |
|---|---|---|
| `cmd/langfuse-mcp` | wiring only: flags → config → server | none (thin) |
| `internal/config` | env/flag parsing + validation (keys, host, CA, writes flag) | pure `Load(env, args)` |
| `internal/langfuse` | HTTP client: auth, TLS pool, proxy, timeouts, retries/429, response caps | `http.RoundTripper` / `httptest.Server` |
| `internal/catalog` | operations from `docs/langfuse-openapi.json`, scope exclusions (ADR-0004), search | pure data + functions |
| `internal/server` | MCP tool registration, annotations, read/write split (ADR-0002/0003) | in-memory MCP client from go-sdk |
