---
status: accepted
---
# Optional OS-standard config file for non-secret settings only

Several MCP hosts do not forward the user's environment to stdio servers (Codex clears it, Claude Desktop passes a subset, Gemini CLI redacts `*KEY*`/`*SECRET*` names — `docs/research/mcp-hosts-env.md`), so "set the corporate CA once in the system" cannot rely on environment variables alone. The server therefore also reads `config.env` from `os.UserConfigDir()/langfuse-mcp/` (KEY=VALUE lines, `#` comments). Precedence: environment > config file > defaults. The file may hold only non-secret settings (host, CA paths, proxy, behavior flags); if it contains `LANGFUSE_PUBLIC_KEY` or `LANGFUSE_SECRET_KEY` startup fails with an explicit error, so secrets never sit in a plaintext file. (Amended 2026-09-25, #12: an environment variable set to an empty value counts as unset and does not override the file, matching the rule that an empty value means unset; the key check ignores case and a leading dotenv `export `.) Keys arrive through the host's `env` block (with `${VAR}` interpolation where supported) or the MCPB keychain-backed `sensitive` config.

## Considered Options

- Documentation only — leaves Codex/Claude Desktop users re-declaring the CA per host.
- Allowing keys with a `0600` permission check — plaintext secrets on disk, against the `build-mcpb` local-security guidance.
- OS keychain for keys — extra dependency and per-OS behavior; MCPB already covers the Claude Desktop case.
