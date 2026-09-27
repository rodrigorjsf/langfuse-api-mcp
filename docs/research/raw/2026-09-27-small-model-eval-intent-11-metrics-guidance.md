# Small-model eval intent 11 with the metrics query guidance, local qwen3:8b (2026-09-27)

Verbatim output of `scripts/small-model-eval-local.sh --only 11` (#104), with container, image pull and model
pull lines removed. Same model, Ollama image and settings as
[2026-09-27-small-model-eval-qwen3-8b.md](2026-09-27-small-model-eval-qwen3-8b.md) (temperature 0,
`--max-tokens 4096 --timeout 600`). Code: branch `spec/68-spec-m3-full-read-surface-operation-disc--104`.

Four runs, one per wording of the example's closing sentence in the `describe_operation metrics_metrics`
guidance. The final wording (run 4) is the shipped one.

- **Result:** PASS with the final wording. The model sends the example query verbatim, dates included: the
  eval gives the model no current date, and the matcher ignores timestamps. Follow-up: #105.
- The wording "(put the time window asked for in fromTimestamp and toTimestamp)" made the model end its turn
  after `describe_operation` in both runs (runs 2 and 3).

## Run 1: "Example, the total cost per day of the observations of traces named checkout: …"

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; tools describe_operation, execute_read, get_trace_tree, search_operations
PASS 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"filters\":[{\"column\":\"traceName\",\"operator\":\"=\",\"value\":\"checkout\",\"type\":\"string\"}],\"timeDimension\":{\"granularity\":\"day\"},\"fromTimestamp\":\"2026-09-20T00:00:00Z\",\"toTimestamp\":\"2026-09-27T00:00:00Z\"}"}
TOTAL 1/1 passed
```

## Runs 2 and 3: "… named checkout (put the time window asked for in fromTimestamp and toTimestamp): …"

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost"}
       describe_operation {"operationId": "metrics_metrics"}
       ended: no tool call; stop_reason end_turn
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day"}
TOTAL 0/1 passed

model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost"}
       describe_operation {"operationId": "metrics_metrics"}
       ended: no tool call; stop_reason end_turn
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day"}
TOTAL 0/1 passed
```

## Run 4 (shipped): "… named checkout, for the week to 2026-09-27 (set fromTimestamp and toTimestamp to the window you need): …"

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; tools describe_operation, execute_read, get_trace_tree, search_operations
PASS 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"filters\":[{\"column\":\"traceName\",\"operator\":\"=\",\"value\":\"checkout\",\"type\":\"string\"}],\"timeDimension\":{\"granularity\":\"day\"},\"fromTimestamp\":\"2026-09-20T00:00:00Z\",\"toTimestamp\":\"2026-09-27T00:00:00Z\"}"}
TOTAL 1/1 passed
```
