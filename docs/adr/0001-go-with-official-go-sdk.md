---
status: accepted
---
# Go with the official `modelcontextprotocol/go-sdk`

The server is written in Go on the official `modelcontextprotocol/go-sdk` (Tier 1, co-maintained with Google, supports spec 2026-07-28 from v1.7.0). Go gives a single static binary cross-compiled for Linux/macOS/Windows and a `scratch`/distroless Docker image with no runtime for the user to install, plus full in-code control of the TLS trust pool — the property the official Node-based Langfuse tooling lacks by default.

## Considered Options

- **TypeScript SDK** — default of Anthropic's `build-mcp-server` skill and best spec coverage, but ships a Node runtime that ignores the OS trust store and `HTTPS_PROXY` unless the *launcher* sets `NODE_USE_SYSTEM_CA` / `NODE_EXTRA_CA_CERTS` / `NODE_USE_ENV_PROXY` — the exact failure mode this project exists to fix.
- **Rust (`rmcp`)** — smallest footprint (~7 MB idle, community benchmark), but 2026-07-28 support was beta at decision time and sources disagree on its tier.
- **Python (FastMCP)** — ~97 MB idle and needs a Python runtime on the user's machine.

## Consequences

- Footprint numbers in `docs/research/` come from a community benchmark of `mark3labs/mcp-go`, not the official SDK — re-measure once the tracer bullet exists.
- SDK tiers can be relegated after 4 weeks of conformance failures; re-check the tier before major upgrades.
- Evidence: `docs/research/stack-and-sdk.md`.
