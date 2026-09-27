# Small-model eval intent 11 with one query-encoding wording, local qwen3:8b (2026-09-27)

Verbatim output of `scripts/small-model-eval-local.sh --only 11` (#106), with container, image pull and model
pull lines removed. Same model, Ollama image and settings as
[2026-09-27-small-model-eval-qwen3-8b.md](2026-09-27-small-model-eval-qwen3-8b.md) (temperature 0,
`--max-tokens 4096 --timeout 600`), and the run date in the prompt and the window check of
[#105](2026-09-27-small-model-eval-intent-11-run-date.md). Branch
`spec/68-spec-m3-full-read-surface-operation-disc--106`.

What changed since the [#105 runs](2026-09-27-small-model-eval-intent-11-run-date.md): both refusals of a
`metrics_metrics` query that is not a JSON string (an over-escaped string, an object) and the
`describe_operation metrics_metrics` guidance now carry one static wording, `catalog.MetricsQueryEncoding`.
Before, a non-JSON string was refused with "want a JSON object" and an object with the generic parameter hint.

- **Result:** PASS, with the window computed from the run date (`2026-09-20T00:00:00Z`…`2026-09-27T00:00:00Z`
  on run date 2026-09-27), twice, identical output, with the shipped wording.
- The first wording tried, "…not an object, and with its quotes escaped once, as in any JSON string, never
  twice", stopped the alternation (the model no longer sent an object) but the model kept sending the
  over-escaped string: FAIL. The word "escaped" read as an instruction to escape. The shipped wording drops it.

## Shipped wording (runs 2 and 3, at `d757ae4` + the wording of `03fe535`, then at `03fe535`)

Wording: "one string whose value is the JSON text of the query object, such as {"view":"observations",…}: not
an object, and the value itself has no backslash before its quotes".

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-27 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
PASS 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost daily"}
       execute_read metrics_metrics {"end": "2026-09-27", "start": "2026-09-20", "traceName": "checkout"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\"view\":\"observations\",\"metrics\":[{\"measure\":\"totalCost\",\"aggregation\":\"sum\"}],\"filters\":[{\"column\":\"traceName\",\"operator\":\"=\",\"value\":\"checkout\",\"type\":\"string\"}],\"timeDimension\":{\"granularity\":\"day\"},\"fromTimestamp\":\"2026-09-20T00:00:00Z\",\"toTimestamp\":\"2026-09-27T00:00:00Z\"}"}
TOTAL 1/1 passed
```

## First wording (run 1, at `d757ae4`)

Wording: "one string whose value is the JSON text of the query object, such as {"view":"observations",…}: not
an object, and with its quotes escaped once, as in any JSON string, never twice".

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-27 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost daily"}
       execute_read metrics_metrics {"end": "2026-09-27", "start": "2026-09-20", "traceName": "checkout"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-27T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-27T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-27T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-27T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-27T00:00:00Z\\\"}"}
       ended: max turns (8)
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day", "last_days": 7}
TOTAL 0/1 passed
```
