# Small-model eval: which local model makes better use of a 12 GB GPU (2026-09-27)

Verbatim output of `scripts/small-model-eval-local.sh`, all 11 intents, for four local model configurations,
with container, image pull and model pull lines removed. Question: with the eval's run settings kept as they
are (context 16384, temperature 0, `--max-tokens 4096 --timeout 600`, one request at a time, every layer on the
GPU, container capped at 4 GiB of RAM and 2 CPUs), does a larger or less quantized model than `qwen3:8b` Q4_K_M
pass more intents on the maintainer's RTX 3060 12 GB?

- **Code:** `5333416`, whose Go code, catalog and eval script are identical to `48000b9` (the three commits
  between them touch only docs and ADRs). Ollama 0.34.4, image pinned in `scripts/small-model-eval-ollama.yml`.
- **Host:** WSL with 7.8 GiB of RAM, 4 GiB of swap, 12 CPUs; 11249 MiB of 12288 MiB VRAM free before the runs
  (the Windows desktop holds the rest). The preflight minimum of 6144 MiB `MemAvailable` refused to start
  (6044 MiB available), so every run used `MIN_MEM_AVAILABLE_MIB=5120`, a one-off override the maintainer
  approved; the container caps were unchanged.
- **The only server knob changed:** `OLLAMA_KV_CACHE_TYPE` (`f16`, Ollama's default, or `q8_0`). Ollama started
  llama-server with `--cache-type-k q8_0 --cache-type-v q8_0 --flash-attn auto`, and the log said
  `flash_attn = enabled` and `KV buffer size = 1224.00 MiB` for qwen3:8b at 16384 cells (about 2304 MiB at
  `f16`). `qwen3:14b` does not fit at `f16`: its 9.3 GB file plus an `f16` cache of 16384 cells is over 12 GB.
- **Measured by** sampling `nvidia-smi` memory.used and `/proc/meminfo` `MemAvailable` every 2 s for the whole
  run. VRAM is the whole GPU, the Windows desktop's ~865 MiB included. Tokens per second were not measured, so a
  spill into shared system memory near the VRAM ceiling (the Windows driver's sysmem fallback) is not ruled out
  for `qwen3:14b`; `ollama ps` said `100% GPU` for every model.
- **Adoption rule, agreed before the runs:** a candidate replaces the default only if it passes every intent the
  baseline passes, passes at least one more, and never hits the timeout; and its peak VRAM stays at or under
  about 11 GiB.

| Configuration | Model ID | Loaded (`ollama ps`) | Peak VRAM | Min `MemAvailable` | Wall time | Result | Fails |
|---|---|---|---|---|---|---|---|
| `qwen3:8b` Q4_K_M, KV `f16` (baseline, the default) | `500a1f067a9f` | 7.5 GB | 8169 MiB | 4637 MiB | 435 s | **8/11** | 02, 03, 08 |
| `qwen3:8b` Q4_K_M, KV `q8_0` (control) | `500a1f067a9f` | 6.4 GB | 7176 MiB | 4340 MiB | 449 s | 7/11 | 02, 03, 08, 09 |
| `qwen3:14b` Q4_K_M, KV `q8_0` | `bdbd181c33f2` | 10 GB | 11337 MiB | 4906 MiB | 1461 s | 9/11 | 03, 11 |
| `qwen3:8b-q8_0`, KV `q8_0` | `e56358ca25dd` | 9.6 GB | 10551 MiB | 4704 MiB | 1761 s | 6/11 | 02, 03, 08, 09, 11 |

**Verdict: the default stays `qwen3:8b` Q4_K_M with an `f16` KV cache.**

- `qwen3:14b` passes the most intents (9/11, gaining 02 and 08), but it fails intent 11, which the baseline
  passes (it ends its turn right after `describe_operation metrics_metrics`, with no timeout). Its peak of
  11337 MiB leaves 951 MiB of the GPU free, over the ~11 GiB budget. It also takes 3.4 times as long. It is a
  measured opt-in, `SMALL_MODEL=qwen3:14b OLLAMA_KV_CACHE_TYPE=q8_0`, not the default.
- The `q8_0` KV cache on its own costs the 8B model intent 09 (control run), so `f16` stays the default cache
  type. Whether the cache type also caused the 14B model's intent-11 failure cannot be tested here: the 14B model
  does not fit with an `f16` cache.
- `qwen3:8b-q8_0` does worse than the Q4_K_M baseline (6/11) and is four times as slow.
- Host memory was never the limit: every run started at about 6000 MiB `MemAvailable` and never went under
  4340 MiB, the 14B model included (its weights live in VRAM; host memory holds the runner and the page cache).
- Against the first recorded run (7/11, failing 02, 03, 05 and 11), the baseline now passes 05 and 11 (#99 is
  still open, but its intent passes at this code) and fails 08, which passed then: intent 08 stops after a
  `search_operations` query for "billing" that returns no match, a regression filed as #114.

## Baseline: `qwen3:8b` Q4_K_M, KV `f16`

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-27 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
PASS 01 [traces] Show me everything that happened inside trace 7f3c2a91-checkout, as a tree of its observations.
       get_trace_tree {"include": ["io", "metadata"], "traceId": "7f3c2a91-checkout"}
FAIL 02 [observations] List the generation observations named 'summarize-ticket'.
       search_operations {"query": "observation generation"}
       describe_operation {"operationId": "observations_getMany"}
       execute_read observations_getMany {"name": "summarize-ticket"}
       get_trace_tree {"traceId": "summarize-ticket"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"name": "summarize-ticket", "type": "GENERATION"}
FAIL 03 [observations] Which observations belong to session sess-2024-17?
       get_trace_tree {"include": ["io", "metadata"], "traceId": "sess-2024-17"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"sessionId": "sess-2024-17"}
PASS 04 [observations] Find the observations with level ERROR for user u-5521.
       search_operations {"query": "trace list user"}
       describe_operation {"operationId": "observations_getMany"}
       execute_read observations_getMany {"level": "ERROR", "userId": "u-5521"}
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       search_operations {"query": "score"}
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
PASS 06 [scores] Which score configs are defined in this project?
       search_operations {"query": "score"}
       execute_read scoreConfigs_get {}
PASS 07 [prompts] Fetch the prompt 'support-reply' with the label production.
       execute_read prompts_get {"label": "production", "name": "support-reply"}
       execute_read prompts_get {"label": "production", "promptName": "support-reply"}
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
PASS 09 [datasets] Show me the dataset called golden-qa.
       search_operations {"query": "golden-qa"}
       search_operations {"query": "Datasets"}
       execute_read datasets_list {"name": "golden-qa"}
       describe_operation {"operationId": "datasets_get"}
       execute_read datasets_get {"datasetName": "golden-qa"}
PASS 10 [datasets] List the items of the dataset golden-qa.
       search_operations {"query": "dataset"}
       execute_read datasetItems_list {"datasetId": "golden-qa"}
       execute_read datasetItems_list {"datasetName": "golden-qa"}
PASS 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost daily"}
       execute_read metrics_metrics {"end": "2026-09-27", "start": "2026-09-20", "traceName": "checkout"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"filters\":[{\"column\":\"traceName\",\"operator\":\"=\",\"value\":\"checkout\",\"type\":\"string\"}],\"timeDimension\":{\"granularity\":\"day\"},\"fromTimestamp\":\"2026-09-20T00:00:00Z\",\"toTimestamp\":\"2026-09-27T00:00:00Z\"}"}
TOTAL 8/11 passed
```

## Control: `qwen3:8b` Q4_K_M, KV `q8_0`

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-27 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
PASS 01 [traces] Show me everything that happened inside trace 7f3c2a91-checkout, as a tree of its observations.
       get_trace_tree {"include": ["io", "metadata"], "traceId": "7f3c2a91-checkout"}
FAIL 02 [observations] List the generation observations named 'summarize-ticket'.
       search_operations {"query": "observation generation summarize-ticket"}
       execute_read observations_getMany {"name": "summarize-ticket"}
       execute_read trace_list {"name": "summarize-ticket"}
       search_operations {"query": "observation"}
       execute_read observations_getMany {"name": "summar,ize-ticket"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"name": "summarize-ticket", "type": "GENERATION"}
FAIL 03 [observations] Which observations belong to session sess-2024-17?
       get_trace_tree {"include": ["metadata"], "traceId": "sess-2024-17"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"sessionId": "sess-2024-17"}
PASS 04 [observations] Find the observations with level ERROR for user u-5521.
       search_operations {"query": "user trace error"}
       execute_read observations_getMany {"level": "ERROR", "user": "u-5521"}
       execute_read observations_getMany {"level": "ERROR", "userId": "u-5521"}
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       get_trace_tree {"include": ["metadata"], "traceId": "tr-88"}
       search_operations {"query": "score"}
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
PASS 06 [scores] Which score configs are defined in this project?
       search_operations {"query": "score config"}
       execute_read scoreConfigs_get {}
PASS 07 [prompts] Fetch the prompt 'support-reply' with the label production.
       execute_read prompts_get {"label": "production", "promptId": "support-reply"}
       execute_read prompts_get {"label": "production", "promptName": "support-reply"}
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "prompts"}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
FAIL 09 [datasets] Show me the dataset called golden-qa.
       search_operations {"query": "golden-qa"}
       search_operations {"query": "Datasets"}
       execute_read datasets_list {"limit": 100, "name": "golden-qa"}
       describe_operation {"operationId": "datasets_list"}
       execute_read datasets_list {"limit": 100}
       ended: no tool call; stop_reason end_turn
       want: execute_read datasets_get {"datasetName": "golden-qa"}
PASS 10 [datasets] List the items of the dataset golden-qa.
       search_operations {"query": "dataset"}
       execute_read datasetItems_list {"datasetId": "golden-qa"}
       execute_read datasetItems_list {"datasetName": "golden-qa"}
PASS 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost day"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"filters\":[{\"column\":\"traceName\",\"operator\":\"=\",\"value\":\"checkout\",\"type\":\"string\"}],\"timeDimension\":{\"granularity\":\"day\"},\"fromTimestamp\":\"2026-09-20T00:00:00Z\",\"toTimestamp\":\"2026-09-27T00:00:00Z\"}"}
TOTAL 7/11 passed
```

## `qwen3:14b` Q4_K_M, KV `q8_0`

```text
model qwen3:14b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-27 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
PASS 01 [traces] Show me everything that happened inside trace 7f3c2a91-checkout, as a tree of its observations.
       get_trace_tree {"include": ["io", "metadata"], "traceId": "7f3c2a91-checkout"}
PASS 02 [observations] List the generation observations named 'summarize-ticket'.
       search_operations {"query": "observation list"}
       describe_operation {"operationId": "observations_getMany"}
       execute_read observations_getMany {"name": "summarize-ticket", "type": "generation"}
FAIL 03 [observations] Which observations belong to session sess-2024-17?
       get_trace_tree {"include": ["io", "metadata"], "traceId": "sess-2024-17"}
       execute_read observations_getMany {}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"sessionId": "sess-2024-17"}
PASS 04 [observations] Find the observations with level ERROR for user u-5521.
       search_operations {"query": "observation level user"}
       execute_read observations_getMany {"level": "ERROR", "userId": "u-5521"}
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
PASS 06 [scores] Which score configs are defined in this project?
       search_operations {"query": "score"}
       execute_read scoreConfigs_get {}
PASS 07 [prompts] Fetch the prompt 'support-reply' with the label production.
       execute_read prompts_get {"label": "production", "name": "support-reply"}
       execute_read prompts_get {"label": "production", "promptName": "support-reply"}
PASS 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "prompt get"}
       describe_operation {"operationId": "prompts_list"}
       execute_read prompts_list {"tag": "billing"}
PASS 09 [datasets] Show me the dataset called golden-qa.
       search_operations {"query": "dataset"}
       execute_read datasets_get {"name": "golden-qa"}
       execute_read datasets_get {"datasetName": "golden-qa"}
PASS 10 [datasets] List the items of the dataset golden-qa.
       search_operations {"query": "dataset"}
       execute_read datasetItems_list {"datasetId": "golden-qa"}
       execute_read datasetItems_list {"datasetName": "golden-qa"}
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost"}
       search_operations {"query": "metrics"}
       describe_operation {"operationId": "metrics_metrics"}
       ended: no tool call; stop_reason end_turn
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day", "last_days": 7}
TOTAL 9/11 passed
```

## `qwen3:8b-q8_0`, KV `q8_0`

```text
model qwen3:8b-q8_0; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-27 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
PASS 01 [traces] Show me everything that happened inside trace 7f3c2a91-checkout, as a tree of its observations.
       get_trace_tree {"include": ["io", "metadata"], "traceId": "7f3c2a91-checkout"}
FAIL 02 [observations] List the generation observations named 'summarize-ticket'.
       search_operations {"query": "observation generation"}
       describe_operation {"operationId": "observations_getMany"}
       execute_read observations_getMany {"name": "summarize-ticket"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"name": "summarize-ticket", "type": "GENERATION"}
FAIL 03 [observations] Which observations belong to session sess-2024-17?
       get_trace_tree {"include": ["io", "metadata"], "traceId": "sess-2024-17"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"sessionId": "sess-2024-17"}
PASS 04 [observations] Find the observations with level ERROR for user u-5521.
       search_operations {"query": "trace list"}
       execute_read observations_getMany {"level": "ERROR", "user": "u-5521"}
       execute_read observations_getMany {"level": "ERROR", "userId": "u-5521"}
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       search_operations {"query": "score"}
       execute_read scoresV3_getManyV3 {"scoreName": "helpfulness", "traceId": "tr-88"}
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
PASS 06 [scores] Which score configs are defined in this project?
       search_operations {"query": "score config"}
       execute_read scoreConfigs_get {}
PASS 07 [prompts] Fetch the prompt 'support-reply' with the label production.
       execute_read prompts_get {"label": "production", "name": "support-reply"}
       execute_read prompts_get {"label": "production", "promptName": "support-reply"}
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
FAIL 09 [datasets] Show me the dataset called golden-qa.
       search_operations {"query": "dataset"}
       ended: no tool call; stop_reason end_turn
       want: execute_read datasets_get {"datasetName": "golden-qa"}
PASS 10 [datasets] List the items of the dataset golden-qa.
       search_operations {"query": "dataset golden-qa"}
       execute_read dataset_items_list {"datasetId": "golden-qa"}
       search_operations {}
       execute_read datasetItems_list {"datasetId": "golden-qa"}
       execute_read datasetItems_list {"datasetName": "golden-qa"}
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost trace"}
       execute_read metrics_metrics {"endDate": "2026-09-27", "metric": "cost", "startDate": "2026-09-20", "traceName": "checkout"}
       execute_read metrics_metrics {"query": "traceName=checkout&startDate=2026-09-20&endDate=2026-09-27&metric=cost"}
       execute_read metrics_metrics {"query": "{\\\"filters\\\":{\\\"traceName\\\":\\\"checkout\\\"},\\\"timeDimension\\\":\\\"day\\\",\\\"metrics\\\":[\\\"cost\\\"],\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-27T23:59:59Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"filters\\\":{\\\"traceName\\\":\\\"checkout\\\"},\\\"timeDimension\\\":\\\"day\\\",\\\"metrics\\\":[\\\"cost\\\"],\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-27T23:59:59Z\\\",\\\"view\\\":\\\"observations\\\"}"}
       execute_read metrics_metrics {"query": "{\"filters\":{\"traceName\":\"checkout\"},\"timeDimension\":\"day\",\"metrics\":[\"cost\"],\"fromTimestamp\":\"2026-09-20T00:00:00Z\",\"toTimestamp\":\"2026-09-27T23:59:59Z\"}"}
       execute_read metrics_metrics {"query": "{\"filters\":[{\"key\":\"traceName\",\"value\":\"checkout\"}],\"timeDimension\":\"day\",\"metrics\":[\"cost\"],\"fromTimestamp\":\"2026-09-20T00:00:00Z\",\"toTimestamp\":\"2026-09-27T23:59:59Z\"}"}
       execute_read metrics_metrics {"query": "{\"filters\":[{\"key\":\"traceName\",\"value\":\"checkout\"}],\"timeDimension\":{\"granularity\":\"day\"},\"metrics\":[\"cost\"],\"fromTimestamp\":\"2026-09-20T00:00:00Z\",\"toTimestamp\":\"2026-09-27T23:59:59Z\",\"view\":\"observations\"}"}
       ended: max turns (8)
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day", "last_days": 7}
TOTAL 6/11 passed
```
