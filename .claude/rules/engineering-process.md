# Engineering process (Matt Pocock skills)

Full glossary (~100 terms, each with source path): `docs/research/engineering-process.md`. Use its exact vocabulary: **module, interface, implementation, depth, seam, adapter, port** — not "component/service/API/boundary".

## Flow

idea → grill → (prototype) → spec → tickets → implement (red → green per slice) → code review → commit. Entry points for existing work: triage, diagnosing-bugs, wayfinder.

| Step | Skill | Who starts it |
|---|---|---|
| stress-test an idea against the domain | `/grill-with-docs` (writes `CONTEXT.md`/ADRs inline) or `/grilling` | user / agent |
| answer a design question cheaply | `/prototype` — kept on a `prototype/<name>` branch as a primary source, not deleted | agent |
| write the spec | `/to-spec` | **ask the user to run it** |
| split into vertical-slice tickets with blocking edges | `/to-tickets` | **ask the user** |
| implement a ticket | `/implement` (drives `/tdd` at pre-agreed seams, then `/code-review`) | **ask the user** |
| test-first | `/tdd` | agent |
| review (Standards + Spec axes) | `/code-review` | agent |
| hard bug / regression | `/diagnosing-bugs` | agent |
| module/seam design | `/codebase-design` (Design It Twice for hard interfaces) | agent |
| architecture survey of hot spots | `/improve-codebase-architecture` | **ask the user** |
| issue intake | `/triage` (labels in `docs/agents/triage-labels.md`) | **ask the user** |

## Rules

- **Vertical slices.** A ticket cuts through every layer (config → client → catalog → tool) and fits one context window. Tracer bullet first: the thinnest end-to-end path before breadth.
- **Red before green.** One seam, one test, one minimal implementation per cycle; the test fails first. No horizontal slicing (all tests, then all code). Refactoring is not part of the loop — it happens at `/code-review`.
- **Pre-agreed seams.** Before the first test, list the seams under test and confirm them with the user. Prefer the highest existing seam; ideally one.
- **The interface is the test surface.** Tests cross the same seam callers use; needing internals means the module is the wrong shape.
- **Deep modules.** Small interface over substantial behavior; apply the deletion test to every wrapper; a port/interface only when two adapters exist (production + test).
- **Mock only true externals** (Langfuse over HTTP via `httptest.Server`, time, randomness); real code for everything we own. Expected values come from literals, worked examples or the spec — never recomputed like the code does.
- **Ubiquitous language.** Read `CONTEXT.md` and relevant ADRs before exploring; name types, tools, tests, issues with its terms; update it via `/domain-modeling` the moment a term resolves. It is a glossary only — no implementation details.
- **ADRs** only when hard to reverse + surprising + a real trade-off. Surface conflicts as "_Contradicts ADR-NNNN — worth reopening because…_".
- Specs/tickets describe interfaces and behavioral contracts, not file paths or line numbers.
- Specs/tickets touching a tool, operation, parameter or workflow carry prompt-injection and dangerous-parameter acceptance criteria (`security.md` "Every slice").
