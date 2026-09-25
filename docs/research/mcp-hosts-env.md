# MCP hosts: how a local stdio server receives environment variables

Fact-finding only, no decisions. Access date for every source: **2026-09-25**.
Labels: `[sourced]` = quoted from an official doc or the host's own source code;
`[sourced — unverified]` = inferred from official text that does not say it outright, or
not found in official docs. No claim here is run-verified: no host was executed for this note.

**Why it matters for a Langfuse MCP server.** The server needs `LANGFUSE_PUBLIC_KEY`,
`LANGFUSE_SECRET_KEY` and `LANGFUSE_HOST` (or equivalents). Whether a key exported in
`~/.bashrc` or set as an OS environment variable reaches the server depends on the host, not
on the server. Several hosts start the server with a **filtered** environment.

## Summary table

| Host | Config file | Env declaration syntax | Inherits parent env? | Variable interpolation | Source URL (date) |
|---|---|---|---|---|---|
| Claude Code | `.mcp.json` (project), `~/.claude.json` (local/user scope); CLI `claude mcp add` | `"env": {"K": "v"}`; CLI `--env K=v` / `-e K=v` | Probably yes, the full Claude Code process env (docs say `env` settings reach "the subprocesses Claude Code starts"). No MCP-specific sentence found `[sourced — unverified]` | `${VAR}`, `${VAR:-default}` in `command`, `args`, `env`, `url`, `headers` `[sourced]` | https://code.claude.com/docs/en/mcp , https://code.claude.com/docs/en/settings-reference (2026-09-25) |
| Claude Desktop | macOS `~/Library/Application Support/Claude/claude_desktop_config.json`; Windows `%APPDATA%\Claude\claude_desktop_config.json` | `"env": {"K": "v"}` under `mcpServers.<name>` | **No, only a limited platform-dependent subset** `[sourced]` | None documented `[sourced — unverified]` | https://modelcontextprotocol.io/docs/develop/connect-local-servers , https://modelcontextprotocol.io/docs/tools/debugging (2026-09-25) |
| Cursor | `.cursor/mcp.json` (project), `~/.cursor/mcp.json` (global) | `"env": {...}`, `"envFile": "path"` (stdio only) | Not stated in docs `[sourced — unverified]` | `${env:NAME}`, `${userHome}`, `${workspaceFolder}`, `${workspaceFolderBasename}`, `${pathSeparator}`/`${/}` in `command`, `args`, `env`, `url`, `headers` `[sourced]` | https://cursor.com/docs/mcp (redirect from /docs/context/mcp) (2026-09-25) |
| VS Code / GitHub Copilot | `.vscode/mcp.json` or user-profile `mcp.json` (`servers`); portable `.mcp.json` / `~/.copilot/mcp-config.json` (`mcpServers`) | `"env": {...}` (values string, number or `null`), `"envFile": "path"` | **Yes, full `process.env`**, but confirmed only for the extension-host launch path (source code) `[sourced]`; the Agent Host path that reads the portable format is `[sourced — unverified]` | `${input:id}` (prompted, stored securely), predefined vars such as `${workspaceFolder}`, `${env:Name}` `[sourced]` | https://code.visualstudio.com/docs/copilot/reference/mcp-configuration , https://code.visualstudio.com/docs/reference/variables-reference , `microsoft/vscode` `src/vs/workbench/api/node/extHostMcpNode.ts` (2026-09-25) |
| OpenAI Codex CLI | `~/.codex/config.toml` or project `.codex/config.toml`, table `[mcp_servers.<name>]` | `env = {K = "v"}` / `[mcp_servers.<name>.env]`; `env_vars = ["NAME", ...]` allow-list; CLI `codex mcp add <name> --env K=V -- <cmd>` | **No: env is cleared, then only a fixed default allow-list plus `env_vars` is forwarded** `[sourced]` (source code) | None documented `[sourced — unverified]`; forward a host var by listing it in `env_vars` `[sourced]` | https://learn.chatgpt.com/docs/extend/mcp?surface=cli , https://learn.chatgpt.com/docs/config-file/config-reference , `openai/codex` `codex-rs/rmcp-client/src/utils.rs` (2026-09-25) |
| Gemini CLI | `~/.gemini/settings.json` (user), `.gemini/settings.json` (project), key `mcpServers` | `"env": {...}`; CLI `gemini mcp add -e KEY=value` | **Partially: inherits the host env but redacts sensitive names** (`*TOKEN*`, `*SECRET*`, `*KEY*`, ...) unless listed in `env` `[sourced]` | `$VAR`, `${VAR}` (all OSes), `%VAR%` (Windows) in `env`; undefined becomes `""` `[sourced]` | https://github.com/google-gemini/gemini-cli/blob/main/docs/tools/mcp-server.md (2026-09-25) |
| Windsurf (Cascade, docs now under Devin) | `~/.config/devin/mcp_config.json` (macOS/Linux, or `$XDG_CONFIG_HOME/devin/`), `%APPDATA%\devin\mcp_config.json` (Windows) | `"env": {...}` | Not stated `[sourced — unverified]` | `${env:VAR}` (unset gives `""`), `${file:/path}` in `command`, `args`, `env`, `serverUrl`, `url`, `headers` `[sourced]` | https://docs.windsurf.com/windsurf/cascade/mcp (redirects to https://docs.devin.ai/desktop/cascade/mcp) (2026-09-25) |
| Docker as the command (`docker run -i`) | host's config; `command: "docker"` | `-e VAR` in `args` (no `=`: value taken from the `docker` CLI's own env), `-e VAR=value`, `--env-file` | Only names passed with `-e`/`--env`/`--env-file` are set in the container `[sourced]` | Host-dependent (whatever the host interpolates in `args`/`env`) | https://docs.docker.com/reference/cli/docker/container/run/ (2026-09-25) |

**Hosts that do not pass arbitrary system/shell env vars to a stdio server by default:**
Codex CLI (clears env, then an allow-list), Claude Desktop (limited subset), Gemini CLI (passes most
but redacts anything matching `*KEY*`/`*SECRET*`/`*TOKEN*`, so `LANGFUSE_SECRET_KEY` and
`LANGFUSE_PUBLIC_KEY` would be dropped), and any Docker-wrapped server (the container sees only the
`-e` names). The MCP TypeScript SDK's `StdioClientTransport`, which some hosts build on, also passes only a
small default set (see below).

## Per-host evidence

### 1. Claude Code

- `[sourced]` CLI syntax (https://code.claude.com/docs/en/mcp):
  ```bash
  claude mcp add --env AIRTABLE_API_KEY=YOUR_KEY --transport stdio airtable \
    -- npx -y airtable-mcp-server
  ```
  > "`claude mcp add --env KEY=value --transport stdio myserver -- python server.py --port 8080` → runs `python server.py --port 8080` with `KEY=value` in environment"

  > "`--env` accepts multiple `KEY=value` pairs. If the server name comes directly after `--env`, the CLI reads the name as another pair and rejects it, so place at least one other option, such as `--transport stdio`, between `--env` and the server name."

  > "Set environment variables with `-e` or `--env` flags (for example, `-e KEY=value`)"
- `[sourced]` `.mcp.json` interpolation:
  > "Claude Code supports environment variable expansion in `.mcp.json` files, allowing teams to share configurations while maintaining flexibility for machine-specific paths and sensitive values like API keys."

  > "`${VAR}`: expands to the value of environment variable `VAR`" / "`${VAR:-default}`: expands to `VAR` if set, otherwise uses `default`"

  > "Environment variables can be expanded in: `command` ... `args` ... `env`: environment variables passed to the server ... `url` ... `headers`"

  > "If a referenced environment variable isn't set and has no default value, the config still loads: Claude Code reports a missing-variable warning for that server in `claude mcp list` output and uses the unexpanded `${VAR}` text as-is."
- `[sourced]` stdio JSON shape, from the same page's `add-json` examples:
  ```bash
  claude mcp add-json local-weather '{"type":"stdio","command":"/path/to/weather-cli","args":["--api-key","abc123"],"env":{"CACHE_DIR":"/tmp"}}'
  ```
  In `.mcp.json` this object sits under `"mcpServers": { "<name>": { ... } }`. The page's own `.mcp.json`
  sample is `{"mcpServers": {"example": {"command": "npx", "args": ["-y", "@example/mcp-server"]}}}`.
- `[sourced]` Remote-only caveat: in a remote server's `url`/`headers`, credential names such as `ANTHROPIC_API_KEY`, `AWS_BEARER_TOKEN_BEDROCK`, `HTTPS_PROXY`, `NPM_TOKEN` "read as empty". This does not apply to a stdio server's `env`.
- `[sourced]` Injected variable:
  > "Claude Code sets `CLAUDE_PROJECT_DIR` in the spawned server's environment to the project root"
- `[sourced — unverified]` Inheritance. The MCP page has no sentence saying stdio servers inherit
  the full environment. Indirect evidence: the settings reference says the `env` setting applies
  "for every session and for the subprocesses Claude Code starts from it"
  (https://code.claude.com/docs/en/settings-reference#env), and the plugin section says plugin
  servers get "access to the same environment variables as manually configured servers". Because
  Claude Code runs from a terminal, its process env normally includes shell-profile exports.
  Full pass-through is likely but not documented; test it before relying on it.

### 2. Claude Desktop

- `[sourced]` Config paths (https://modelcontextprotocol.io/docs/develop/connect-local-servers):
  > "**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`"
  > "**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`"
- `[sourced]` Inheritance (https://modelcontextprotocol.io/docs/tools/debugging):
  > "MCP servers launched over stdio inherit only a limited subset of environment variables automatically (the exact set is platform-dependent)."

  > "To override the default variables or provide your own, you can specify an `env` key in `claude_desktop_config.json`"
  ```json
  { "mcpServers": { "myserver": { "command": "mcp-server-myapp", "env": { "MYAPP_API_KEY": "some_key" } } } }
  ```
- `[sourced]` Windows symptom of the filtered env (connect-local-servers, troubleshooting):
  > "If your configured server fails to load, and you see within its logs an error referring to `${APPDATA}` within a path, you may need to add the expanded value of `%APPDATA%` to your `env` key in `claude_desktop_config.json`"
- `[sourced — unverified]` macOS GUI apps launched from Finder/Dock do not read `~/.zshrc` /
  `~/.bash_profile`, so shell `export`s and a shell-extended `PATH` (nvm, Homebrew, asdf) are
  absent. The official docs above only mention the "limited subset" and absolute paths. They do not
  mention this launchd behaviour, and it is not quoted here from an official source. Practical
  consequence: use an absolute `command` path and put secrets in `env`.
- `[sourced — unverified]` No `${VAR}` interpolation syntax is documented for this file.
- `[sourced]` The exact subset in the reference TypeScript SDK (see the SDK section below) is
  `HOME, LOGNAME, PATH, SHELL, TERM, USER` (POSIX) plus a Windows list. The docs do not say that
  Claude Desktop uses that SDK default.

### 3. Cursor

Source: https://cursor.com/docs/mcp (the requested `/docs/context/mcp` redirects there).

- `[sourced]` > "Project Configuration Create .cursor/mcp.json in your project for project-specific tools. Global Configuration Create ~/.cursor/mcp.json in your home directory for tools available everywhere."
- `[sourced]` stdio fields table: `type` ("stdio"), `command`, `args`, `env` ("Environment variables for the server", example `{"API_KEY": "${env:api-key}"}`), `envFile` ("Path to an environment file to load more variables", examples `".env"`, `"${workspaceFolder}/.env"`).
  > "The envFile option is only available for STDIO servers. Remote servers (HTTP/SSE) do not support envFile. For remote servers, use config interpolation with environment variables set in your shell profile or system environment instead."
- `[sourced]` > "Use variables in mcp.json values. Cursor resolves variables in these fields: command, args, env, url, and headers."
  Supported: `${env:NAME}`, `${userHome}`, `${workspaceFolder}` ("the folder that contains .cursor/mcp.json"), `${workspaceFolderBasename}`, `${pathSeparator}` and `${/}`.
  ```json
  { "mcpServers": { "local-server": { "command": "python", "args": ["${workspaceFolder}/tools/mcp_server.py"], "env": { "API_KEY": "${env:API_KEY}" } } } }
  ```
- `[sourced — unverified]` Inheritance of the full parent env is not stated. The quote above about
  "environment variables set in your shell profile or system environment" means Cursor's own process
  can see them for `${env:...}`. It does not say they are passed straight to the child. Cursor is a
  VS Code fork, so pass-through is plausible but undocumented. To be safe, forward each needed var
  explicitly with `"K": "${env:K}"`.

### 4. VS Code / GitHub Copilot

Source: https://code.visualstudio.com/docs/copilot/reference/mcp-configuration

- `[sourced]` > "The VS Code format is stored in .vscode/mcp.json in your workspace or in your user profile. It defines servers in a top-level servers object."
  > "The portable format is stored in .mcp.json at the root of your workspace or in ~/.copilot/mcp-config.json for your user. It defines servers in a top-level mcpServers object."
- `[sourced]` stdio fields: `env`: "Environment variables for the server. Values can be strings, numbers, or null." (example `{"API_KEY": "${input:api-key}"}`); `envFile`: "Path to an environment file to load more variables" (example `"${workspaceFolder}/.env"`); `cwd`: "Defaults to the workspace folder when run in a workspace."
- `[sourced]` Input variables:
  > "When you reference an input variable using ${input:variable-id}, VS Code prompts you for the value when the server starts for the first time. The value is then securely stored for subsequent use."
  Declared in a top-level `"inputs": []` array (`type` `promptString` / `pickString` / `command`, `id`, `description`, optional `password: true`).
- `[sourced]` > "You can use predefined variables in the server configuration, for example to refer to the workspace folder (${workspaceFolder})." The variables reference (https://code.visualstudio.com/docs/reference/variables-reference) says: "You can reference environment variables with the ${env:Name} syntax."
- `[sourced]` > "In a remote window, VS Code resolves configuration locations and environment variables in the remote environment."
- `[sourced]` > "VS Code forwards servers from .vscode/mcp.json to the Agent Host, except servers that require interactive input, such as ${input:...} variables."
- `[sourced]` Inheritance comes from the source code, not the docs. `microsoft/vscode`
  `src/vs/workbench/api/node/extHostMcpNode.ts` (main, fetched 2026-09-25):
  ```ts
  const env = { ...process.env };
  if (launch.envFile) { /* parseEnvFile(...) -> env[key] = value */ }
  for (const [key, value] of Object.entries(launch.env)) {
      // For PATH, we want to append to the existing PATH instead of overwriting it.
  ```
  The same function then passes this `env` object unchanged to `spawn(executable, args, { stdio: 'pipe', cwd, env, shell })`.
  A `null` value deletes the key (`env[key] = value === null ? undefined : String(value)`), and `PATH`
  entries are appended to the inherited `PATH` rather than replacing it.
  So the child gets the extension host's full env, then `envFile`, then `env`.
  This covers only the extension-host launcher. The docs also describe an Agent Host that
  "reads the portable format directly". How that host builds the child env was not checked
  `[sourced — unverified]`. On macOS the
  extension host's env comes from VS Code's shell-environment resolution when launched from the
  Dock. That last point is `[sourced — unverified]` here.

### 5. OpenAI Codex CLI

Sources: https://learn.chatgpt.com/docs/extend/mcp?surface=cli (the requested `developers.openai.com/codex/mcp` gives a 308 redirect there) and https://learn.chatgpt.com/docs/config-file/config-reference.

- `[sourced]` > "codex mcp add <server-name> --env VAR1=VALUE1 --env VAR2=VALUE2 -- <stdio server-command>"
- `[sourced]` > "For more fine-grained control, edit ~/.codex/config.toml or a project-scoped .codex/config.toml ... Configure each MCP server with a [mcp_servers.<server-name>] table in the configuration file."
- `[sourced]` STDIO options: "command (required) ... args (optional) ... env (optional): Environment variables to set for the server. env_vars (optional): Environment variables to allow and forward. cwd (optional): Working directory to start the server from."
  > "String entries and source = "local" read from Codex's local environment. source = "remote" reads from the remote executor environment and requires remote MCP stdio."
- `[sourced]` Config reference: `mcp_servers.<id>.env_vars` has type `array<string | { name = string, source = "local" | "remote" }>`, "Additional environment variables to whitelist for an MCP stdio server."
  ```toml
  [mcp_servers.context7]
  command = "npx"
  args = ["-y", "@upstash/context7-mcp"]
  env_vars = ["LOCAL_TOKEN"]
  [mcp_servers.context7.env]
  MY_ENV_VAR = "MY_ENV_VALUE"
  ```
- `[sourced]` Default is **not** full inheritance. Source code, `openai/codex` main (fetched 2026-09-25):
  - `codex-rs/utils/pty/src/child_command.rs`, `Command::new`: `tokio::process::Command::new(program)` followed by `.env_clear()`.
  - `codex-rs/rmcp-client/src/stdio_server_launcher.rs`: `let envs = create_env_for_mcp_server(env, &env_vars)` then `command.current_dir(&cwd).envs(&envs)`.
  - `codex-rs/rmcp-client/src/utils.rs`: the env is built from `DEFAULT_ENV_VARS.iter().chain(additional_env_vars)` (only those set in Codex's env), plus custom-CA keys, plus the explicit `env` map. On non-Windows:
    ```rust
    pub(crate) const DEFAULT_ENV_VARS: &[&str] = &[
        "HOME", "LOGNAME", "PATH", "SHELL", "USER", "__CF_USER_TEXT_ENCODING",
        "LANG", "LC_ALL", "TERM", "TMPDIR", "TZ",
    ];
    ```
    On Windows it is `codex_protocol::shell_environment::WINDOWS_CORE_ENV_VARS`.
  - `local_stdio_env_var_names` drops `env_vars` entries only when they match `NON_INHERITABLE_ENV_VARS`
    (`codex-rs/protocol/src/shell_environment.rs`). That list holds only Codex-internal names:
    `NODE_REPL_AUTH_TOKEN`, the exec-server noise auth token and the OpenAI federation/identity-token
    variables. It has no `*KEY*`/`*SECRET*` pattern, so an allow-listed `LANGFUSE_SECRET_KEY` gets through.
  - Consequence: an exported `LANGFUSE_SECRET_KEY` reaches the server only when you list it in `env_vars` (or set a literal in `env`).

### 6a. Gemini CLI

Source: https://github.com/google-gemini/gemini-cli/blob/main/docs/tools/mcp-server.md

- `[sourced]` Config lives in `settings.json` under `mcpServers`. User scope is `~/.gemini/settings.json`, project scope is `.gemini/settings.json`. CLI: `gemini mcp add ... -e, --env` ("Set environment variables (for example, `-e KEY=value`)").
- `[sourced]` > "**`env`** (object): Environment variables for the server process. Values can reference environment variables using `$VAR_NAME` or `${VAR_NAME}` syntax (all platforms), or `%VAR_NAME%` (Windows only)."
  > "If a variable is not defined in the current environment, it resolves to an empty string."
- `[sourced]` > "By default, the CLI redacts sensitive environment variables from the base environment (inherited from the host process). ... This includes: Core project keys: `GEMINI_API_KEY`, `GOOGLE_API_KEY`, etc. Variables matching sensitive patterns: `*TOKEN*`, `*SECRET*`, `*PASSWORD*`, `*KEY*`, `*AUTH*`, `*CREDENTIAL*`."
  > "If an environment variable must be passed to an MCP server, you must explicitly state it in the `env` property of the server configuration in `settings.json` ... Explicitly defined variables (including those from extensions) are trusted and are **not** subjected to the automatic redaction process."
  Recommended form: `"MY_KEY": "$MY_KEY"`.

### 6b. Windsurf (Cascade)

Source: https://docs.windsurf.com/windsurf/cascade/mcp, which now redirects to https://docs.devin.ai/desktop/cascade/mcp (page titled "Cascade MCP Configuration", describing the "legacy Cascade agent").

- `[sourced]` > "macOS / Linux: ~/.config/devin/mcp_config.json (or $XDG_CONFIG_HOME/devin/mcp_config.json if XDG_CONFIG_HOME is set)" / "Windows: %APPDATA%\devin\mcp_config.json"
  (Older Windsurf builds used `~/.codeium/windsurf/mcp_config.json`. That path is `[sourced — unverified]` and absent from the current page.)
- `[sourced]` > "The mcp_config.json file supports variable interpolation in the following fields: command, args, env, serverUrl, url, and headers."
  > "${env:VAR_NAME} — replaced with the value of the environment variable VAR_NAME. If the variable is not set, it resolves to an empty string."
  > "${file:/path/to/file} — replaced with the trimmed contents of the file at the given path. ... If the file cannot be read, the pattern is left unchanged."
- `[sourced — unverified]` Inheritance of the parent env is not stated.

### 7. Docker `docker run -i` as the MCP command

Source: https://docs.docker.com/reference/cli/docker/container/run/

- `[sourced]` `-i, --interactive`: "Keep STDIN open even if not attached". stdio MCP needs this.
- `[sourced]` > "Use the -e, --env, and --env-file flags to set simple (non-array) environment variables in the container you're running"
  > "You can also use variables exported to your local environment: `export VAR1=value1` ... `docker run --env VAR1 --env VAR2 ubuntu env | grep VAR`"
  > "When running the command, the Docker CLI client checks the value the variable has in your local environment and passes it to the container. If no = is provided and that variable isn't exported in your local environment, the variable is unset in the container."
- `[sourced]` Two hops follow from this. (1) The host must put the var into the `docker` CLI process's env, via
  the host's `env` block or inheritance. (2) `args` must name it with `-e VAR` (no value) to
  forward it into the container. Official example, from the GitHub MCP server README
  (https://github.com/github/github-mcp-server, VS Code format):
  ```json
  "github": {
    "command": "docker",
    "args": ["run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "-e", "GITHUB_HOST", "ghcr.io/github/github-mcp-server"],
    "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "${input:github_token}", "GITHUB_HOST": "https://<your GHES or ghe.com domain name>" }
  }
  ```
  Writing `-e VAR=value` in `args` instead would put the secret on the command line and in the config file.

### Reference: MCP TypeScript SDK default (`StdioClientTransport`)

`[sourced]` `modelcontextprotocol/typescript-sdk` `packages/client/src/client/stdio.ts` (main, 2026-09-25):
> "Environment variables to inherit by default, if an environment is not explicitly given."

POSIX `['HOME', 'LOGNAME', 'PATH', 'SHELL', 'TERM', 'USER']` ("list inspired by the default env
inheritance of sudo"). Windows `APPDATA, COMSPEC, HOMEDRIVE, HOMEPATH, LOCALAPPDATA, PATH, PATHEXT,
PROCESSOR_ARCHITECTURE, PROGRAMDATA, PROGRAMFILES, PROGRAMFILES(X86), PROGRAMW6432, SYSTEMDRIVE,
SYSTEMROOT, TEMP, USERNAME, USERPROFILE, WINDIR`. At spawn: `env: { ...getDefaultEnvironment(), ...this._serverParams.env }`.
A host that uses this transport and passes only the configured `env` gives the server that small
set plus the configured keys. It does not give the full user env.
