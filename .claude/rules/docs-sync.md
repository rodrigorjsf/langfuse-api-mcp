# Docs are part of done

A change is **not done** until every doc it affects matches the code in the same commit. Never report work as done, complete, or ready while a doc below is stale.

| If the change touches… | Update in the same commit |
|---|---|
| config variable, flag, default, transport, install/run step | `README.md` (config tables + scenarios) |
| a tool name, description, annotation, or the operation catalog | `README.md` tool table; `docs/research/langfuse.md` if API facts changed |
| a domain term or its meaning | `CONTEXT.md` (via `/domain-modeling`) |
| a hard-to-reverse, surprising, trade-off decision | new/superseding ADR in `docs/adr/` (format: `/domain-modeling`) |
| scope, milestones, or a finished slice | `ROADMAP.md` |
| a security control | `README.md` "Security model" + `.claude/rules/security.md` |
| agent workflow or rule | the matching `.claude/rules/*.md` / `CLAUDE.md` |

Before claiming done, run and show fresh output of: tests, `golangci-lint run`, and a stale-docs check (`git diff --stat` and confirm each affected row above was edited). If a doc gap cannot be closed now, file an issue (`/follow-up-issue`) and link it — do not leave it in a chat summary.

A doc that describes something not yet implemented must say so (`Planned`), never present it as shipped.
