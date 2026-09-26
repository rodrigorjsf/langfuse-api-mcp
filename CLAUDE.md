# langfuse-api-mcp

Stateless Go MCP server exposing the Langfuse public API (Cloud + self-hosted) with explicit corporate TLS/CA support. Status: `ROADMAP.md`.

## Read before working

| Need | Go to |
|---|---|
| domain terms (use them verbatim) | `CONTEXT.md` |
| why things are the way they are | `docs/adr/` (0001 Go · 0002 tool surface · 0003 read-only default · 0004 scope · 0005 local-first · 0006 TLS · 0007 license · 0008 tool errors · 0009 package layout · 0010 generic executor · 0011 config file · 0012 version-aware catalog) |
| architecture diagram (archify) | `docs/architecture/target-architecture.html` |
| evidence behind decisions | `docs/research/INDEX.md` |
| milestones | `ROADMAP.md` |

Before creating any file, place it per `.claude/rules/project-structure.md`. Rules in `.claude/rules/` load automatically; path-scoped ones load when you touch matching files.

## Definition of done

Never report work as done without the fresh-output checklist in `.claude/rules/docs-sync.md`. Unclosable gaps become GitHub issues (`/follow-up-issue`), not chat notes.

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

## Applied Learning

When something fails repeatedly, when User has to re-explain, or when a workaround is found for a platform/tool limitation, add a one-line bullet here. Keep each bullet under 15 words. No explanations. Only add things that will save time in future sessions.

- archify showcase: landscape viewBox ~1100x530; wider fails readability, taller overflows viewport.
- Wizard `template.sh` ships CRLF; `sed -i 's/\r$//'` before `bash -n`.
- Vertical archify edge labels overlap nodes by default; set `labelAt` at segment midpoint.
- Langfuse compose: `docker compose pull` first; cached `:4` image silently stale.
- Windows Go test from WSL: `GOOS=windows` build, run `.exe` directly; WSL env not inherited.
- Old Langfuse compose: pin postgres 17 and clickhouse 24.3; `latest` breaks.
