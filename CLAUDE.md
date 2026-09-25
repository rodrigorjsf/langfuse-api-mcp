# langfuse-api-mcp

Stateless Go MCP server exposing the Langfuse public API (Cloud + self-hosted) with explicit corporate TLS/CA support. Phase: **M0 foundations — no code yet** (`ROADMAP.md`).

## Read before working

| Need | Go to |
|---|---|
| domain terms (use them verbatim) | `CONTEXT.md` |
| why things are the way they are | `docs/adr/` (0001 Go · 0002 tool surface · 0003 read-only default · 0004 scope · 0005 local-first · 0006 TLS · 0007 license) |
| evidence behind decisions | `docs/research/INDEX.md` |
| milestones | `ROADMAP.md` |

Rules in `.claude/rules/` load automatically; path-scoped ones (`go`, `testing`, `mcp-tool-design`, `langfuse-api`) load when you touch matching files.

## Definition of done

Never report work as done unless, in this turn, you have fresh output of `go build ./...`, `go test -race ./...`, `golangci-lint run` (once code exists) **and** every affected doc is updated in the same commit (`.claude/rules/docs-sync.md`). Unclosable gaps become GitHub issues (`/follow-up-issue`), not chat notes.

## Always

- MCP work: load `mcp-server-dev` skills first (`.claude/rules/mcp-server-dev.md`).
- Architecture: create/recreate/review with `archify` / `archify-review` (`.claude/rules/architecture.md`).
- Langfuse facts: verify with the `langfuse-docs` MCP before coding them.
- Process: Matt Pocock flow and vocabulary (`.claude/rules/engineering-process.md`).
- Security controls are mandatory (`.claude/rules/security.md`).
- User-facing skills (M6): author with `/writing-great-skills`.

## Agent skills

### Issue tracker

Issues and specs live as GitHub issues in `rodrigorjsf/langfuse-api-mcp` (use the `gh` CLI). See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles use their default label strings (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
