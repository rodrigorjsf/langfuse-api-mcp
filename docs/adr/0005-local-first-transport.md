---
status: accepted
---
# Local-first: stdio default, loopback Streamable HTTP opt-in; ship binary, Docker image and MCPB

Anthropic's `build-mcp-server` skill recommends a remote Streamable HTTP server for SaaS APIs — which is precisely the shape of the official Langfuse MCP and cannot see the user's corporate CA or proxy. This server deliberately runs on the user's machine: stdio is the default transport; Streamable HTTP is opt-in, binds to loopback only and requires a bearer token (MCP security guidance for local servers). Distribution: release binaries for Linux/macOS/Windows (amd64/arm64), a minimal Docker image, and an MCPB bundle (`"type": "binary"`).

## Consequences

- The server is stateless (spec 2026-07-28, no sessions); it holds no data between calls beyond configuration.
- It never accepts client-supplied credentials for Langfuse (no token passthrough): Langfuse keys come only from its own configuration.
