# Docs are part of done

A change is **not done** until every doc it affects matches the code in the same commit. Never report work as done, complete, or ready while a doc below is stale.

| If the change touches… | Update in the same commit |
|---|---|
| config variable, flag, default, transport, install/run step | `README.md` (config tables + scenarios, install and client snippets) + `docs/reference/configuration.md` / `docs/reference/installation.md` |
| a tool name, description, annotation, or the operation catalog | `README.md` tool table + `docs/reference/tools.md`; `docs/research/langfuse.md` if API facts changed |
| a domain term or its meaning | `CONTEXT.md` (via `/domain-modeling`) |
| a hard-to-reverse, surprising, trade-off decision | new/superseding ADR in `docs/adr/` (format: `/domain-modeling`) |
| scope, milestones, or a finished slice | `ROADMAP.md` |
| a security control | `docs/reference/security-model.md` + `.claude/rules/security.md`; `README.md` "Security model" when a user-facing essential changes |
| a new package or top-level directory | ADR + `project-structure.md` + `archify` diagram |
| build, CI, release or test procedure | `CONTRIBUTING.md` / the matching `docs/development/*.md` |
| a new doc | a row in `docs/INDEX.md` |
| agent workflow or rule | the matching `.claude/rules/*.md` / `CLAUDE.md` |

A Stop hook (`.claude/hooks/docs-sync-check.sh`) blocks ending a turn when code files changed and no doc did; satisfy it by updating docs, or state explicitly why no doc is affected.

Before claiming done, run and show fresh output of: `go build ./...`, `go test -race ./...`, `golangci-lint run` (first confirm `golangci-lint version` says built with go1.27+; a silent exit is not a pass — `CONTRIBUTING.md` install note), and a stale-docs check (`git diff --stat` and confirm each affected row above was edited). If a doc gap cannot be closed now, file an issue (`/follow-up-issue`) and link it — do not leave it in a chat summary.

A doc that describes something not yet implemented must say so (`Planned`), never present it as shipped. `README.md` is user-facing only (what the server does, install, configure, security essentials); deeper material goes to `docs/` through `docs/INDEX.md`. It links no GitHub issue, no ADR and no `docs/research/raw` record: the word `Planned` alone marks unshipped behaviour there, and its tracking lives in the issue tracker and `ROADMAP.md`.
