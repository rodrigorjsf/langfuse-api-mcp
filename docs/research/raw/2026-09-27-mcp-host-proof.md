# MCP client proof of the README client snippets (spec #119, ticket #125)

Run on 2026-09-27 (UTC 2026-09-28 01:00–01:30) by the agent implementing #125, on Linux (WSL2,
kernel 6.18.33.2-microsoft-standard-WSL2, x86_64). Verbatim results, keys redacted. What it proves
and what it does not is stated per host; nothing here was inferred without a run.

## Setup (shared by every host)

| Item | Value |
|---|---|
| Langfuse | local self-hosted 4.46.0 (`scripts/langfuse-selfhosted.sh up`, deployment `4.46.0-events_only`), base URL `http://localhost:3000`, a fresh project key pair from the script's headless init |
| Data | one prompt created through the public API before the runs: `host-proof-greeting` (text, label `production`) |
| Channel | **npx**, exactly `npx -y langfuse-api-mcp` as the README snippets say, version `0.0.0-SNAPSHOT-c5bc80c` |
| Packages | the 6 binaries built with `go build -trimpath -ldflags "-s -w -X main.version=0.0.0-SNAPSHOT-c5bc80c"` (`CGO_ENABLED=0`, one per GoReleaser target) at commit `c5bc80c`, described in a hand-written `dist/artifacts.json` + `metadata.json`, then packed by `node packaging/npm/pack.mjs` (7 tarballs). GoReleaser itself was not run; the packaging script and the shim are the shipped ones. |
| Registry | the packages are not published (M7), so npm was pointed at `packaging/npm/smoke-registry.mjs` serving the 7 tarballs on `127.0.0.1` (`npm_config_registry`), as the release workflow's npx smoke does |
| Node | 24.21.0 (official tarball; the pinned release-workflow version) |

The keys were exported in the shell as `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY` and
`LANGFUSE_BASE_URL` unless a row says otherwise.

## Results

| MCP client | Version | Snippet | Result |
|---|---|---|---|
| Claude Code | 2.1.283 | README `claude mcp add --env … --transport stdio langfuse -- npx -y langfuse-api-mcp` (local scope) | **PASS**. `claude mcp list`: `langfuse: npx -y langfuse-api-mcp - ✔ Connected`. Then `claude -p` (model `claude-haiku-4-5`) **with the three `LANGFUSE_*` variables unset in the parent shell**, so the keys could only come from the `--env` pairs: `search_operations` `{"query":"list prompts"}` returned `prompts_list`; `execute_read` `{"operationId":"prompts_list"}` returned the untrusted-data envelope with `host-proof-greeting`. `initialize` reported `serverVersion` `{"name":"langfuse-mcp","version":"0.0.0-SNAPSHOT-c5bc80c"}` (Claude Code MCP log). |
| Claude Code | 2.1.283 | README project `.mcp.json` with `${LANGFUSE_PUBLIC_KEY}`, `${LANGFUSE_SECRET_KEY}`, `${LANGFUSE_BASE_URL:-https://cloud.langfuse.com}` in `env`, loaded with `--mcp-config <file> --strict-mcp-config` | **PASS**. Same two calls, same result, keys from the parent environment through `${VAR}` expansion. |
| Gemini CLI | 0.61.0 | README `settings.json` with `"LANGFUSE_PUBLIC_KEY": "$LANGFUSE_PUBLIC_KEY"` (and the other two) in `env`, as a project `.gemini/settings.json` in a trusted folder, `HOME` set to a scratch directory | **Startup PASS**: `gemini mcp list` → `✓ langfuse: npx -y langfuse-api-mcp (stdio) - Connected`. No tool call: this machine has no Gemini credentials, so no model turn could run. |
| Gemini CLI | 0.61.0 | the same server **without** an `env` block, the keys still exported in the parent shell | `✗ langfuse: npx -y langfuse-api-mcp (stdio) - Disconnected`. The only difference from the row above is the `env` block, which confirms the documented redaction of `*KEY*`/`*SECRET*` names: without `env` the server gets no keys and stops at startup. |
| Codex CLI | 0.157.1 | README `config.toml` with `env_vars = ["LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY", "LANGFUSE_BASE_URL"]` (plus `npm_config_registry` and `npm_config_cache` for the local registry only) | **NOT RUN to completion**. No OpenAI credentials here, so `codex exec --oss --local-provider ollama -m qwen3:8b` was tried against the project's local Ollama stack (`scripts/small-model-eval-ollama.yml`). The model never loaded: `cudaMalloc failed: out of memory` on the RTX 3060 at every context size tried (16384 with f16 and q8_0 KV cache, 8192, 4096), with the Langfuse stack running beside it, also after raising the Ollama container's memory cap to 7 GiB; no MCP evidence was produced. `codex mcp add … --env K=V … langfuse -- npx -y langfuse-api-mcp` was run and writes the three pairs under `[mcp_servers.langfuse.env]` (in either argument order). |

## Snippet corrections found during the proof

- **Claude Code**: `claude mcp add --env A=… --env B=… --env C=… langfuse -- npx -y langfuse-api-mcp`
  fails with `Invalid environment variable format: langfuse, environment variables should be
  added as: -e KEY1=value1 -e KEY2=value2`: `--env` reads the server name as one more pair, as the
  Claude Code docs warn. The README snippet puts `--transport stdio` between the last `--env` and
  the name; that form is the one that passed above.
- **Claude Code inheritance**: `npm_config_registry`, set only in the shell that started Claude
  Code, reached `npx` (it resolved from the local registry), so Claude Code passes its own
  environment to a stdio server on Linux. `docs/research/mcp-hosts-env.md` had this as
  `[sourced — unverified]`.
- **Codex CLI**: the environment clearing also removes npm's own settings (`npm_config_*`, proxy
  variables), so an `npx` behind a proxy or a private registry needs them in `env_vars` too; the
  README says so. Observed only in the configuration, not in a completed run.

## Still open

Tracked in #128. The ticket asks for one non-Anthropic MCP client completing `search_operations` and `execute_read`.
Gemini CLI reached a proven startup with the keys delivered; the tool calls need a person with
Gemini CLI or Codex CLI credentials (or a working local model) to run the README snippet once and
append a row here.
