---
status: accepted
---
# Domain-oriented package layout with a fixed dependency direction

Packages are cut by domain concept (`config`, `trust`, `langfuse`, `catalog`, `sanitize`, `workflows`, `server`, `transport`) under `internal/`, each a deep module behind a small interface, with a one-way dependency direction (`cmd → transport → server → workflows → langfuse → trust`; `catalog`, `sanitize` leaves). Technical-layer folders (`handlers/`, `models/`, `utils/`) are banned. The exact tree, placement table and import rules live in `.claude/rules/project-structure.md` so agents place new files by domain; changing it needs a new ADR plus an `archify` diagram update. `trust` is split from `langfuse` because certificate loading is the project's core differentiator and must be tested per OS in isolation; `sanitize` is split so every payload path shares one audited implementation.
