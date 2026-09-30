# Configuration reference

Every setting in full: defaults, allowed values, startup errors and log lines, the deployment profile, each MCP client's environment handling and the config file rules. The short version is in the [README](../../README.md#configuration) · [documentation index](../INDEX.md).

## Configuration

Settings come from environment variables, then from an optional [config file](#config-file-non-secret-settings) for non-secret settings. Values set **system-wide** reach the server only if your MCP client passes its environment through; several clients do not (see [Where do environment variables come from?](#where-do-environment-variables-come-from)).

### Connection

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `LANGFUSE_PUBLIC_KEY` | yes | — | Project public key (`pk-lf-…`). Environment only. |
| `LANGFUSE_SECRET_KEY` | yes | — | Project secret key (`sk-lf-…`). Environment only; never logged and never returned to the agent. |
| `LANGFUSE_BASE_URL` | yes | — | Langfuse host, an absolute `https` URL. Plain `http` is accepted only for a loopback host (`localhost`, `127.0.0.0/8`, `::1`), because the keys travel in every request; any other `http` host stops startup. `LANGFUSE_HOST` is accepted as an alias (`LANGFUSE_BASE_URL` wins when both are set). May also be set in the config file; the environment wins. |

A missing host or key, a key without its Langfuse prefix (`pk-lf-` for the public key, `sk-lf-` for the secret key) or a swapped pair stops startup with one error naming the variable, without echoing the key. The host has no default on purpose: a default would send the keys of an operator who forgot the host (typically self-hosted) to a Cloud region. Cloud regions are chosen by URL; there is no region-name shortcut.

Cloud regions: EU `https://cloud.langfuse.com` · US `https://us.cloud.langfuse.com` · JP `https://jp.cloud.langfuse.com` · HIPAA `https://hipaa.cloud.langfuse.com`. Keys only work in the region where they were created.

### Certificates and proxy

| Variable | Kind | If it can't be loaded | Status |
|---|---|---|---|
| `LANGFUSE_CA_CERT` | PEM file with one or more CA certificates | startup fails with an error naming the variable and the path (missing, unreadable, empty, no PEM certificate, or a damaged certificate) | works: loaded into the trust pool at startup |
| `LANGFUSE_CA_CERTS_PATH` | directory of PEM files; every regular file directly inside it that holds PEM certificates is loaded (subdirectories and non-PEM files are ignored) | startup fails with an error naming the variable and the path (missing directory, unreadable file, a damaged certificate, or no PEM certificate at all) | works: loaded into the trust pool at startup |
| `SSL_CERT_FILE`, `SSL_CERT_DIR`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE` (**ambient**) | picked up automatically if already set, so a CA you exported for other tools works here too. `SSL_CERT_DIR` may list several directories, separated by your OS's path-list separator (`:` on Linux/macOS, `;` on Windows); each is loaded like `LANGFUSE_CA_CERTS_PATH` | a `WARN` log line naming the variable and the path, then the source is skipped and startup continues. In a directory, only the files that cannot be loaded are skipped | works: loaded into the trust pool at startup |
| `LANGFUSE_MCP_IGNORE_AMBIENT_CA` | `true` = do not pick up the variables in the row above; only the OS store and `LANGFUSE_CA_CERT`/`LANGFUSE_CA_CERTS_PATH` are trusted. Accepts `true`/`false` in any of Go's boolean spellings (`true`, `TRUE`, `True`, `t`, `1` and their false counterparts) | any other value stops startup with an error naming the variable | works |
| `HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY` (and their lower-case spellings) | standard proxy variables, read from the environment or the [config file](#config-file-non-secret-settings) (the environment wins for a variable it sets in either spelling): the Langfuse client sends its requests through the proxy with Go's standard semantics (upper case wins over lower case on Linux and macOS; on Windows, where variable names are case-insensitive, any casing such as `Https_Proxy` works; `NO_PROXY` hosts are reached directly). Proxy URL scheme `http`, `https`, `socks5` or `socks5h`; a bare `host` or `host:port` is read as an `http` proxy, as Go and curl do; a scheme-less value holding `/` or `@` (such as `user:pw@proxy:3128` or `proxy:3128/`), which Go would still accept, is refused: write the scheme | a value that does not parse as a URL, has no host or a percent-encoded one, uses another scheme or a port outside 1–65535, or holds whitespace, control or invisible characters stops startup with an error naming the variable and its source (`environment`, or `config file` with its path and line), never the value | works, from the environment and from the config file |

"Works" means the server builds its trust pool from these sources when it starts, logs them, and the Langfuse client trusts exactly that pool for its connections.

**Proxy.** `HTTPS_PROXY` is the variable that matters: Langfuse hosts are `https` except loopback ones, and Go never sends a loopback host through a proxy, so `HTTP_PROXY` is checked at startup but never used. Through the proxy, the TLS connection to Langfuse is still verified end to end against the trust pool above, exactly as without a proxy. Credentials in the proxy URL (`http://user:password@proxy:3128`) are sent as **Basic** proxy authentication, the only kind supported: NTLM and Kerberos proxies need a local authenticating relay in front of them. The credentials never appear in a log line, an error, a hint or a tool result. The startup log shows the proxy in use as `scheme://host:port` with its variable and source, or `none`, and whether `NO_PROXY` is set:

```json
{"time":"…","level":"INFO","msg":"proxy","endpoint":"http://proxy.internal:3128","variable":"HTTPS_PROXY","source":"environment","noProxySet":false}
```

The server does not read the Windows or macOS proxy settings, nor a PAC file. If your proxy is configured only there, find it and set `HTTPS_PROXY` yourself: on Windows run `netsh winhttp show proxy` (or look under Settings → Network & Internet → Proxy); on macOS run `scutil --proxy`; with a PAC file, open the PAC URL those commands show and read the `PROXY host:port` it returns for your Langfuse host.

Trusted roots = **your operating system's certificate store + every CA from the sources above**. Nothing replaces the OS store: Go normally lets `SSL_CERT_FILE`/`SSL_CERT_DIR` *replace* it, so the server reads them as extra CA sources and removes them from its own environment before building the trust pool. If the OS offers no certificates at all (for example a minimal container image without a CA bundle), the server starts from the public roots bundled into the binary instead, then adds your CAs. TLS 1.2 is the minimum version. **There is no option to disable certificate verification.** This is deliberate.

### Request limits

| Variable | Default | Allowed values | Meaning |
|---|---|---|---|
| `LANGFUSE_MCP_RATE_LIMIT` | `30` on a Langfuse Cloud host, `1000` on any other host | whole number, 1 to 60000 | The most Langfuse requests the server sends per minute, retries included. A value you set always wins, on any host. |
| `LANGFUSE_MCP_MAX_CONCURRENCY` | `4` | whole number, 1 to 64 | The most Langfuse requests in flight at once. Up to this many requests may also leave at once before the rate limit starts pacing them. |

**Rate-limit default by host.** When you do not set `LANGFUSE_MCP_RATE_LIMIT`, the server picks the default from the host of `LANGFUSE_BASE_URL`. A Cloud host (exactly `cloud.langfuse.com`, `us.cloud.langfuse.com`, `jp.cloud.langfuse.com` or `hipaa.cloud.langfuse.com`; case and port do not matter) gets `30`, the Langfuse Cloud Hobby General API limit, the lowest plan's, so a Hobby project does not hit Langfuse's 429s under load. Any other host is self-hosted, which Langfuse does not rate-limit, and gets `1000`: generous, yet still a brake on an agent loop hammering your own instance. **On a paid Cloud plan, raise the value yourself** (for example to your plan's General API limit): the host does not reveal the plan. The host must match exactly; a look-alike such as `cloud.langfuse.com.example.org` counts as self-hosted. The startup log states the effective value and why it applies, as one JSON line on stderr:

```json
{"time":"…","level":"INFO","msg":"rate limit","perMinute":30,"source":"default-cloud"}
```

`source` is `default-cloud`, `default-self-hosted` or `explicit` (you set the variable, in the environment or the config file).

Both limits apply to the whole server process, shared by every tool call; no tool argument can change them. May also be set in the config file; the environment wins. A zero, negative, non-numeric or out-of-range value stops startup with an error naming the variable (from the config file, without quoting the value). A call that cannot get through the limits before its deadline is not sent: the agent gets the tool error `timeout` with `retryable: true` and a hint to call again later.

### Deployment profile

Nothing to configure: at startup the server detects which Langfuse it talks to, then offers exactly the operations that deployment serves. It asks `GET /api/public/health` for the version (without your keys) and sends one small request per operation family: legacy (`GET /api/public/traces?limit=1`), v4 read (`GET /api/public/v2/observations?limit=1&fields=core`) and experiments (`GET /api/public/experiments?limit=1&fromStartTime=<now>`). All run in parallel, within your [request limits](#request-limits), and startup waits at most about 5 seconds for them. A family is off only when Langfuse answers that it does not serve it (a 404 with an HTML body, or one naming `events_only` or a v4 write mode). Anything else (401, another error, no answer in time) keeps the family on and logs a `WARN` line, so a Langfuse that is slow or down at startup hides nothing. A health answer without a plain `major.minor.patch` version leaves the version unknown, which keeps the operations of every release. A version below 3.0.0 is unsupported: the server logs `{"level":"WARN","msg":"unsupported Langfuse version","version":"2.95.0",…}` and filters by version range alone, ignoring the families. `/v2/metrics` is never called, so detection does not spend the Cloud Hobby plan's 100-per-day metrics budget. The profile is fixed until the process exits; restart the server after upgrading Langfuse.

The startup log shows what was detected, as one JSON line on stderr:

```json
{"time":"…","level":"INFO","msg":"deployment profile","version":"4.46.0","families":["v4 read","experiments"],"undecided":[],"unsupported":false,"operations":93}
```

`version` is `unknown` when it could not be detected; `families` lists the families on; `undecided` lists those of them kept on only because their probe got no deciding answer (the others answered); `unsupported` is `true` for a version below 3.0.0, and then `families` and `undecided` are empty because the families are ignored; `operations` counts the operations offered, reads and writes. Each undecided probe adds one line such as `{"level":"WARN","msg":"deployment profile probe undecided","probe":"legacy","reason":"family kept on: sentinel answered HTTP 401"}`. These lines never hold what Langfuse sent, nor your keys.

### Behavior

| Variable | Default | Meaning |
|---|---|---|
| `LANGFUSE_MCP_ALLOW_WRITES` | `false` | `true` turns [write mode](security-model.md#write-mode) on: `execute_write` is registered. Exactly `true` or `false`; any other value stops startup with an error naming the variable and where it was set, never the value. May also be set in the config file; the environment wins. Leave it off unless you want the agent to change Langfuse data. |
| `LANGFUSE_MCP_TRANSPORT` | `stdio` | `http` starts Streamable HTTP on `127.0.0.1` only, protected by a bearer token **(Planned)** |

The startup log states whether write mode is on, as one JSON line on stderr: `{"level":"INFO","msg":"write mode","on":true,"source":"environment"}` (`source` is `environment`, `config-file` or `default`).

## Client configuration

One snippet per MCP client. Every snippet uses the [npx](installation.md#npx) channel; to use a [release archive](installation.md#release-archive) instead, replace `"command": "npx", "args": ["-y", "langfuse-api-mcp"]` with `"command": "/absolute/path/to/langfuse-mcp"` and no `args` (`langfuse-mcp.exe` on Windows).

Each client starts the server with its own view of your environment, and several do not pass your shell's variables on (details and sources: [docs/research/mcp-hosts-env.md](../research/mcp-hosts-env.md)). So every snippet names the three variables the server needs in the client's own `env` mechanism, the one place a key may go. Settings that are not secret (`LANGFUSE_CA_CERT`, the proxy, the request limits) can go in the client's `env` as well, or once for every client in the [config file](#config-file-non-secret-settings). **Never put `LANGFUSE_PUBLIC_KEY` or `LANGFUSE_SECRET_KEY` in the config file**: the server refuses to start if it finds them there.

`pk-lf-...` and `sk-lf-...` below stand for your keys. Where a snippet reads `${LANGFUSE_SECRET_KEY}` or similar, the client copies the value from its own environment when it starts the server, so the key never sits in the file. It does sit in the environment the client runs in, which the agent's own shell and tools inherit: see [Keep the keys out of the agent's environment](../../README.md#keep-the-keys-out-of-the-agents-environment). The snippets were checked against each client's official docs on 2026-09-25. On Linux on 2026-09-27 ([records](../research/raw/2026-09-27-mcp-host-proof.md)), the one marked **proven** ran end to end (`search_operations`, then `execute_read`, against a local Langfuse); the one marked **startup proven** started the server with its keys, but no tool call was made through it (a non-Anthropic client's tool calls are still to be recorded).

### Claude Code — proven

Source: [code.claude.com/docs/en/mcp](https://code.claude.com/docs/en/mcp). **Pitfall:** Claude Code passes its own environment to the server, but that environment is only what the shell that started Claude Code exported; a variable set elsewhere is missing. **Workaround:** pass the keys with `--env`. `--env` takes several `KEY=value` pairs and reads the next word as one more pair, so an option (here `--transport stdio`) must sit between the last `--env` and the server name, or the command fails with `Invalid environment variable format`:

```bash
claude mcp add \
  --env LANGFUSE_PUBLIC_KEY=pk-lf-... \
  --env LANGFUSE_SECRET_KEY=sk-lf-... \
  --env LANGFUSE_BASE_URL=https://cloud.langfuse.com \
  --transport stdio langfuse -- npx -y langfuse-api-mcp
```

This stores the keys in `~/.claude.json` (local or user scope). For a project `.mcp.json` shared through git, you can reference the variables instead (see the trade-off below the snippet); Claude Code expands `${VAR}` and `${VAR:-default}` in `env`. A variable that is not set and has no default is passed on as the literal text `${VAR}`, which the server rejects at startup as a key without its `pk-lf-`/`sk-lf-` prefix; `claude mcp list` warns about it:

```json
{
  "mcpServers": {
    "langfuse": {
      "command": "npx",
      "args": ["-y", "langfuse-api-mcp"],
      "env": {
        "LANGFUSE_PUBLIC_KEY": "${LANGFUSE_PUBLIC_KEY}",
        "LANGFUSE_SECRET_KEY": "${LANGFUSE_SECRET_KEY}",
        "LANGFUSE_BASE_URL": "${LANGFUSE_BASE_URL:-https://cloud.langfuse.com}"
      }
    }
  }
}
```

**Trade-off:** the `${VAR}` form needs the keys exported where Claude Code starts, so the agent's own commands inherit them and can write outside this server's write gate: [Keep the keys out of the agent's environment](../../README.md#keep-the-keys-out-of-the-agents-environment).

**Protocol.** Over stdio the server speaks the MCP protocols negotiated through the `initialize` handshake, up to 2025-11-25. It does not offer 2026-07-28 on stdio. Claude Code first probes `server/discover` on 2026-07-28, and after 3 s without an answer it falls back to `initialize`. The server reads stdin only after it has detected your Langfuse deployment, which takes up to 5 s, so a slow Langfuse, a far Cloud region or a proxy can leave both messages waiting. The MCP Go SDK v1.8.0 then refused the `initialize` as a duplicate, and Claude Code 2.1.283 marked the server `failed`. With 2026-07-28 not offered on stdio, the `initialize` succeeds, and Claude Code connects under a 5 s detection ([record](../research/raw/2026-09-28-claude-code-slow-startup-handshake.md)). stdio will offer 2026-07-28 again once the SDK accepts that `initialize`.

### Claude Desktop

Source: [modelcontextprotocol.io/docs/develop/connect-local-servers](https://modelcontextprotocol.io/docs/develop/connect-local-servers) and [modelcontextprotocol.io/docs/tools/debugging](https://modelcontextprotocol.io/docs/tools/debugging). **Pitfall:** Claude Desktop passes the server only "a limited subset of environment variables"; your shell exports never reach it, and on macOS an app started from the Dock does not see the `PATH` your shell profile builds (nvm, Homebrew), so a bare `npx` may not be found. **Workaround:** prefer the [MCPB bundle](installation.md#claude-desktop-mcpb), which stores the keys in the OS keychain. To configure it by hand, edit `claude_desktop_config.json` (macOS `~/Library/Application Support/Claude/`, Windows `%APPDATA%\Claude\`), put the keys in `env`, and give `command` as an absolute path (`which npx` on macOS, `where npx` on Windows, or the path of the extracted `langfuse-mcp` binary):

```json
{
  "mcpServers": {
    "langfuse": {
      "command": "/absolute/path/to/npx",
      "args": ["-y", "langfuse-api-mcp"],
      "env": {
        "LANGFUSE_PUBLIC_KEY": "pk-lf-...",
        "LANGFUSE_SECRET_KEY": "sk-lf-...",
        "LANGFUSE_BASE_URL": "https://cloud.langfuse.com"
      }
    }
  }
}
```

This file holds the keys in plain text; that is why the MCPB bundle is the better choice here.

### Cursor

Source: [cursor.com/docs/mcp](https://cursor.com/docs/mcp). **Pitfall:** the docs do not say whether Cursor passes its environment to the server. **Workaround:** forward each variable explicitly with `${env:NAME}` in `env`, in `~/.cursor/mcp.json` (every project) or `.cursor/mcp.json` (one project):

```json
{
  "mcpServers": {
    "langfuse": {
      "command": "npx",
      "args": ["-y", "langfuse-api-mcp"],
      "env": {
        "LANGFUSE_PUBLIC_KEY": "${env:LANGFUSE_PUBLIC_KEY}",
        "LANGFUSE_SECRET_KEY": "${env:LANGFUSE_SECRET_KEY}",
        "LANGFUSE_BASE_URL": "${env:LANGFUSE_BASE_URL}"
      }
    }
  }
}
```

Cursor must itself have been started with those variables set (for example from a terminal where they are exported). **Trade-off:** this needs the keys exported where Cursor starts, so the agent's own commands inherit them and can write outside this server's write gate: [Keep the keys out of the agent's environment](../../README.md#keep-the-keys-out-of-the-agents-environment). Cursor also reads an `envFile`, but a `.env` file is a plaintext key file; keep it out of the repository if you use one.

### VS Code (GitHub Copilot)

Source: [code.visualstudio.com/docs/copilot/reference/mcp-configuration](https://code.visualstudio.com/docs/copilot/reference/mcp-configuration). **Pitfall:** VS Code passes its full environment, but a VS Code started from the Dock or Start menu may not have your shell's exports, and `.vscode/mcp.json` is usually committed. **Workaround:** declare the keys as `inputs` with `"password": true`: VS Code asks for them the first time the server starts and stores them securely. VS Code does not forward a server that needs `${input:...}` to its Agent Host, so there use `"LANGFUSE_SECRET_KEY": "${env:LANGFUSE_SECRET_KEY}"` (and the same for the other two) instead; that trade-off puts the keys in VS Code's environment, where the agent's terminal inherits them and can write to Langfuse without this server's write gate (see [Keep the keys out of the agent's environment](../../README.md#keep-the-keys-out-of-the-agents-environment)). The file uses `servers`, not `mcpServers`:

```json
{
  "inputs": [
    { "type": "promptString", "id": "langfuse-public-key", "description": "Langfuse public key", "password": true },
    { "type": "promptString", "id": "langfuse-secret-key", "description": "Langfuse secret key", "password": true }
  ],
  "servers": {
    "langfuse": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "langfuse-api-mcp"],
      "env": {
        "LANGFUSE_PUBLIC_KEY": "${input:langfuse-public-key}",
        "LANGFUSE_SECRET_KEY": "${input:langfuse-secret-key}",
        "LANGFUSE_BASE_URL": "https://cloud.langfuse.com"
      }
    }
  }
}
```

### Codex CLI

Source: [learn.chatgpt.com/docs/extend/mcp?surface=cli](https://learn.chatgpt.com/docs/extend/mcp?surface=cli) and the [config reference](https://learn.chatgpt.com/docs/config-file/config-reference). **Pitfall:** Codex clears the environment before it starts the server and passes on only a fixed list (`HOME`, `PATH`, `LANG`, …); an exported `LANGFUSE_SECRET_KEY` never arrives. **Workaround:** list the variable names in `env_vars` in `~/.codex/config.toml` (or a project `.codex/config.toml`); Codex then forwards their values from its own environment, and no key is written in the file:

```toml
[mcp_servers.langfuse]
command = "npx"
args = ["-y", "langfuse-api-mcp"]
env_vars = ["LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY", "LANGFUSE_BASE_URL"]
```

**Trade-off:** this needs the keys exported where Codex starts, so the agent's own commands inherit them and can write outside this server's write gate: [Keep the keys out of the agent's environment](../../README.md#keep-the-keys-out-of-the-agents-environment).

Or set literal values with `codex mcp add --env LANGFUSE_PUBLIC_KEY=pk-lf-... --env LANGFUSE_SECRET_KEY=sk-lf-... --env LANGFUSE_BASE_URL=https://cloud.langfuse.com langfuse -- npx -y langfuse-api-mcp`, which stores them in plain text in `config.toml`; keep such a file out of the repository (a project `.codex/config.toml` is often committed). The same clearing applies to npm's own settings: if npm needs a proxy or a private registry, add `HTTPS_PROXY`, `npm_config_registry` or the like to `env_vars` too.

### Gemini CLI — startup proven

Source: [gemini-cli docs/tools/mcp-server.md](https://github.com/google-gemini/gemini-cli/blob/main/docs/tools/mcp-server.md). **Pitfall:** Gemini CLI passes its environment but removes every variable whose name contains `KEY`, `SECRET`, `TOKEN` (and a few more), so both Langfuse keys are dropped and the server stops at startup. **Workaround:** name the keys in `env`; variables named there are not removed. In `~/.gemini/settings.json` (or a project `.gemini/settings.json`, which Gemini CLI reads only in a trusted folder):

```json
{
  "mcpServers": {
    "langfuse": {
      "command": "npx",
      "args": ["-y", "langfuse-api-mcp"],
      "env": {
        "LANGFUSE_PUBLIC_KEY": "$LANGFUSE_PUBLIC_KEY",
        "LANGFUSE_SECRET_KEY": "$LANGFUSE_SECRET_KEY",
        "LANGFUSE_BASE_URL": "$LANGFUSE_BASE_URL"
      }
    }
  }
}
```

An unset variable becomes an empty string, which the server refuses at startup naming the variable. **Trade-off:** this needs the keys exported where Gemini CLI starts, so the agent's own commands inherit them and can write outside this server's write gate: [Keep the keys out of the agent's environment](../../README.md#keep-the-keys-out-of-the-agents-environment).

### Windsurf

Source: [docs.windsurf.com/windsurf/cascade/mcp](https://docs.windsurf.com/windsurf/cascade/mcp) (now served at docs.devin.ai). **Pitfall:** the docs do not say whether Windsurf passes its environment to the server. **Workaround:** forward each variable with `${env:NAME}` in `env`, in `~/.config/devin/mcp_config.json` (macOS and Linux; `%APPDATA%\devin\mcp_config.json` on Windows; older Windsurf builds may use `~/.codeium/windsurf/mcp_config.json`, a path the current docs no longer name):

```json
{
  "mcpServers": {
    "langfuse": {
      "command": "npx",
      "args": ["-y", "langfuse-api-mcp"],
      "env": {
        "LANGFUSE_PUBLIC_KEY": "${env:LANGFUSE_PUBLIC_KEY}",
        "LANGFUSE_SECRET_KEY": "${env:LANGFUSE_SECRET_KEY}",
        "LANGFUSE_BASE_URL": "${env:LANGFUSE_BASE_URL}"
      }
    }
  }
}
```

**Trade-off:** this needs the keys exported where Windsurf starts, so the agent's own commands inherit them and can write outside this server's write gate: [Keep the keys out of the agent's environment](../../README.md#keep-the-keys-out-of-the-agents-environment). Windsurf also reads `${file:/path}`, which puts the trimmed content of a file (for example a key file only you can read) in its place, and keeps the keys out of the environment.

### Docker as the command

In any client, `command` can be `docker` with the [Docker](installation.md#docker) `args`. The container sees only the variables named with `-e` in `args`, and `-e NAME` takes the value from the environment of the `docker` process, which is what the client passes it: put the keys in the client's `env` block (for Codex CLI, list them in `env_vars`), never as `-e NAME=value` in `args`.

## Where do environment variables come from?

The server reads its own process environment, then an optional config file. Whether your *system* variables reach the process depends on the MCP client (facts and sources: [docs/research/mcp-hosts-env.md](../research/mcp-hosts-env.md), checked 2026-09-25; Claude Code and Gemini CLI also run on Linux on 2026-09-27, [records](../research/raw/2026-09-27-mcp-host-proof.md)). The [per-client snippets](#client-configuration) above apply the last column:

| Client | Passes your system/shell variables? | What to do |
|---|---|---|
| VS Code / GitHub Copilot | yes (full environment) | nothing, or `"env"` / `${env:NAME}` |
| Claude Code | yes, the environment Claude Code was started with (not documented; observed on Linux) | `--env` pairs, or reference them: `"env": {"LANGFUSE_SECRET_KEY": "${LANGFUSE_SECRET_KEY}"}` |
| Cursor, Windsurf | not documented | reference them with `${env:NAME}` in `"env"` |
| Claude Desktop | **no**, only a limited subset | put values in `"env"`, or install the `.mcpb` bundle (keys stored in the OS keychain) |
| Codex CLI | **no**, the environment is cleared | list names in `env_vars = ["LANGFUSE_PUBLIC_KEY", …]` |
| Gemini CLI | yes, but **hides names containing `KEY`/`SECRET`/`TOKEN`** (observed on Linux: without `env` the server gets no keys and stops) | declare the keys explicitly in `"env"`: `"LANGFUSE_SECRET_KEY": "$LANGFUSE_SECRET_KEY"` |
| Docker | only what you pass with `-e` | `-e LANGFUSE_PUBLIC_KEY -e LANGFUSE_SECRET_KEY …` |

Every answer in the last column that reads a key from the client's environment ("nothing", `${VAR}`, `${env:NAME}`, `$NAME`, `env_vars`) needs the key exported where the client starts, and the agent's own shell and tools inherit it from there: see [Keep the keys out of the agent's environment](../../README.md#keep-the-keys-out-of-the-agents-environment).

**Variable names and case.** On Windows, variable names are case-insensitive, and the server reads them that way, like every other Windows program: `Langfuse_Base_Url` or `Https_Proxy` works as `LANGFUSE_BASE_URL` or `HTTPS_PROXY`, gets the same checks, and a startup error or log line names it in upper case (a lower-case `https_proxy`, `http_proxy` or `no_proxy` keeps its spelling). On Linux and macOS names are case-sensitive: only the exact names above are read, and `HTTPS_PROXY` and `https_proxy` are two variables, the upper case winning. Config-file keys are matched as described [below](#config-file-non-secret-settings), on every OS.

## Config file (non-secret settings)

To set the CA, host or proxy once for every client, put them in a config file at your OS's standard config location:

| OS | Path |
|---|---|
| Linux | `~/.config/langfuse-mcp/config.env` (or `$XDG_CONFIG_HOME/langfuse-mcp/config.env`) |
| macOS | `~/Library/Application Support/langfuse-mcp/config.env` |
| Windows | `%AppData%\langfuse-mcp\config.env` |

```ini
# KEY=VALUE per line; environment variables win over this file
LANGFUSE_BASE_URL=https://langfuse.internal.example.com
LANGFUSE_CA_CERT=/etc/ssl/private/corp-root.pem
HTTPS_PROXY=http://proxy.example.com:8080
```

How the file is read:

| Rule | Example |
|---|---|
| One `KEY=VALUE` per line; spaces around the key and the value are trimmed | `LANGFUSE_CA_CERT = /etc/corp/root.pem` |
| A leading `export ` (dotenv style) is ignored | `export LANGFUSE_CA_CERT=/etc/corp/root.pem` |
| Blank lines and lines starting with `#` are ignored | `# corporate CA` |
| Values are taken literally: no quotes, no `${VAR}` expansion | write `C:\corp\root.pem`, not `"C:\corp\root.pem"` |
| A variable set (non-empty) in the environment wins over the file; an empty one does not | `LANGFUSE_CA_CERT=` in the environment still uses the file's value |
| A line without `=`, or with nothing before `=`, stops startup with an error naming the file and the line number | `config file …/config.env line 2: expected KEY=VALUE` |
| No file at that location is fine; the server starts with environment variables only | — |
| An unknown key is ignored with a `WARN` log line naming the file, the line and the key (never the value) | `config file key ignored: not a known setting` … `"line":2,"key":"LANGFUSE_CA_CRT"` |
| Files saved by Windows editors (byte order mark, CRLF line endings) work | — |

CA paths from the file (`LANGFUSE_CA_CERT`, `LANGFUSE_CA_CERTS_PATH`) are explicit CA sources, exactly like the environment variables: if one cannot be loaded, startup fails naming the variable and the path. The five ambient variables (`SSL_CERT_FILE`, `SSL_CERT_DIR`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`) may be set in the file too, for MCP clients that do not forward your environment. Set there, they are explicit sources like every CA path in the file: a broken one stops startup naming the variable and the path, and `LANGFUSE_MCP_IGNORE_AMBIENT_CA=true` does not drop them. A variable set in the environment wins over the file's value for that variable, and then stays ambient. Today the server acts on the CA settings, the host (`LANGFUSE_BASE_URL`, alias `LANGFUSE_HOST`), the [request limits](#request-limits) (`LANGFUSE_MCP_RATE_LIMIT`, `LANGFUSE_MCP_MAX_CONCURRENCY`) and [write mode](security-model.md#write-mode) (`LANGFUSE_MCP_ALLOW_WRITES`, an invalid value there stops startup naming the file and the variable, never the value) in the file; the proxy variables (`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`) are used from the file too, in either spelling: within the file, and within the environment, the upper-case spelling wins over the lower-case one, as in Go and curl; a variable the environment sets in either spelling is read from the environment only, so an environment `https_proxy` wins over a file `HTTPS_PROXY`; the startup `proxy` line then shows `"source":"config-file"`. `LANGFUSE_MCP_TRANSPORT` is accepted but used only once that setting exists **(Planned)**. `LANGFUSE_MCP_IGNORE_AMBIENT_CA` may be set in the file too (the environment wins); an invalid value there stops startup with an error naming the file and the variable, never the value. A key the server does not know (for example the typo `LANGFUSE_CA_CRT`) is ignored, and startup logs one `WARN` line per such line naming the file, the line number and the key (control, invisible and bidi characters escaped), never the value; startup still succeeds. Key names are matched exactly, including case, with one exception: the proxy variables are known in both spellings (`https_proxy`, `http_proxy`, `no_proxy` too), because Go itself reads both spellings of them; a lower-case `langfuse_ca_cert` still draws the warning. The known keys are the settings above plus `LANGFUSE_MCP_TRANSPORT`, documented as **Planned**, so it draws no warning.

**Keys are not allowed in this file.** If it contains `LANGFUSE_PUBLIC_KEY` or `LANGFUSE_SECRET_KEY`, the server refuses to start and tells you to set the key in the environment or your MCP client's `env` block (the error names the line, never the value), so that no secret sits in a plaintext file.

The log at startup lists which CA sources were loaded (paths and counts, never contents). Use it to confirm the setup. It is one JSON line on stderr, for example:

```json
{"time":"…","level":"INFO","msg":"CA sources loaded","roots":"os","sources":[{"variable":"LANGFUSE_CA_CERT","path":"/etc/ssl/private/corp-root.pem","kind":"explicit","origin":"environment","certificates":2},{"variable":"NODE_EXTRA_CA_CERTS","path":"/home/me/corp-root.pem","kind":"ambient","origin":"environment","certificates":1}]}
```

`roots` is `os` (your operating system's certificate store) or `bundled-fallback` (the OS offered none). `kind` is `explicit` (`LANGFUSE_CA_CERT`, `LANGFUSE_CA_CERTS_PATH`, or any CA variable set in the config file) or `ambient` (the five widely used variables, read from the environment). `origin` is where the setting was read: `environment` or `config-file`. `certificates` is how many CA certificates each source added. With `LANGFUSE_MCP_IGNORE_AMBIENT_CA=true`, each ambient variable that is set is still listed, with `"certificates":0` and `"ignored":true` (not as a warning), so a log paste tells "ignored" apart from "not set". An ambient source that could not be fully loaded also carries a `warning`, and a separate `WARN` line is logged before the summary:

```json
{"time":"…","level":"WARN","msg":"CA source not fully loaded","variable":"REQUESTS_CA_BUNDLE","path":"/old/bundle.pem","warning":"source skipped: open /old/bundle.pem: no such file or directory"}
```
