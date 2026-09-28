# Claude Code loads the user skill and completes the M6 workflows against a local Langfuse (2026-09-28)

This is the M6 exit proof 3 of spec #132 (ticket #140), run on Linux (WSL2) on 2026-09-28 between 06:15 and 06:25 UTC. Tool calls are copied verbatim from Claude Code's `stream-json` output, and from the session transcript for the interactive run. The scratch path is shown as `<project>`. No key appears in any output.

## Setup

| Item | Value |
|---|---|
| Claude Code | 2.1.283. Model `claude-sonnet-5` (`--model sonnet`), the maintainer's own user settings and plugins. Langfuse's own `langfuse` skill is installed in `~/.claude/skills` beside this one. |
| Langfuse | Local self-hosted 4.46.0 `events_only` (`scripts/langfuse-selfhosted.sh up`), fresh project keys |
| Server | `go build ./cmd/langfuse-mcp` at `eaaccb9`, registered in a project `.mcp.json` with `--mcp-config .mcp.json --strict-mcp-config`. Keys come in through `${LANGFUSE_*}` expansion, with `LANGFUSE_MCP_ALLOW_WRITES=true`. |
| Permissions | `--allowedTools` for the langfuse tools, `Skill` and `Read`. Tool permission prompts therefore never appear, and the server's confirmation form is the only prompt. |
| Skill | Installed by `npx skills add` (below), byte-identical to `skills/langfuse-api-mcp/` on the #140 branch |

**Data.** A throwaway script seeded the data through the public API: OTLP/HTTP JSON with `x-langfuse-ingestion-version: 4`, `POST /v2/prompts`, `POST /v2/datasets`, `POST /dataset-items` and `POST /scores`. The experiments were sent as OTel spans with the `langfuse.experiment.*` attributes (Langfuse docs, "Ingest experiment spans with OpenTelemetry").

- **Traces.**
  - Background traffic: 11 cheap `gpt-4o-mini` traces over 24 hours.
  - Session `sess-ana-42` of `user-ana`: three traces, the second holding a `llm-call` generation at level `ERROR` ("upstream model timeout").
  - A cost spike: six `gpt-4o` calls of $1.75 each within one hour, five hours before the run.
- **Prompts.** `support-reply` v1 (`production`) and v2 (`staging`).
- **Experiments.** Dataset `faq-eval` with 3 items, and two experiments on it, `faq-baseline` and `faq-candidate`. The candidate answers "Can I change my delivery address?" wrongly, with `accuracy` 0 instead of 1.

The requests are natural requests. None names the skill, a slash command or a tool. Claude Code answered in Brazilian Portuguese where the maintainer's global `CLAUDE.md` asks for it; the answers are quoted as given.

## 1. Install with `npx skills add`

```console
$ npx -y skills@1.5.18 add rodrigorjsf/langfuse-api-mcp --list
◇  Source: https://github.com/rodrigorjsf/langfuse-api-mcp.git
◇  Repository cloned
◇  No skills found
└  No valid skills found. Skills require a SKILL.md with name and description.

$ npx -y skills@1.5.18 add <#140 worktree> --agent claude-code --copy -y
◇  Installation complete
◇  Installed 1 skill
│  ✓ langfuse-api-mcp (copied)
│    → ./.claude/skills/langfuse-api-mcp
└  Done!  Review skills before use; they run with full agent permissions.

$ find .claude -type f | sort
.claude/skills/langfuse-api-mcp/SKILL.md
.claude/skills/langfuse-api-mcp/references/cost-latency.md
.claude/skills/langfuse-api-mcp/references/errors.md
.claude/skills/langfuse-api-mcp/references/experiments.md
.claude/skills/langfuse-api-mcp/references/prompts.md
.claude/skills/langfuse-api-mcp/references/scores.md
.claude/skills/langfuse-api-mcp/references/traces.md

$ diff -r .claude/skills/langfuse-api-mcp <#140 worktree>/skills/langfuse-api-mcp && echo IDENTICAL
IDENTICAL
```

- **The repository form** still clones `main`, which has no skill until spec #132 merges (as in `2026-09-28-npx-skills-add.md`). The local-path form installs the same files the repository form will install after the merge.
- **After the merge,** re-run `npx skills add rodrigorjsf/langfuse-api-mcp --agent claude-code -y` once and append its output here.
- **Spinner frames** were removed from the output above.

## 2. Results

| Workflow | Request | Skill loaded on its own | Reference read | Completed |
|---|---|---|---|---|
| trace | "In our Langfuse project, user-ana had a bad experience in session sess-ana-42 today. What went wrong in that session? Show me the step that failed." | **yes** | `traces.md` | **yes**: found the `llm-call` ERROR generation ("upstream model timeout", 30 s) in trace `e3912aba…` |
| cost | "Our LLM spend in Langfuse jumped at some point in the last 24 hours. When did it happen and what caused it?" | **yes** | `cost-latency.md` | **yes**: 01:00 UTC bucket, $10.50 on `gpt-4o`, six calls of `user-carla` |
| experiments | "Compare the two most recent experiments on our faq-eval dataset in Langfuse. Did the newer one regress on any item, and what did the app do differently there?" | **yes** | `experiments.md` | **yes**: the regressed item is "Can I change my delivery address?" (1 → 0), and both item traces were opened with `get_trace_tree` |
| prompt, read (2 runs) | "What does the production version of our support-reply prompt in Langfuse say, and how is the staging version different?" | **no** (0/2) | — | **yes**: v1 `production` and v2 `staging` fetched by label and compared |
| prompt, promote (interactive) | "Promote version 2 of our support-reply prompt to production in Langfuse." | **no** | — | **yes**: `execute_write` `promptVersion_update` ran after the confirmation form was **accepted**. v2 then carries `production`, and the model re-read the prompt to check. |

Cost as reported by `claude -p` (`total_cost_usd`): trace $0.197, cost $0.231, experiments $0.250, prompt read $0.155 and $0.039. The interactive run shows no dollar figure: `/cost` shows only plan usage.

**The gap: the prompt workflow completes, but the skill does not load for it.** Follow-up #142.
- The other three workflows load the skill on the first turn. In every session the model chose `langfuse-api-mcp` over Langfuse's own `langfuse` skill, which was also offered.
- For the prompt requests, the model went straight to the tools. It still followed the discovery path the skill teaches: `describe_operation` before the first call, the body taken from `describe_operation promptVersion_update`, and the `staging` label kept.

## 3. Tool calls, verbatim

```text
## trace
init: model claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
  ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read,mcp__langfuse__get_trace_tree", "max_results": 4}
  Skill {"skill": "langfuse-api-mcp"}
  Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/traces.md"}
  mcp__langfuse__search_operations {"query": "trace"}
  mcp__langfuse__describe_operation {"operationId": "observations_getMany"}
  mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"sessionId": "sess-ana-42", "userId": "user-ana", "fromStartTime": "2026-09-28T00:00:00Z", "toStartTime": "2026-09-29T00:00:00Z", "limit": 100, "fields": "core,basic,io"}}
result: success turns 8 cost_usd 0.19682039999999998 duration_ms 27116
## cost
init: model claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
  ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read,mcp__langfuse__get_trace_tree", "max_results": 5}
  Skill {"skill": "langfuse-api-mcp"}
  Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/cost-latency.md"}
  mcp__langfuse__describe_operation {"operationId": "metrics_metrics"}
  mcp__langfuse__execute_read {"operationId": "metrics_metrics", "parameters": {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"dimensions\":[{\"field\":\"providedModelName\"}],\"timeDimension\":{\"granularity\":\"hour\"},\"fromTimestamp\":\"2026-09-27T07:00:00Z\",\"toTimestamp\":\"2026-09-28T07:00:00Z\",\"config\":{\"row_limit\":1000}}"}}
  mcp__langfuse__describe_operation {"operationId": "observations_getMany"}
  mcp__langfuse__execute_read {"operationId": "observations_getMany", "parameters": {"fromStartTime": "2026-09-28T01:00:00Z", "toStartTime": "2026-09-28T02:00:00Z", "fields": "core,basic,usage,model,metrics", "limit": 20, "filter": "[{\"type\":\"string\",\"column\":\"providedModelName\",\"operator\":\"=\",\"value\":\"gpt-4o\"}]"}}
result: success turns 9 cost_usd 0.2311156 duration_ms 26988
## experiments
init: model claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
  ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read,mcp__langfuse__get_trace_tree", "max_results": 5}
  Skill {"skill": "langfuse-api-mcp"}
  Read {"file_path": "<project>/.claude/skills/langfuse-api-mcp/references/experiments.md"}
  mcp__langfuse__search_operations {"query": "dataset run"}
  mcp__langfuse__describe_operation {"operationId": "datasets_get"}
  mcp__langfuse__describe_operation {"operationId": "experiments_list"}
  mcp__langfuse__describe_operation {"operationId": "experiments_listItems"}
  mcp__langfuse__execute_read {"operationId": "datasets_get", "parameters": {"datasetName": "faq-eval"}}
  mcp__langfuse__execute_read {"operationId": "experiments_list", "parameters": {"datasetId": "cmukuxfxr0005o907xphwnez5", "fromStartTime": "2026-09-01T00:00:00Z"}}
  mcp__langfuse__execute_read {"operationId": "experiments_listItems", "parameters": {"experimentId": "b2193c82004f59f5", "fromStartTime": "2026-09-28T00:00:00Z", "fields": "io,scores"}}
  mcp__langfuse__execute_read {"operationId": "experiments_listItems", "parameters": {"experimentId": "5cf9dfdf2b93a2fe", "fromStartTime": "2026-09-28T00:00:00Z", "fields": "io,scores"}}
  mcp__langfuse__get_trace_tree {"traceId": "dc426db6d467a21af078969d00b7491c", "include": ["io", "metadata"]}
  mcp__langfuse__get_trace_tree {"traceId": "7aa4c9e8f4a3720470e89447a0cbba92", "include": ["io", "metadata"]}
result: success turns 15 cost_usd 0.24987159999999997 duration_ms 40407
## prompt-read
init: model claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
  ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read", "max_results": 5}
  mcp__langfuse__describe_operation {"operationId": "prompts_get"}
  mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
  mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "staging"}}
result: success turns 5 cost_usd 0.15456699999999998 duration_ms 11958
## prompt-read-2
init: model claude-sonnet-5 | mcp [('langfuse', 'connected')] | langfuse skills: ['langfuse', 'langfuse-api-mcp']
  ToolSearch {"query": "select:mcp__langfuse__search_operations,mcp__langfuse__describe_operation,mcp__langfuse__execute_read", "max_results": 5}
  mcp__langfuse__describe_operation {"operationId": "prompts_get"}
  mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
  mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "staging"}}
result: success turns 5 cost_usd 0.0393406 duration_ms 10172
```

The cost run's metrics query and the observations filter are cut above at 400 characters by the extraction script. They were a `totalCost` sum per hour by `providedModelName` over 24 hours, then `observations_getMany` for the spike hour filtered to `gpt-4o`.

### Prompt promote, interactive (tmux), transcript tool calls

```text
ToolSearch {"query": "langfuse prompt", "max_results": 10}
mcp__langfuse__search_operations {"query": "prompt"}
mcp__langfuse__execute_read {"operationId": "prompts_list"}
mcp__langfuse__describe_operation {"operationId": "promptVersion_update"}
mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "version": 2}}
mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "version": 1}}
mcp__langfuse__execute_write {"operationId": "promptVersion_update", "parameters": {"name": "support-reply", "version": 2}, "body": {"newLabels": ["production", "staging"]}}
mcp__langfuse__execute_read {"operationId": "prompts_get", "parameters": {"promptName": "support-reply", "label": "production"}}
```

The server's confirmation, as Claude Code's MCP log received it (`elicitation/create`, form mode, empty `requestedSchema`):

```text
Confirm this destructive change to Langfuse. The values below come from the call's arguments and are shown as quoted text.

Operation: promptVersion_update
Method: PATCH
Path parameters:
  name = "support-reply"
  version = 2
Body (38 bytes): {"newLabels":["production","staging"]}
```

On screen, Claude Code shows `MCP server “langfuse” requests your input`, then the text above collapsed to `… (+6 more lines)`, then `❯ Accept    Decline`. Enter was pressed on Accept. Claude Code's MCP log then records `Elicitation response: {"action":"accept","content":{}}`.

The `execute_write` result came back inside the untrusted-data envelope, with `"labels":["production","staging","latest"]` for v2. The final answer was:

```text
● Versão 2 de support-reply agora é production. Confirmei: buscar pelo label production retorna a v2 ("...at most two sentences, and always offer a follow-up."). A v1 perdeu o label, porque labels são
  únicos entre versões. A v2 manteve staging e latest.
  Para reverter, aplique ["production"] na v1 com promptVersion_update.
```

## 4. Answers, verbatim

```text
## trace
Falhou o step `llm-call` no trace `e3912aba988f9faf9dafc282b2d6e0ba`, às 04:54 UTC de 2026-09-28.

**Step que falhou**
- Observation `59e427577d13b8ba`, tipo `GENERATION`, nome `llm-call`.
- `level`: `ERROR`
- `statusMessage`: `upstream model timeout`
- Durou 30s, de 04:54:12.2 a 04:54:42.2. Os outros `llm-call` da sessão levaram cerca de 2.5s, então 30s indica timeout.
- Pai: span raiz `support-chat` (`2ec5b4d5b81fe0ad`). O usuário mandou `"Order 1234"` e o output ficou `""`, sem resposta.

**Sessão `sess-ana-42` em ordem**

| Hora (UTC) | Input | Output | Estado |
|---|---|---|---|
| 04:49 | "I want a refund" | "Sure, which order?" | ok |
| 04:54 | "Order 1234" | vazio | **`llm-call` com timeout** |
| 04:59 | "Order 1234, again" | "Refund started for order 1234." | ok |

**Leitura**
- Ana informou o pedido e ficou sem resposta.
- Ela reenviou 5 minutos depois e o reembolso só começou na segunda tentativa.
- O span raiz do trace com falha ficou em `DEFAULT`, sem `ERROR`. O erro aparece só no filho, então alertas baseados no span raiz não pegam esse caso.

**Limites**
- Não abri a árvore completa com `get_trace_tree`. Só há dois spans por trace, e a listagem já mostra o erro.
- Não busquei scores nem feedback explícito da Ana.
- A causa do timeout está fora do que a Langfuse mostra. Precisa olhar o provedor do modelo por volta de 04:54 UTC.
## experiments
Sim, regrediu em 1 dos 3 itens. Os outros dois ficaram iguais.

Os dois experimentos são `faq-baseline` (`5cf9dfdf2b93a2fe`) e `faq-candidate` (`b2193c82004f59f5`, o mais novo). Os scores de `accuracy` foram:

| Item | baseline | candidate |
|---|---|---|
| Do you ship abroad? | 1 | 1 |
| How do I reset my password? | 1 | 1 |
| Can I change my delivery address? | 1 | **0** |

Nos itens 1 e 2 a saída também foi idêntica.

**Item regredido: "Can I change my delivery address?"**
- Esperado: `{"answer": "Yes, until the order ships."}`
- baseline: `Yes, until the order ships.` (trace `7aa4c9e8f4a3720470e89447a0cbba92`)
- candidate: `No, addresses cannot be changed.` (trace `dc426db6d467a21af078969d00b7491c`)

**O que o app fez de diferente:** nos traces não aparece nenhuma diferença de execução, só o texto da resposta. Os dois traces têm a mesma forma:
- Um span raiz `experiment-item` com um único filho `llm-call`.
- Mesmo modelo, `gpt-4o-mini`.
- Mesmos tokens (300 de entrada, 40 de saída), mesmo custo (0.0005) e mesma latência (1.5 s).
- Sem erros.

O `llm-call` não tem `input` nem `output` gravados, e os `modelParameters` vêm vazios. Por isso não dá para ver o prompt nem os parâmetros do candidate. O modelo respondeu o contrário do esperado (nega a mudança de endereço), mas os traces não mostram a causa.

Para achar a causa, a próxima coisa a comparar seria a versão do prompt ou da config entre as duas execuções, se o app registrar isso fora do trace. Não fiz essa comparação.
## prompt-read
**Production** (v1):
> You are a support agent. Answer politely in one sentence.

**Staging** (v2, também com label `latest`):
> You are a support agent. Answer politely in at most two sentences, and always offer a follow-up.

Diferenças:
- Limite de tamanho: exatamente uma frase no production, no máximo duas no staging.
- O staging exige oferecer um follow-up sempre. O production não diz nada sobre isso.
- Tom (polido) e papel (support agent) são iguais nos dois.

Nenhum dos dois tem `commitMessage` nem `config`, então não há nota explicando a mudança.
## cost
Spend jumped in the hour starting **2026-09-28 01:00 UTC** (22:00 on 09-27 in GMT-3).

**What happened**
- Baseline before that hour was about $0.002/hr, all `gpt-4o-mini`.
- The 01:00 UTC bucket shows **$10.50 on `gpt-4o`**. That model has no cost in any earlier hour of the window.
- After that, cost went back to `gpt-4o-mini` only: $0.002 at 02:00, $0.0015 at 03:00, and $0.0125 at 04:00. The 04:00 figure is about 6x baseline. I did not investigate it.

**Cause**
- The listing for that hour holds 6 `llm-call` generations, all on `gpt-4o` in `production`, all from `user-carla`.
- The sessions are `sess-carla-0` through `sess-carla-4`, and there is a sixth observation on `sess-carla-5`.
- They start about every 8 minutes, from 01:19 to 01:59.
- Each call used 60,000 input and 15,000 output tokens, which is 75,000 total, for $1.75 per call.
- 6 × $1.75 = $10.50, which matches the bucket total.
- The calls are large and identical, and fire on a fixed cadence. That looks like a script or agent loop, not organic traffic.
- Each call has a different parent observation and trace ID, so the calls are not sub-steps of one run.

**Caveats**
- The listing stopped at 6 rows out of a 20-row limit, so nothing was cut off.
- I have not opened the traces, so I don't know what prompt filled 60k input tokens, or why this user's app used `gpt-4o` instead of `gpt-4o-mini`. To find out, run `get_trace_tree` with `include: ["io"]` on trace `a1f7ba9459aa87350a44ab583ac2d9de`, or on the other five. Say so if you want me to.
- I used one metrics query, since the daily budget is small. The window was 09-27 07:00 UTC to 09-28 07:00 UTC, which stands in for "last 24h".
```

## What this does not prove

- Other models (Haiku, Opus) and other MCP clients were not run.
- The repository form of `npx skills add` was not run after the merge; it is to be appended after the merge.
- The trace workflow did not call `get_trace_tree`. The session listing already held the failing observation, and the model said so. The experiments run exercised `get_trace_tree`.
