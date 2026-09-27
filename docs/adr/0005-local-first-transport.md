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
