# Small-model eval intent 08 after the #114 fix, local qwen3:8b: the `filter` parameter (2026-09-28)

Verbatim output of `scripts/small-model-eval-local.sh --only 8` (#133), with container, image pull and model pull
lines removed. Same model, Ollama image (`0.34.4`, KV cache f16) and settings as
[2026-09-28-small-model-eval-intent-08-no-match-hint.md](2026-09-28-small-model-eval-intent-08-no-match-hint.md)
(temperature 0, `--max-tokens 4096 --timeout 600`). Code: `3489a63` (the #114 fix, merged into the spec #132 work
branch), branch `spec/132-spec-m6-user-skill-langfuse-api-mcp-skil--133`.

## Summary

- **Intent 08 fails in 3 of 3 runs, the same way each time.** The model searches "billing" (no match, no hint),
  searches again with the `Prompts` tag, calls `describe_operation prompts_list`, then sends
  `execute_read prompts_list {"filter": "billing"}` and ends its turn.
- **The stop after the no-match is gone.** The model searched again after the "billing" no-match in all 3 runs here,
  as in 4 of 4 runs of the earlier record. #114's first hypothesis (the trace-only hint read as "nothing here") is
  confirmed by the fix. Its second hypothesis (#105's run date in the system prompt made the model stop) no longer
  applies: the run date is still in the prompt, and the model no longer stops there.
- **The failure left is a parameter choice, not a stop.** `prompts_list` in the bundled catalog (4.46.0) has a
  `filter` query parameter next to `tag`. The Langfuse OpenAPI spec gives neither parameter a description, and the
  current Langfuse API reference does not list `filter` at all. After `describe_operation` the model sees two
  undocumented string parameters and picks `filter`. The call is valid, the fake Langfuse answers it, and the model
  ends its turn with an answer. No hint or no-match text reaches this step. Closing it takes either the user skill
  (spec #132, "describe, then execute; never guess a parameter", re-measured by the eval A/B) or a new line of
  static server text, which spec #132 allows only for intents 03 and 05. That decision is follow-up #141.

## Run 1

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "Prompts"}
       describe_operation {"operationId": "prompts_list"}
       execute_read prompts_list {"filter": "billing"}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
TOTAL 0/1 passed
```

## Run 2

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "Prompts"}
       describe_operation {"operationId": "prompts_list"}
       execute_read prompts_list {"filter": "billing"}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
TOTAL 0/1 passed
```

## Run 3

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 08 [prompts] List the prompts tagged 'billing'.
       search_operations {"query": "billing"}
       search_operations {"query": "Prompts"}
       describe_operation {"operationId": "prompts_list"}
       execute_read prompts_list {"filter": "billing"}
       ended: no tool call; stop_reason end_turn
       want: execute_read prompts_list {"tag": "billing"}
TOTAL 0/1 passed
```
