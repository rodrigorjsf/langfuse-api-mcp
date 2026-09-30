# Architecture decisions

- **Create, recreate or review any architecture decision with the `archify` skills**: `archify` to draw/redraw the architecture, workflow, sequence or data-flow diagram from repo evidence; `archify-review` to review an architecture change. Commit the diagram next to the ADR it illustrates (`docs/architecture/`).
- Hard-to-reverse, surprising, trade-off decisions get an ADR in `docs/adr/` (`/domain-modeling` ADR format). Contradicting an accepted ADR requires a superseding ADR, never a silent change.
- Before any architecture/spec decision, run an adversarial pass (advisor, or the `rubber-duck` agent if advisor is off).
- Current diagram: `docs/architecture/target-architecture.json` (source) → `.html` (rendered with `archify deliver … --quality showcase`). Edit the JSON, re-validate, re-deliver; never hand-edit the HTML.
- Accepted decisions so far: [ADR index](../../docs/adr/). Read the ADRs touching an area before changing it.

## Module layout

Binding tree, placement table and dependency direction: `project-structure.md` (ADR-0009). Seams under test: `config.Load(env, file)` (pure; `config.ReadFile` does the I/O), `trust` pool builder (temp CA files), `langfuse` client (`httptest.Server`), `catalog` (pure data), `server` (go-sdk in-memory client), `transport` (loopback `httptest`).

## Executor

Catalog operations run through one generic executor; typed functions exist only for workflow tools (ADR-0010).

## archify gotchas

- archify showcase: landscape viewBox ~1100x530; wider fails readability, taller overflows viewport.
- Vertical archify edge labels overlap nodes by default; set `labelAt` at segment midpoint.
