---
status: accepted
---
# Domain-oriented package layout with a fixed dependency direction

Packages are cut by domain concept (`config`, `trust`, `langfuse`, `catalog`, `sanitize`, `workflows`, `server`, `transport`) under `internal/`, each a deep module behind a small interface, with a one-way dependency direction (`cmd → transport → server → workflows → langfuse → trust`; `catalog`, `sanitize` leaves). Technical-layer folders (`handlers/`, `models/`, `utils/`) are banned. The exact tree, placement table and import rules live in `.claude/rules/project-structure.md` so agents place new files by domain; changing it needs a new ADR plus an `archify` diagram update. `trust` is split from `langfuse` because certificate loading is the project's core differentiator and must be tested per OS in isolation; `sanitize` is split so every payload path shares one audited implementation.

## Amendment: `langfuse` imports `catalog` for the operation family (2026-09-26, #78)

The operation family (ADR-0012 §2) is catalog data: each operation of the union catalog carries one. The deployment profile, detected in `langfuse`, lists the families that are on. The family type is defined once, in `catalog`, and `langfuse` imports it; `catalog` stays a leaf that imports nothing internal, so the direction stays one-way and acyclic. Two copies bridged by name in `cmd` were rejected: they could drift, and a test was needed only to keep them equal.

## Amendment: `langfuse` also imports the catalog's version rules (2026-09-29, #94)

The deployment profile needs the same version rules the catalog filters by: what a plain major.minor.patch version is and which versions sit below the supported floor (ADR-0012 §4). They are defined once, in `catalog` (`IsPlainVersion`, `BelowSupportedFloor`, `SupportedFloor`), and `langfuse` imports them beside the family type; `catalog` stays a leaf, so the direction stays one-way and acyclic. A second parser in `langfuse` was removed: two parsers of one version could disagree, and the floor was duplicated as a bare major number.
