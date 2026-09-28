# Claude Code loads the user skill for prompt requests after the description names the tool call (2026-09-28)

This record is for #142, the follow-up to manual proof 3 of spec #132 ([record](2026-09-28-claude-code-skill-run.md)). It was run on Linux (WSL2) on 2026-09-28. Tool calls are copied from Claude Code's `stream-json` output, with each call's input cut at 300 characters. The scratch path is shown as `<project>`. No key appears in any output.

## Setup

The setup is that of the #140 record, with these differences:

| Item | Value |
|---|---|
| Claude Code | 2.1.283, model `claude-sonnet-5` (`--model sonnet`), the maintainer's own user settings and plugins. Langfuse's own `langfuse` skill is installed in `~/.claude/skills`, and every run's `init` event lists both `langfuse` and `langfuse-api-mcp`. The `CLAUDE*` variables of the calling session were unset for each run. |
| Langfuse | Local self-hosted 4.46.0 `events_only` (`scripts/langfuse-selfhosted.sh up`), fresh project keys |
| Server | `go build ./cmd/langfuse-mcp` at `5160f4c`, registered in a project `.mcp.json` through `--mcp-config .mcp.json --strict-mcp-config`, with `LANGFUSE_MCP_ALLOW_WRITES=true` |
| Permissions | `--allowedTools` for the five langfuse tools, `Skill` and `Read` |
| Skill | Installed by `npx -y skills@1.5.18 add <checkout> --agent claude-code --copy -y`. After each description change, the new `SKILL.md` was copied in, and `diff -r` against `skills/langfuse-api-mcp/` printed nothing. |
| Data | Only the prompts were seeded: `support-reply` v1 (`production`) and v2 (`staging`), through `POST /v2/prompts`. The labels were reset after every promote run. The trace, cost and experiment requests found no data, which does not matter here: the `Skill` call comes before the first data read. |

**Non-interactive promote.** `claude -p` answers the server's confirmation form with a decline. Every promote run therefore ends in `confirmation_declined`, and nothing is sent to Langfuse. Only the `Skill` call is measured here. The accepted promotion is in the #140 record.

## Results

| Description | Prompt read | Prompt promote | Trace | Cost | Experiments |
|---|---|---|---|---|---|
| A: the #140 text | 0 of 1 | 0 of 1 (+1 invalid run) | (#140: loads) | (#140: loads) | (#140: loads) |
| B: names prompt actions | 0 of 2 | not run | not run | not run | not run |
| C: "Use for every Langfuse request" | 0 of 1 | not run | not run | not run | not run |
| **D: "Load before any langfuse-api-mcp tool call"** (committed) | **3 of 3** (1 trial, then 2 measured) | **2 of 2** | **1 of 1** | **1 of 1** | **1 of 1** |

The descriptions tried:

- **A** (#140, 194 characters): `Investigate Langfuse traces, sessions, scores, prompts, datasets and metrics with the langfuse-api-mcp tools (search_operations, describe_operation, execute_read, get_trace_tree, execute_write).`
- **B** (200): `Langfuse traces, sessions, scores, cost, experiments; fetch, compare or promote prompts via langfuse-api-mcp tools (search_operations, describe_operation, execute_read, get_trace_tree, execute_write).`
- **C** (199): `Use for every Langfuse request (traces, sessions, scores, cost, experiments, prompts) with langfuse-api-mcp tools (search_operations, describe_operation, execute_read, get_trace_tree, execute_write).`
- **D** (195): `Load before any langfuse-api-mcp tool call (search_operations, describe_operation, execute_read, get_trace_tree, execute_write) on Langfuse traces, sessions, scores, cost, experiments or prompts.`

**What moved the trigger.** Naming the prompt actions (B) or claiming every Langfuse request (C) did not. With both, the model still went straight to `describe_operation prompts_get`. D ties the skill to the moment the model is about to call one of the server's tools, and the model then called `Skill` first in every run. It then read `references/prompts.md` for both prompt requests. In every D run the model still chose `langfuse-api-mcp` over Langfuse's own `langfuse` skill.

**Invalid run.** `base-promote-1` is left out of the counts. The server did not connect (`duplicate "initialize" received`, #145), so the model loaded Langfuse's own `langfuse` skill and moved the label with `npx langfuse-cli` through Bash, using the keys exported for `.mcp.json`. The server's confirmation was never involved; this is #146. The labels were reset by hand before the next run.

**YAML guard.** B's first drafts held `": "`, which ends a YAML plain scalar, so a real loader would reject or cut the description. The hand-rolled parser of `scripts/check-skill.py` did not notice. The check now fails on an unquoted `": "` or `" #"` in the description.

## Tool calls

The runs are listed in the order they ran. `base-*` used A, `new-*` B, `v2-*` C, and `v3-*` and `fin-*` D.

```text
## base-read-1
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   ToolSearch {"query": "select:mcp__langfuse__execute_read,mcp__langfuse__search_operations,mcp__langfuse__describe_operation", "max_results": 3}
   mcp__langfuse__describe_operation {"operationId": "prompts_get"}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "staging"}}
result: success turns 5 cost_usd 0.164628
## base-promote-1
init: claude-sonnet-5 | mcp [('langfuse', 'failed')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   Skill {"skill": "langfuse"}
   Bash {"command": "env | grep -c '^LANGFUSE_' ; npx langfuse-cli api prompts --help 2>&1 | head -30"}
   Bash {"command": "npx langfuse-cli api prompts get --help 2>&1 | head -20; npx langfuse-cli api prompts get support-reply --label production 2>&1 | head -40"}
   Bash {"command": "npx langfuse-cli api prompts update-version --help 2>&1 | sed -n 1,20p"}
   Bash {"command": "npx langfuse-cli api prompts get support-reply --version 2 2>&1 | grep -A4 labels; npx langfuse-cli api prompts update-version support-reply 2 --body-json '{\"newLabels\":[\"production\"]}' 2>&1 | grep -A5 labels; npx langfuse-cli api prompts get support-reply --label production 2>&1 | 
result: success turns 7 cost_usd 0.1892932
## base-promote-2
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read,mcp__langfuse__execute_write", "max_results": 4}
   mcp__langfuse__describe_operation {"operationId": "prompts_get"}
   mcp__langfuse__search_operations {"query": "prompt"}
   mcp__langfuse__execute_read {"operationId": "prompts_list", "parameters": {"name": "support-reply"}}
   mcp__langfuse__describe_operation {"operationId": "promptVersion_update"}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "version": 2}}
   mcp__langfuse__execute_write {"operationId": "promptVersion_update", "parameters": {"name": "support-reply", "version": 2}, "body": {"newLabels": ["production", "staging"]}}
result: success turns 9 cost_usd 0.19993240000000004
## new-read-1
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read", "max_results": 5}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "staging"}}
result: success turns 4 cost_usd 0.1464584
## new-read-2
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read", "max_results": 5}
   mcp__langfuse__describe_operation {"operationId": "prompts_get"}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "staging"}}
result: success turns 5 cost_usd 0.1568928
## v2-read-1
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read", "max_results": 5}
   mcp__langfuse__describe_operation {"operationId": "prompts_get"}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "staging"}}
result: success turns 5 cost_usd 0.1573482
## v3-read-1
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   Skill {"skill": "langfuse-api-mcp"}
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read", "max_results": 5}
   Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/prompts.md"}
   mcp__langfuse__describe_operation {"operationId": "prompts_get"}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "staging"}}
result: success turns 8 cost_usd 0.1679682
## fin-read-1
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   Skill {"skill": "langfuse-api-mcp"}
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read", "max_results": 5}
   Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/prompts.md"}
   mcp__langfuse__describe_operation {"operationId": "prompts_get"}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "staging"}}
result: success turns 8 cost_usd 0.16794699999999999
## fin-read-2
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   Skill {"skill": "langfuse-api-mcp"}
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read", "max_results": 5}
   Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/prompts.md"}
   mcp__langfuse__describe_operation {"operationId": "prompts_get"}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "staging"}}
result: success turns 8 cost_usd 0.060012800000000005
## fin-promote-1
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   Skill {"skill": "langfuse-api-mcp"}
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read,mcp__langfuse__execute_write", "max_results": 4}
   Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/prompts.md"}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "version": 2}}
   mcp__langfuse__describe_operation {"operationId": "promptVersion_update"}
   mcp__langfuse__execute_write {"operationId": "promptVersion_update", "parameters": {"name": "support-reply", "version": 2}, "body": {"newLabels": ["production", "staging"]}}
result: success turns 9 cost_usd 0.18448739999999997
## fin-promote-2
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   Skill {"skill": "langfuse-api-mcp"}
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read,mcp__langfuse__execute_write", "max_results": 4}
   Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/prompts.md"}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
   mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "version": 2}}
   mcp__langfuse__describe_operation {"operationId": "promptVersion_update"}
   mcp__langfuse__execute_write {"operationId": "promptVersion_update", "parameters": {"name": "support-reply", "version": 2}, "body": {"newLabels": ["production", "staging"]}}
result: success turns 9 cost_usd 0.18446479999999998
## fin-trace
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   Skill {"skill": "langfuse-api-mcp"}
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read,mcp__langfuse__get_trace_tree", "max_results": 5}
   Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/traces.md"}
   mcp__langfuse__describe_operation {"operationId": "observations_getMany"}
   mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"sessionId": "sess-ana-42", "userId": "user-ana", "fromStartTime": "2026-09-28T00:00:00Z", "toStartTime": "2026-09-29T00:00:00Z", "limit": 100}}
   mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"sessionId": "sess-ana-42", "fromStartTime": "2026-09-27T00:00:00Z", "toStartTime": "2026-09-29T12:00:00Z", "limit": 100}}
   mcp__langfuse__search_operations {"query": "trace"}
   mcp__langfuse__search_operations {"query": "session"}
   mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"userId": "user-ana", "fromStartTime": "2026-09-27T00:00:00Z", "toStartTime": "2026-09-29T12:00:00Z", "limit": 100}}
   mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"level": "ERROR", "fromStartTime": "2026-09-27T00:00:00Z", "toStartTime": "2026-09-29T12:00:00Z", "limit": 100}}
   mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"limit": 5}}
result: success turns 13 cost_usd 0.2454488
## fin-cost
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   Skill {"skill": "langfuse-api-mcp"}
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read,mcp__langfuse__get_trace_tree", "max_results": 5}
   Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/cost-latency.md"}
   mcp__langfuse__describe_operation {"operationId": "metrics_metrics"}
   mcp__langfuse__execute_read {"operationId": "metrics_metrics", "parameters": {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"dimensions\":[{\"field\":\"providedModelName\"}],\"timeDimension\":{\"granularity\":\"hour\"},\"fromTimestamp\":\"2026-09-27T06:00:00Z\",\"toTime
   mcp__langfuse__describe_operation {"operationId": "observations_getMany"}
   mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"fromStartTime": "2026-09-27T06:00:00Z", "toStartTime": "2026-09-28T09:00:00Z", "type": "GENERATION", "fields": "core,basic,usage", "limit": 100}}
   mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"fromStartTime": "2026-09-27T06:00:00Z", "toStartTime": "2026-09-28T09:00:00Z", "limit": 20}}
   mcp__langfuse__execute_read {"operationId": "trace_list", "parameters": {"fromTimestamp": "2026-09-27T06:00:00Z", "toTimestamp": "2026-09-28T09:00:00Z", "limit": 20}}
   mcp__langfuse__search_operations {"query": "trace"}
   mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"fromStartTime": "2026-09-21T00:00:00Z", "toStartTime": "2026-09-29T00:00:00Z", "limit": 5}}
result: success turns 13 cost_usd 0.2692222
## fin-experiments
init: claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
   Skill {"skill": "langfuse-api-mcp"}
   ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read,mcp__langfuse__get_trace_tree", "max_results": 5}
   Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/experiments.md"}
   mcp__langfuse__describe_operation {"operationId": "datasets_get"}
   mcp__langfuse__describe_operation {"operationId": "experiments_list"}
   mcp__langfuse__describe_operation {"operationId": "experiments_listItems"}
   mcp__langfuse__execute_read {"operationId": "datasets_get", "parameters": {"datasetName": "faq-eval"}}
   mcp__langfuse__search_operations {"query": "dataset"}
   mcp__langfuse__execute_read {"operationId": "datasets_list"}
   mcp__langfuse__execute_read {"operationId": "experiments_list", "parameters": {"fromStartTime": "2026-01-01T00:00:00Z"}}
result: success turns 12 cost_usd 0.2149012
```

Every `fin-promote-*` `execute_write` came back as `confirmation_declined` ("the user declined the change and nothing was sent to Langfuse"), and so did `base-promote-2`.

## What this does not prove

- Only Sonnet was run. Haiku and Opus may trigger differently.
- Trace, cost and experiments were run once each with D, against a project without their data. #140 has the full workflows.
- The small-model eval A/B appends the skill's text, so it does not exercise the description.
