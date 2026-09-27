# Small-model discovery eval, local qwen3:8b through Ollama (2026-09-27)

Verbatim output of `scripts/small-model-eval.py` (#75, #88) run by `scripts/small-model-eval-local.sh`, with
image pull and model pull progress removed. No secret involved: the API key sent to Ollama is the literal
placeholder `ollama`.

- **Why this model:** the maintainer chose a local model over a paid API (no Anthropic key, #88); qwen3:8b is
  the largest model that fits fully on the development GPU.
- **Model:** `qwen3:8b` (Ollama ID 500a1f067a9f, Q4_K_M, 5.2 GB file, 7.5 GB loaded), context 16384,
  every layer on the GPU (`offloaded 37/37 layers to GPU`, `ollama ps`: `100% GPU`), temperature 0,
  `--max-tokens 4096 --timeout 600`, 8 turns per intent.
- **Server:** Ollama 0.34.4 (image pinned in `scripts/small-model-eval-ollama.yml`), Anthropic Messages API.
- **Hardware:** RTX 3060 12 GB (8.3 GB used during the run), WSL2 with 10 GB RAM, 4 CPUs.
- **Code:** branch `fix/88-small-model-eval-local`, commit f395eab plus the `ended:` diagnostic line of the
  eval script (the reruns).
- **Result:** 7/11. Intents 02, 03, 05 and 11 fail, and a rerun of those four gives the same calls (temperature 0).
  Follow-ups: intent 02 #97, 03 #98, 05 #99, 11 #100.

An earlier run the same day (before the prompt cache was turned off) was OOM-killed at intent 09 inside its
4 GiB container cap (`eval aborted: Messages API answered 500 (api_error)`); it is an infrastructure abort, not a result.

## Full run

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; tools describe_operation, execute_read, get_trace_tree, search_operations
PASS 01 [traces] Show me everything that happened inside trace 7f3c2a91-checkout, as a tree of its observations.
       get_trace_tree {"include": ["io", "metadata"], "traceId": "7f3c2a91-checkout"}
FAIL 02 [observations] List the generation observations named 'summarize-ticket'.
       want: execute_read observations_getMany {"name": "summarize-ticket", "type": "GENERATION"}
FAIL 03 [observations] Which observations belong to session sess-2024-17?
       get_trace_tree {"include": ["metadata"], "traceId": "sess-2024-17"}
       execute_read observations_getMany {"limit": 100}
       want: execute_read observations_getMany {"sessionId": "sess-2024-17"}
PASS 04 [observations] Find the observations with level ERROR for user u-5521.
       search_operations {"query": "trace list"}
       search_operations {"query": "observations"}
       execute_read observations_getMany {"level": "ERROR", "userId": "u-5521"}
FAIL 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       get_trace_tree {"include": ["metadata"], "traceId": "tr-88"}
       execute_read scoresV3_getManyV3 {"traceId": "tr-88"}
       want: execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
PASS 06 [scores] Which score configs are defined in this project?
       search_operations {"query": "score"}
       execute_read scoreConfigs_get {}
PASS 07 [prompts] Fetch the prompt 'support-reply' with the label production.
       execute_read prompts_get {"label": "production", "promptName": "support-reply"}
PASS 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "Prompts"}
       describe_operation {"operationId": "prompts_list"}
       execute_read prompts_list {"tag": "billing"}
PASS 09 [datasets] Show me the dataset called golden-qa.
       search_operations {"query": "dataset"}
       describe_operation {"operationId": "datasets_get"}
       execute_read datasets_get {"datasetName": "golden-qa"}
PASS 10 [datasets] List the items of the dataset golden-qa.
       search_operations {"query": "dataset"}
       execute_read datasetItems_list {"datasetName": "golden-qa"}
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       get_trace_tree {"include": ["metadata"], "traceId": "checkout"}
       search_operations {"query": "trace"}
       search_operations {"query": "trace list"}
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day"}
TOTAL 7/11 passed
```

Intent 11 later passed after #104 without its time window being checked, and fails again once the
eval names the run date (#105): see
[2026-09-27-small-model-eval-intent-11-run-date.md](2026-09-27-small-model-eval-intent-11-run-date.md).

## Rerun of the failing intents (`--only 2,3,5,11`)

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 02 [observations] List the generation observations named 'summarize-ticket'.
       ended: no tool call; stop_reason max_tokens
       want: execute_read observations_getMany {"name": "summarize-ticket", "type": "GENERATION"}
FAIL 03 [observations] Which observations belong to session sess-2024-17?
       get_trace_tree {"include": ["metadata"], "traceId": "sess-2024-17"}
       execute_read observations_getMany {"limit": 100}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"sessionId": "sess-2024-17"}
FAIL 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       get_trace_tree {"include": ["metadata"], "traceId": "tr-88"}
       execute_read scoresV3_getManyV3 {"traceId": "tr-88"}
       ended: no tool call; stop_reason end_turn
       want: execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       get_trace_tree {"include": ["metadata"], "traceId": "checkout"}
       search_operations {"query": "trace"}
       search_operations {"query": "trace list"}
       ended: no tool call; stop_reason end_turn
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day"}
TOTAL 0/4 passed
```

## Intent 02 with a larger turn budget (`--only 2 --max-tokens 8192`)

```text
model qwen3:8b; max tokens 8192; fake Langfuse 4.46.0 events_only; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 02 [observations] List the generation observations named 'summarize-ticket'.
       ended: no tool call; stop_reason max_tokens
       want: execute_read observations_getMany {"name": "summarize-ticket", "type": "GENERATION"}
TOTAL 0/1 passed
```
