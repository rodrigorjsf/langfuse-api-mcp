---
status: accepted
---
# Local-first: stdio default, loopback Streamable HTTP opt-in; ship binary, Docker image and MCPB

Anthropic's `build-mcp-server` skill recommends a remote Streamable HTTP server for SaaS APIs — which is precisely the shape of the official Langfuse MCP and cannot see the user's corporate CA or proxy. This server deliberately runs on the user's machine: stdio is the default transport; Streamable HTTP is opt-in, binds to loopback only and requires a bearer token (MCP security guidance for local servers). Distribution: release binaries for Linux/macOS/Windows (amd64/arm64), a minimal Docker image, and an MCPB bundle (`"type": "binary"`).

## Consequences

- The server is stateless (spec 2026-07-28, no sessions); it holds no data between calls beyond configuration.
- It never accepts client-supplied credentials for Langfuse (no token passthrough): Langfuse keys come only from its own configuration.

## Amendment: an npm channel, and nothing published before the repository is public (2026-09-27, M5 grilling)

The server targets every MCP host, not only Claude: binary, npm and Docker are host-agnostic and cover Linux; MCPB is the one Claude-specific channel (only Claude for macOS and Windows installs `.mcpb` today). A fourth channel is added: an npm package `langfuse-api-mcp` (bin `langfuse-mcp`), so hosts configured with `npx -y` run the Go binary. It carries one optional dependency per platform holding the prebuilt binary and a small shim that execs it with stdio inherited — no `postinstall` download, which fails with `--ignore-scripts` and behind a corporate proxy that cannot reach GitHub (GoReleaser's npm publisher does that and is Pro-only, so the packages are built from GoReleaser's output by our own script). A binary installed by npm, `curl`, `gh` or Docker carries no macOS quarantine flag or Windows Mark-of-the-Web, so the release ships without Apple notarization or Authenticode; a browser download needs the workaround the README gives.

Targets are six: Linux, macOS, Windows × amd64, arm64 (`windows/arm64` built, not CI-tested). The MCPB is one bundle: a universal macOS binary and `windows/amd64`, since the manifest's `platform_overrides` is keyed by OS only.

Considered: keeping three channels — rejected because most host configurations launch servers with `npx`; a postinstall download — rejected for the proxy and `--ignore-scripts` failures above.

Consequences: Node becomes a runtime dependency of that channel only, and the npm registry joins the supply chain. `npm publish`, `cosign` keyless signing (its Rekor entry is public and permanent), build provenance and the GHCR push run only on a release tag, and the first tag is cut after the repository goes public (M7); until then the release pipeline is proven by snapshot builds.

## Amendment: the user skill ships beside the server, not inside a plugin (2026-09-28, M6 grilling)

M6 adds one user skill, `skills/langfuse-api-mcp/`, in the Agent Skills layout (`skills/<name>/SKILL.md` + `references/`). None of the four channels above carries a skill, and every MCP host the README configures (Claude Code, Claude Desktop, Cursor, VS Code, Codex CLI, Gemini CLI, Windsurf) loads Agent Skills natively, each from its own directory. The skill therefore ships two ways: installed from the repository with `npx skills add rodrigorjsf/langfuse-api-mcp` (one command for every host but Claude Desktop; it uses the git credentials already on the machine, so it works while the repository is private), and as a ZIP attached to the GitHub release for Claude Desktop, which installs skills only by upload.

Considered: a Claude Code plugin bundling the skill with an `.mcp.json` that launches `npx langfuse-api-mcp` — the best single-install experience in Claude Code, deferred because it depends on the npm package being published (M7), serves one host only, and renames the tools to `mcp__plugin_<plugin>_<server>__<tool>`, which breaks users' existing permission rules and hooks; revisit after the first release. MCP prompts served by the server — rejected: prompts are user-invoked commands, so the model never loads one on its own, and a skill is what an agent triggers from its description.

Consequences: the skill is fetched from `main` while the installed binary may be older, so the skill names only the stable surface (tool names, ADR-0008 error codes, v4 operation IDs) and sends the agent to `describe_operation` for everything else; the server can neither load the skill nor rely on it being loaded (ADR-0002), so a fix that must reach every client still belongs in the server's static text.
