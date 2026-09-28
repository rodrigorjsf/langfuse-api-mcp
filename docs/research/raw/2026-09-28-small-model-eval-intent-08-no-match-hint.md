# Small-model eval after the #114 no-match hint fix, local qwen3:8b (2026-09-28)

Verbatim output of `scripts/small-model-eval-local.sh` (#114), with container, image pull and model pull lines
removed. Same model, Ollama image (`0.34.4`, KV cache f16) and settings as
[2026-09-27-small-model-eval-local-model-vram.md](2026-09-27-small-model-eval-local-model-vram.md)
(temperature 0, `--max-tokens 4096 --timeout 600`). Branch `spec/132-spec-m6-user-skill-langfuse-api-mcp-skil--114`.

What changed: a `search_operations` query that matches nothing and does not hold the word `trace` or `traces`
no longer carries the trace reads hint; its result ends with the tag list and the search-again text. A query
holding `trace`/`traces` keeps the hint, with or without matches (#100). Run 2 onwards also carries a reworded
tool description ("When the query holds the word trace or traces, a hint names…").

## Summary

| Run | Code | 08 | 09 | 11 | Other |
|---|---|---|---|---|---|
| 0 | base `fadc725` (before the fix) | FAIL: searched "billing", "Prompts billing", "Prompts", then ended | — | PASS | — |
| 1 | `ce6a26f` | FAIL: searched again, then `prompts_list {}`, then ended | — | FAIL: ended after two searches | — |
| 2 | `ce6a26f` + description rewording | FAIL: searched again, described `prompts_list`, sent `filter` instead of `tag` | — | PASS | — |
| 3 | same as 2, full run | **PASS** | FAIL | FAIL (over-escaped query, #106 shape) | 7/11: 01, 04, 05, 06, 07, 08, 10 pass; 02, 03 fail |
| 4 | same as 2 | FAIL: same as run 2 | PASS | PASS | — |

- **Intent 08:** after the fix, qwen3:8b searched again after the "billing" no-match in every run (4/4) and reached
  `prompts_list` in every run (4/4). The acceptance criterion (a deterministic `PASS 08`) is **not met**: it passed
  1 run in 4. The failures left are the wrong parameter (`filter`, `name` or none instead of `tag`), the
  parameter-guessing gap spec #132 gives to the user skill (describe, then execute; never guess a parameter).
- **Intents 09 and 11 are not deterministic on this host at temperature 0.** Each passed in one run of the same
  code and failed in another. The fix cannot reach them: intent 11's query ("cost trace", "cost day trace") holds
  `trace`, so it gets the same hint as before, and intent 09's query ("dataset") matches, and a matching query not
  about traces had no hint before the fix either.
- The run date is 2026-09-28 here, 2026-09-27 in the earlier runs (#105 puts it in the prompt).

## Run 0 — base `fadc725`, `--only 8,11`

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "Prompts billing"}
       search_operations {"query": "Prompts"}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
PASS 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost trace"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"filters\":[{\"column\":\"traceName\",\"operator\":\"=\",\"value\":\"checkout\",\"type\":\"string\"}],\"timeDimension\":{\"granularity\":\"day\"},\"fromTimestamp\":\"2026-09-21T00:00:00Z\",\"toTimestamp\":\"2026-09-28T00:00:00Z\"}"}
TOTAL 1/2 passed
```

## Run 1 — `ce6a26f`, `--only 8,11`

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "Prompts"}
       execute_read prompts_list {}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost day"}
       search_operations {"query": "Metrics cost"}
       ended: no tool call; stop_reason end_turn
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day", "last_days": 7}
TOTAL 0/2 passed
```

## Run 2 — description reworded, `--only 8,11`

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "Prompts"}
       describe_operation {"operationId": "prompts_list"}
       execute_read prompts_list {"filter": "billing"}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
PASS 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost trace"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"filters\":[{\"column\":\"traceName\",\"operator\":\"=\",\"value\":\"checkout\",\"type\":\"string\"}],\"timeDimension\":{\"granularity\":\"day\"},\"fromTimestamp\":\"2026-09-21T00:00:00Z\",\"toTimestamp\":\"2026-09-28T00:00:00Z\"}"}
TOTAL 1/2 passed
```

## Run 3 — description reworded, full run

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
PASS 01 [traces] Show me everything that happened inside trace 7f3c2a91-checkout, as a tree of its observations.
       get_trace_tree {"include": ["io", "metadata"], "traceId": "7f3c2a91-checkout"}
FAIL 02 [observations] List the generation observations named 'summarize-ticket'.
       search_operations {"query": "generation summarize-ticket"}
       get_trace_tree {"include": ["io"], "traceId": "summarize-ticket"}
       execute_read observations_getMany {"name": "summarize-ticket"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"name": "summarize-ticket", "type": "GENERATION"}
FAIL 03 [observations] Which observations belong to session sess-2024-17?
       get_trace_tree {"include": [], "traceId": "sess-2024-17"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"sessionId": "sess-2024-17"}
PASS 04 [observations] Find the observations with level ERROR for user u-5521.
       search_operations {"query": "error trace"}
       execute_read observations_getMany {"level": "ERROR", "user": "u-5521"}
       execute_read observations_getMany {"level": "ERROR", "userId": "u-5521"}
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       search_operations {"query": "score"}
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
PASS 06 [scores] Which score configs are defined in this project?
       search_operations {"query": "score config"}
       execute_read scoreConfigs_get {}
PASS 07 [prompts] Fetch the prompt 'support-reply' with the label production.
       search_operations {"query": "prompt get"}
       describe_operation {"operationId": "prompts_get"}
       execute_read prompts_get {"label": "production", "promptName": "support-reply"}
PASS 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "prompts"}
       execute_read prompts_list {"name": "billing"}
       describe_operation {"operationId": "prompts_list"}
       execute_read prompts_list {"tag": "billing"}
FAIL 09 [datasets] Show me the dataset called golden-qa.
       search_operations {"query": "dataset"}
       ended: no tool call; stop_reason end_turn
       want: execute_read datasets_get {"datasetName": "golden-qa"}
PASS 10 [datasets] List the items of the dataset golden-qa.
       search_operations {"query": "golden-qa"}
       search_operations {"query": "DatasetItems"}
       execute_read datasetItems_list {"datasetId": "golden-qa"}
       execute_read datasetItems_list {"datasetName": "golden-qa"}
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost day trace"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-21T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-28T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-21T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-28T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-21T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-28T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-21T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-28T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-21T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-28T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-21T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-28T00:00:00Z\\\"}"}
       ended: max turns (8)
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day", "last_days": 7}
TOTAL 7/11 passed
```

## Run 4 — description reworded, `--only 8,9,11`

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "Prompts"}
       describe_operation {"operationId": "prompts_list"}
       execute_read prompts_list {"filter": "billing"}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
PASS 09 [datasets] Show me the dataset called golden-qa.
       search_operations {"query": "dataset"}
       execute_read datasets_get {"datasetId": "golden-qa"}
       execute_read datasets_get {"datasetName": "golden-qa"}
PASS 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost trace"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"filters\":[{\"column\":\"traceName\",\"operator\":\"=\",\"value\":\"checkout\",\"type\":\"string\"}],\"timeDimension\":{\"granularity\":\"day\"},\"fromTimestamp\":\"2026-09-21T00:00:00Z\",\"toTimestamp\":\"2026-09-28T00:00:00Z\"}"}
TOTAL 2/3 passed
```
