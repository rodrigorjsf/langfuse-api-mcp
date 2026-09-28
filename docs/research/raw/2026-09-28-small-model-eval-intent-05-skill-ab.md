# Small-model eval A/B of intent 05 with the user skill, local qwen3:8b (2026-09-28)

Verbatim output of `scripts/small-model-eval-local.sh --only 5` (#99, ticket #135, spec #132), with container, image
pull, model pull, curl readiness-retry and compose warning lines removed. Same model, Ollama image (`0.34.4`, KV cache
f16) and settings as [2026-09-28-small-model-eval-intent-03-skill-ab.md](2026-09-28-small-model-eval-intent-03-skill-ab.md)
(temperature 0, `--max-tokens 4096 --timeout 600`). Branch `spec/132-spec-m6-user-skill-langfuse-api-mcp-skil--135`;
server code unchanged from `708d1f2` in both rounds.

Arms, alternated, two runs each, in two rounds:

- **without**: no `--system-append`; the system prompt every earlier run sent.
- **with**: `--system-append` of `skills/langfuse-api-mcp/SKILL.md` followed by
  `skills/langfuse-api-mcp/references/scores.md`. Round 1 used the first draft of the reference (4622 bytes);
  round 2 used the committed text after the code-review fixes (4778 bytes, byte-identical to the committed files).

Result: intent 05 passes in every run of both arms and both rounds (with the skill 4/4, without it 4/4), so #99 no
longer reproduces. The first recorded run (2026-09-27, [output](2026-09-27-small-model-eval-qwen3-8b.md)) called
`get_trace_tree` and then `scoresV3_getManyV3` with `traceId` alone. The server has changed since (the M4 write
work and #114, among others; which change moved the model was not bisected). Without the skill the model now
guesses `scoreName`, is refused, and corrects it to `name`.

With the round 1 draft, the first call carried `name` and `traceId` plus a time window the user never gave, under
guessed names (`startTime`/`endTime`, likely from the entry file's bounded-window rule); the server refused them and
the model corrected them to `fromTimestamp`/`toTimestamp`. That window would hide the trace's older scores, so the
review fix tells the agent that a call filtered by a trace, session or observation ID needs no window unless the
user named one. With the round 2 text the first and only call is exactly `name` plus `traceId`.

No static server text was added: the arm with the skill passes, as spec #132 requires before any server change.

## Round 2 (committed text)

```text
=== start 2026-09-28T04:55:37Z HEAD d91eb02 (the "with" arms ran the working-tree skill text after the review fixes)
=== arm without 2026-09-28T04:55:37Z
MemAvailable 6222 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 0 bytes
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       execute_read scoresV3_getManyV3 {"scoreName": "helpfulness", "traceId": "tr-88"}
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
TOTAL 1/1 passed
=== exit 0
=== arm with 2026-09-28T04:57:39Z
MemAvailable 6236 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 4778 bytes
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
TOTAL 1/1 passed
=== exit 0
=== arm without 2026-09-28T04:59:12Z
MemAvailable 6139 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 0 bytes
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       execute_read scoresV3_getManyV3 {"scoreName": "helpfulness", "traceId": "tr-88"}
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
TOTAL 1/1 passed
=== exit 0
=== arm with 2026-09-28T05:01:03Z
MemAvailable 6205 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 4778 bytes
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
TOTAL 1/1 passed
=== exit 0
```

## Round 1 (first draft of the reference)

```text
=== start 2026-09-28T04:43:06Z HEAD 708d1f2 (the "with" arms ran the working-tree skill text)
=== arm without 2026-09-28T04:43:36Z
MemAvailable 6084 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 0 bytes
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       execute_read scoresV3_getManyV3 {"scoreName": "helpfulness", "traceId": "tr-88"}
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
TOTAL 1/1 passed
=== exit 0
=== arm with 2026-09-28T04:45:39Z
MemAvailable 6009 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 4622 bytes
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       execute_read scoresV3_getManyV3 {"endTime": "2026-09-28T23:59:59Z", "name": "helpfulness", "startTime": "2026-09-21T00:00:00Z", "traceId": "tr-88"}
       execute_read scoresV3_getManyV3 {"fromTimestamp": "2026-09-21T00:00:00Z", "name": "helpfulness", "toTimestamp": "2026-09-28T23:59:59Z", "traceId": "tr-88"}
TOTAL 1/1 passed
=== exit 0
=== arm without 2026-09-28T04:47:48Z
MemAvailable 6087 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 0 bytes
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       execute_read scoresV3_getManyV3 {"scoreName": "helpfulness", "traceId": "tr-88"}
       execute_read scoresV3_getManyV3 {"name": "helpfulness", "traceId": "tr-88"}
TOTAL 1/1 passed
=== exit 0
=== arm with 2026-09-28T04:49:46Z
MemAvailable 6093 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 4622 bytes
PASS 05 [scores] List the scores named 'helpfulness' that were given to trace tr-88.
       execute_read scoresV3_getManyV3 {"endTime": "2026-09-28T23:59:59Z", "name": "helpfulness", "startTime": "2026-09-21T00:00:00Z", "traceId": "tr-88"}
       execute_read scoresV3_getManyV3 {"fromTimestamp": "2026-09-21T00:00:00Z", "name": "helpfulness", "toTimestamp": "2026-09-28T23:59:59Z", "traceId": "tr-88"}
TOTAL 1/1 passed
=== exit 0
```
