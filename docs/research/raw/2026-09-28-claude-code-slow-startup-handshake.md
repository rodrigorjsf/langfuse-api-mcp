# Claude Code's handshake during a slow startup, before and after stdio stops advertising 2026-07-28 (2026-09-28)

This record is for #145 (spec #150). It was run on Linux (WSL2) on 2026-09-28; capture 6 was added on 2026-09-29. Every line is copied from a stdio shim that timestamps each JSON-RPC line in both directions; each line is cut at about 300 characters, and `…` marks a cut. No key appears in any output (fake keys `pk-lf-capture`/`sk-lf-capture`, checked with `grep`).

## Setup

| Item | Value |
|---|---|
| Claude Code | `claude -p "Reply with only the word ok." --mcp-config mcp.json --strict-mcp-config --model haiku --output-format stream-json --verbose --max-turns 1`, with the `CLAUDECODE` variables of the calling session unset. Captures 1 and 6: 2.1.283. Captures 2 to 5: 2.1.284. |
| Server | `go build ./cmd/langfuse-mcp`. Capture 1: `3549afd`. Capture 2: `3d9656a` (before the fix). Captures 3 to 5: `3d9656a` plus the fix, in which the stdio transport reports no protocol from 2026-07-28 as supported. Capture 6: `d83fe3a`, the fix as committed. |
| Shim | A Python script between Claude Code and the server. It writes `<time> host->server <line>` and `<time> server->host <line>` to a log, and forwards each line unchanged. |
| Langfuse | A Python double of Langfuse 4.46.0. `/api/public/health` answers `{"status":"OK","version":"4.46.0"}` after N seconds, and every other GET answers `{"data":[],"meta":{}}`. N = 5 is the "slow" setting: detection then runs its full 5 s budget. N = 0 is the "fast" setting. |

## Result

| # | Claude Code | Server | Health delay | Handshake | `init` status |
|---|---|---|---|---|---|
| 1 | 2.1.283 | before the fix | 5 s | `server/discover`, then `initialize` after 3 s; the `initialize` is refused as a duplicate | `failed` |
| 2 | 2.1.284 | before the fix | 5 s | the same refusal, then Claude Code probes `server/discover` again and connects on 2026-07-28 | `connected` |
| 3 | 2.1.284 | after the fix | 5 s | `server/discover` answered without 2026-07-28; the queued `initialize` succeeds on 2025-11-25 | `connected`, 4 tools |
| 4 | 2.1.284 | after the fix | 0 s | the same, without the wait | `connected`, 4 tools |
| 5 | 2.1.284 | after the fix, write mode | 0 s | a destructive `execute_write` asks through `elicitation/create`; `claude -p` cancels it | `confirmation_declined`, nothing sent |
| 6 | 2.1.283 | after the fix | 5 s | `server/discover` answered without 2026-07-28; the queued `initialize` succeeds on 2025-11-25 | `connected`, 4 tools |

Claude Code does not re-send `initialize`, which was #145's first hypothesis. It probes `server/discover` (protocol 2026-07-28) first. After 3.0 s with no answer, it falls back to one legacy `initialize` on the same pipe. The server reads stdin only after it has detected the deployment profile, so both lines wait in the pipe. go-sdk v1.8.0 `(*Server).discover` stores the probe's parameters on the session, because the stdio transport supports 2026-07-28. `(*ServerSession).initialize` then finds the session initialized and refuses the queued `initialize` as `duplicate "initialize" received`. The limit that matters is Claude Code's 3 s probe timeout, not the 5 s detection budget or the 30 s connection timeout.

After the fix, the stdio transport reports only protocols older than 2026-07-28 as supported. `server/discover` is then answered with `supportedVersions` lacking 2026-07-28, and it stores nothing on the session. The legacy `initialize` succeeds, whether it waited in the pipe or not. The SDK does not refuse the probe with an error: it answers it with the versions stdio offers, and Claude Code falls back to `initialize` because 2026-07-28 is missing.

Claude Code 2.1.284 recovers on its own: capture 2 connects before the fix. It still logs the refused `initialize` first. 2.1.283, the version #145 was found with, does not recover (capture 1), so capture 2 cannot show the fix is what connects a client. Capture 6 does: the same 2.1.283 under the same 5 s delay connects after the fix, and it does not re-probe. The fix does not rely on the client retrying.

## Capture 1: 2.1.283, before the fix, 5 s (from the #145 failure handoff, attempt 2)

```text
09:38:30.375 shim started
09:38:30.440 host->server {"jsonrpc":"2.0","id":"server-discover-probe-1","method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28", … "io.modelcontextprotocol/clientCapabilities":{"roots":{"listChanged":true},"elicitation":{"form":{},"url":{}}}}}}
09:38:33.448 host->server {"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"roots":{"listChanged":true},"elicitation":{}},"clientInfo":{"name":"claude-code",…}},"jsonrpc":"2.0","id":0}
09:38:35.426 server->host {"jsonrpc":"2.0","id":"server-discover-probe-1","result":{"resultType":"complete", … "supportedVersions":["2026-07-28","2025-11-25","2025-06-18","2025-03-26","2024-11-05"],"capabilities":{"logging":{},"tools":{"listChanged":true}}}}
09:38:35.427 server->host {"jsonrpc":"2.0","id":0,"error":{"code":0,"message":"duplicate \"initialize\" received"}}
09:38:35.460 host closed stdin
09:38:35.461 server exited 0
```

`init` event: `mcp_servers: [{'name': 'langfuse', 'status': 'failed'}]`, no `mcp__langfuse__*` tool.

## Capture 2: 2.1.284, before the fix, 5 s

```text
22:51:31.741 host->server {"jsonrpc":"2.0","id":"server-discover-probe-1","method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",…
22:51:34.748 host->server {"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"roots":{"listChanged":true},"elicitation":{}},…},"jsonrpc":"2.0","id":0}
22:51:36.729 server->host {"jsonrpc":"2.0","id":"server-discover-probe-1","result":{"resultType":"complete", … "supportedVersions":["2026-07-28","2025-11-25","2025-06-18","2025-03-26","2024-11-05"],…
22:51:36.729 server->host {"jsonrpc":"2.0","id":0,"error":{"code":0,"message":"duplicate \"initialize\" received"}}
22:51:36.730 host->server {"method":"server/discover","jsonrpc":"2.0","id":1,"params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",…
22:51:36.731 server->host {"jsonrpc":"2.0","id":1,"result":{"resultType":"complete", … "supportedVersions":["2026-07-28",…
22:51:36.740 host->server {"jsonrpc":"2.0","id":"listen:0","method":"subscriptions/listen",…
22:51:36.744 host->server {"method":"tools/list","jsonrpc":"2.0","id":2,"params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",…
22:51:36.745 server->host {"jsonrpc":"2.0","id":2,"result":{"resultType":"complete", … "tools":[…
```

`init` event: `connected`, tools `describe_operation`, `execute_read`, `get_trace_tree`, `search_operations`.

## Capture 3: 2.1.284, after the fix, 5 s

```text
22:54:08.585 host->server {"jsonrpc":"2.0","id":"server-discover-probe-1","method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",…
22:54:11.594 host->server {"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"roots":{"listChanged":true},"elicitation":{}},…},"jsonrpc":"2.0","id":0}
22:54:13.556 server->host {"jsonrpc":"2.0","id":"server-discover-probe-1","result":{"resultType":"complete", … "supportedVersions":["2025-11-25","2025-06-18","2025-03-26","2024-11-05"],"capabilities":{"logging":{},"tools":{"listChanged":true}}}}
22:54:13.557 server->host {"jsonrpc":"2.0","id":0,"result":{"capabilities":{"logging":{},"tools":{"listChanged":true}},"protocolVersion":"2025-11-25","serverInfo":{"name":"langfuse-mcp","version":"0.0.0-dev"}}}
22:54:13.561 host->server {"jsonrpc":"2.0","method":"notifications/initialized"}
22:54:13.561 host->server {"method":"tools/list","jsonrpc":"2.0","id":1}
22:54:13.562 server->host {"jsonrpc":"2.0","id":1,"result":{"ttlMs":0,"cacheScope":"public","tools":[…
```

`init` event: `connected`, tools `describe_operation`, `execute_read`, `get_trace_tree`, `search_operations`.

## Capture 4: 2.1.284, after the fix, 0 s

```text
22:54:18.534 host->server {"jsonrpc":"2.0","id":"server-discover-probe-1","method":"server/discover",…
22:54:18.535 server->host {"jsonrpc":"2.0","id":"server-discover-probe-1","result":{… "supportedVersions":["2025-11-25","2025-06-18","2025-03-26","2024-11-05"],…
22:54:18.606 host->server {"method":"initialize","params":{"protocolVersion":"2025-11-25",…},"jsonrpc":"2.0","id":0}
22:54:18.607 server->host {"jsonrpc":"2.0","id":0,"result":{"capabilities":{"logging":{},"tools":{"listChanged":true}},"protocolVersion":"2025-11-25",…}}
22:54:18.638 host->server {"jsonrpc":"2.0","method":"notifications/initialized"}
22:54:18.639 host->server {"method":"tools/list","jsonrpc":"2.0","id":1}
22:54:18.640 server->host {"jsonrpc":"2.0","id":1,"result":{"ttlMs":0,"cacheScope":"public","tools":[…
```

`init` event: `connected`, the same 4 tools. On a fast startup Claude Code now speaks 2025-11-25 too; before the fix it spoke 2026-07-28 (the #145 fast capture).

## Capture 5: the destructive-call confirmation over the legacy protocol

The server ran with `LANGFUSE_MCP_ALLOW_WRITES=true` against a double that logs every non-GET request. The prompt asked for one `execute_write` call of `prompts_delete` with `promptName` `capture-test`.

```text
22:55:12.495 server->host {"jsonrpc":"2.0","id":0,"result":{…"protocolVersion":"2025-11-25",…}}
22:55:19.619 host->server {"method":"tools/call","params":{"name":"execute_write","arguments":{"operationId":"prompts_delete","parameters":{"promptName":"capture-test"}},…},"jsonrpc":"2.0","id":2}
22:55:19.620 server->host {"jsonrpc":"2.0","id":1,"method":"elicitation/create","params":{"mode":"form","message":"Confirm this destructive change to Langfuse. …Operation: prompts_delete\nMethod: DELETE\nPath parameters:\n  promptName = \"capture-tes…
22:55:19.622 host->server {"result":{"action":"cancel"},"jsonrpc":"2.0","id":1}
22:55:19.622 server->host {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\"error\":{\"code\":\"confirmation_declined\",…
```

The double logged no request other than GETs. The confirmation reaches Claude Code through the SDK's older-protocol path, a server-to-client `elicitation/create`. `claude -p` cannot show a form and cancels it (as in the #142 record), so the call ends in `confirmation_declined` and nothing is sent.

## Capture 6: 2.1.283, after the fix, 5 s

```text
00:05:58.930 shim started
00:05:58.995 host->server {"jsonrpc":"2.0","id":"server-discover-probe-1","method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",…
00:06:02.002 host->server {"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"roots":{"listChanged":true},"elicitation":{}},"clientInfo":{"name":"claude-code","title":"Claude Code","version":"2.1.283",…},"jsonrpc":"2.0","id":0}
00:06:03.985 server->host {"jsonrpc":"2.0","id":"server-discover-probe-1","result":{"resultType":"complete", … "supportedVersions":["2025-11-25","2025-06-18","2025-03-26","2024-11-05"],"capabilities":{"lo…
00:06:03.985 server->host {"jsonrpc":"2.0","id":0,"result":{"capabilities":{"logging":{},"tools":{"listChanged":true}},"protocolVersion":"2025-11-25","serverInfo":{"name":"langfuse-mcp","version":"0.0.0-dev"}}}
00:06:03.989 host->server {"jsonrpc":"2.0","method":"notifications/initialized"}
00:06:03.989 host->server {"method":"tools/list","jsonrpc":"2.0","id":1}
00:06:03.990 server->host {"jsonrpc":"2.0","id":1,"result":{"ttlMs":0,"cacheScope":"public","tools":[…
```

`init` event: `mcp_servers: [{'name': 'langfuse', 'status': 'connected'}]`, tools `describe_operation`, `execute_read`, `get_trace_tree`, `search_operations`. Against capture 1, only the server changed.

## Upstream report draft (option D, not posted)

Spec #150 says the go-sdk issue is posted only after the maintainer approves its text. This is the draft; posting it and reverting the workaround are tracked in #162.

> **Title:** Legacy `initialize` after a `server/discover` probe on the same stdio session is refused as a duplicate
>
> **Version:** go-sdk v1.8.0, `mcp.StdioTransport`.
>
> **What happens.** A client that supports both protocols (Claude Code 2.1.283 and 2.1.284) sends `server/discover` with `_meta` protocol version `2026-07-28` first. If no answer comes within 3 s, it sends a legacy `initialize` (`protocolVersion` `2025-11-25`) on the same pipe. When the server starts reading stdin late (ours runs a few seconds of startup work before `Server.Run`), both requests are queued. `(*Server).discover` stores `InitializeParams` from the probe's `_meta`, because the transport supports 2026-07-28 (`server.go`, the `discover` handler). `(*ServerSession).initialize` then sees `InitializeParams != nil` and answers `{"code":0,"message":"duplicate \"initialize\" received"}`. Claude Code 2.1.283 marks the server failed.
>
> **Expected.** `server/discover` is a probe. A legacy `initialize` that follows it on the same session should negotiate the legacy protocol as if the probe had not run, or at least not be refused as a duplicate of an `initialize` that never happened.
>
> **Reproduction.** Connect a server over `StdioTransport`. Before `Server.Run` reads, write `{"jsonrpc":"2.0","id":"p","method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}` and then `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`. The second answer is the duplicate error.
>
> **Workaround in use.** A stdio transport implementing `ProtocolVersionSupporter` that rejects `2026-07-28`, so `discover` stores nothing. It costs the sessionless protocol on stdio.
