# Architecture decisions

- **Create, recreate or review any architecture decision with the `archify` skills**: `archify` to draw/redraw the architecture, workflow, sequence or data-flow diagram from repo evidence; `archify-review` to review an architecture change. Commit the diagram next to the ADR it illustrates (`docs/architecture/`).
- Hard-to-reverse, surprising, trade-off decisions get an ADR in `docs/adr/` (`/domain-modeling` ADR format). Contradicting an accepted ADR requires a superseding ADR, never a silent change.
- Before any architecture/spec decision, run an adversarial pass (advisor, or the `rubber-duck` agent if advisor is off).
- Current diagram: `docs/architecture/target-architecture.json` (source) → `.html` (rendered with `archify deliver … --quality showcase`). Edit the JSON, re-validate, re-deliver; never hand-edit the HTML.
- Accepted decisions so far: [ADR index](../../docs/adr/). Read the ADRs touching an area before changing it.

## Module layout

Binding tree, placement table and dependency direction: `project-structure.md` (ADR-0009). Seams under test: `config.Load(env, args)` (pure), `trust` pool builder (temp CA files), `langfuse` client (`httptest.Server`), `catalog` (pure data), `server` (go-sdk in-memory client), `transport` (loopback `httptest`).

## Known tension to resolve at M1

`/tdd` (mocking.md) prefers an SDK-style client — one function per external operation, no generic `Do(endpoint)`. The catalog-driven `execute_read`/`execute_write` (ADR-0002) is inherently generic. Proposed resolution: generic executor for the catalog (tested against `httptest`, no mocks), typed per-operation functions only for the dedicated workflow tools. Decide with `archify` + `/codebase-design` before the first `internal/langfuse` code.
