# langfuse-api-mcp

Stateless Go MCP server exposing the Langfuse public API (Cloud + self-hosted) with explicit corporate TLS/CA support. Status: `ROADMAP.md`.

## Read before working

| Need | Go to |
|---|---|
| every doc, user reference to dev process | `docs/INDEX.md` |
| domain terms (use them verbatim) | `CONTEXT.md` |
| why things are the way they are | `docs/adr/` |
| architecture diagram (archify) | `docs/architecture/target-architecture.html` |
| evidence behind decisions | `docs/research/INDEX.md` |
| milestones | `ROADMAP.md` |

Rules in `.claude/rules/` load automatically; path-scoped ones load when you touch matching files.

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

Issues and specs live as GitHub issues in `rodrigorjsf/langfuse-api-mcp` (use the `gh` CLI). Every issue carries a GitHub milestone (open today: `M8 Older-deployment fidelity`, `Later`); a milestone's spec absorbs its open issues. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles use their default label strings (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### End-to-end campaign

`/e2e-campaign <langfuse version>` tests the server against a seeded self-hosted Langfuse through subagents and writes the report to `docs/research/raw/`. See `docs/development/integration-tests.md` "End-to-end campaign".

## Applied Learning

When something fails repeatedly, when User has to re-explain, or when a workaround is found for a platform/tool limitation, add a one-line bullet here. Keep each bullet under 15 words. No explanations. Only add things that will save time in future sessions.

- Wizard `template.sh` ships CRLF; `sed -i 's/\r$//'` before `bash -n`.
- `gh issue view --comments`, `gh pr edit` fail (Projects-classic GraphQL); use `--json`, `gh api -X PATCH`.
- `golangci-lint` not on bash PATH; run `~/go/bin/golangci-lint`.
- System `gh` 2.45 lacks `gh attestation`; use `~/go/bin/gh` (`go install …/cmd/gh@latest`).
- Local `npm` is broken (missing `npm-cli.js`); run npm in a `node:22.11.0` container.
- New `.claude/agents` and MCP registrations load only at Claude Code start; restart first.
- Every `langfuse-selfhosted.sh up` makes new keys; restart MCP servers reading `.env.integration.selfhosted`.
- `.env.integration.selfhosted` names are `LANGFUSE_TEST_BASE_URL`, `LANGFUSE_TEST_PUBLIC_KEY`, `LANGFUSE_TEST_SECRET_KEY`; map them before curl.
- Terminal CLI replies to a desktop-app session expire held; give the user a paste block.
- Stack swapped under a running session: `/mcp` reconnect both servers before any campaign call.
- Edit tool turns `\uXXXX` in test strings into raw characters; write escapes via script.
