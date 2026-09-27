# Small-model eval intent 11 with the run date, local qwen3:8b (2026-09-27)

Verbatim output of `scripts/small-model-eval-local.sh --only 11` (#105), with container, image pull and model
pull lines removed. Same model, Ollama image and settings as
[2026-09-27-small-model-eval-qwen3-8b.md](2026-09-27-small-model-eval-qwen3-8b.md) (temperature 0,
`--max-tokens 4096 --timeout 600`). Code: branch `spec/68-spec-m3-full-read-surface-operation-disc--105`
at `7ce754c`.

What changed since the [#104 runs](2026-09-27-small-model-eval-intent-11-metrics-guidance.md):

- the eval's system prompt names the run date ("Today is 2026-09-27 (UTC).");
- the intent-11 matcher also checks the time window: `fromTimestamp` 6 or 7 days before the run date,
  `toTimestamp` (when given) on the run date; offline tests in `scripts/test_small_model_eval.py`
  (the review of #68 later relaxed it to #105's own rule: `fromTimestamp` within the 7 days before the run
  date, `toTimestamp` absent or no later than the end of the run date; the runs below fail on the query's
  encoding, not its window, so their result stands);
- the `describe_operation metrics_metrics` example window moved to `2025-01-01`…`2025-01-08`.

- **Result:** FAIL, twice, identical output. The model computes the right window from the run date
  (`2026-09-20T00:00:00Z`…`2026-09-27T00:00:00Z`) with the right view, measure, filter and granularity, but
  sends the query as a string with backslash-escaped quotes (not JSON) or as an object, and the server
  refuses both until the 8-turn budget ends. Follow-up: #106.

## Runs 1 and 2

```text
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-27 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations
FAIL 11 [metrics] What was the total cost per day over the last 7 days for traces named 'checkout'?
       search_operations {"query": "cost daily"}
       execute_read metrics_metrics {"end": "2026-09-27", "start": "2026-09-20", "traceName": "checkout"}
       describe_operation {"operationId": "metrics_metrics"}
       execute_read metrics_metrics {"query": "{\\\"view\\\":\\\"observations\\\",\\\"metrics\\\":[{\\\"measure\\\":\\\"totalCost\\\",\\\"aggregation\\\":\\\"sum\\\"}],\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"value\\\":\\\"checkout\\\",\\\"type\\\":\\\"string\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"toTimestamp\\\":\\\"2026-09-27T00:00:00Z\\\"}"}
       execute_read metrics_metrics {"query": {"filters": [{"column": "traceName", "operator": "=", "type": "string", "value": "checkout"}], "fromTimestamp": "2026-09-20T00:00:00Z", "metrics": [{"aggregation": "sum", "measure": "totalCost"}], "timeDimension": {"granularity": "day"}, "toTimestamp": "2026-09-27T00:00:00Z", "view": "observations"}}
       execute_read metrics_metrics {"query": "{\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"type\\\":\\\"string\\\",\\\"value\\\":\\\"checkout\\\"}],\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"metrics\\\": [{\\\"aggregation\\\":\\\"sum\\\",\\\"measure\\\":\\\"totalCost\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"toTimestamp\\\":\\\"2026-09-27T00:00:00Z\\\",\\\"view\\\":\\\"observations\\\"}"}
       execute_read metrics_metrics {"query": {"filters": [{"column": "traceName", "operator": "=", "type": "string", "value": "checkout"}], "fromTimestamp": "2026-09-20T00:00:00Z", "metrics": [{"aggregation": "sum", "measure": "totalCost"}], "timeDimension": {"granularity": "day"}, "toTimestamp": "2026-09-27T00:00:00Z", "view": "observations"}}
       execute_read metrics_metrics {"query": "{\\\"filters\\\":[{\\\"column\\\":\\\"traceName\\\",\\\"operator\\\":\\\"=\\\",\\\"type\\\":\\\"string\\\",\\\"value\\\":\\\"checkout\\\"}],\\\"fromTimestamp\\\":\\\"2026-09-20T00:00:00Z\\\",\\\"metrics\\\":[{\\\"aggregation\\\":\\\"sum\\\",\\\"measure\\\":\\\"totalCost\\\"}],\\\"timeDimension\\\":{\\\"granularity\\\":\\\"day\\\"},\\\"toTimestamp\\\":\\\"2026-09-27T00:00:00Z\\\",\\\"view\\\":\\\"observations\\\"}"}
       ended: max turns (8)
       want: execute_read metrics_metrics {} with query {"view": "observations", "measure": "totalCost", "filter": ["traceName", "checkout"], "granularity": "day", "last_days": 7}
TOTAL 0/1 passed
```
