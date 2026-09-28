# Small-model eval A/B of intent 03 with the user skill, local qwen3:8b (2026-09-28)

Verbatim output of `scripts/small-model-eval-local.sh --only 3` (#98, spec #132), with container, image pull and
model pull lines removed. Same model, Ollama image (`0.34.4`, KV cache f16) and settings as
[2026-09-27-small-model-eval-local-model-vram.md](2026-09-27-small-model-eval-local-model-vram.md)
(temperature 0, `--max-tokens 4096 --timeout 600`). Branch `spec/132-spec-m6-user-skill-langfuse-api-mcp-skil--98`;
server code unchanged from `3489a63`.

Arms, alternated, two runs each:

- **without**: no `--system-append`; the system prompt every earlier run sent.
- **with**: `--system-append` of `skills/langfuse-api-mcp/SKILL.md` followed by
  `skills/langfuse-api-mcp/references/traces.md` (3153 bytes, byte-identical to the committed files).

Result: without the skill 0/2 (the session ID goes to `get_trace_tree` as a trace ID, then to `observations_getMany`
as `traceId`, and the model stops); with the skill 2/2 (the first call is `observations_getMany` with `sessionId`).
The model's first calls with the skill carry invented or string-typed parameters (`page`, `pageSize`,
`isRootObservation: "true"`); the server refuses them and the model corrects itself from the refusal. No static
server text was added: the arm with the skill passes, as spec #132 requires before any server change.

```text
=== start 2026-09-28T04:09:59Z HEAD 605ba43 (the "with" arms ran the skill text of c848d6c)
=== arm without 2026-09-28T04:09:59Z
MemAvailable 6355 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 0 bytes
FAIL 03 [observations] Which observations belong to session sess-2024-17?
       get_trace_tree {"traceId": "sess-2024-17"}
       execute_read observations_getMany {"traceId": "sess-2024-17"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"sessionId": "sess-2024-17"}
TOTAL 0/1 passed
=== exit 1
=== arm with 2026-09-28T04:12:08Z
MemAvailable 6209 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 3153 bytes
PASS 03 [observations] Which observations belong to session sess-2024-17?
       execute_read observations_getMany {"isRootObservation": "true", "level": "all", "page": 1, "pageSize": 1000, "sessionId": "sess-2024-17"}
       execute_read observations_getMany {"isRootObservation": "true", "limit": 1000, "sessionId": "sess-2024-17"}
       execute_read observations_getMany {"isRootObservation": "true", "limit": 100, "sessionId": "sess-2024-17"}
       execute_read observations_getMany {"isRootObservation": true, "limit": 100, "sessionId": "sess-2024-17"}
TOTAL 1/1 passed
=== exit 0
=== arm without 2026-09-28T04:14:00Z
MemAvailable 6393 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 0 bytes
FAIL 03 [observations] Which observations belong to session sess-2024-17?
       get_trace_tree {"traceId": "sess-2024-17"}
       execute_read observations_getMany {"traceId": "sess-2024-17"}
       ended: no tool call; stop_reason end_turn
       want: execute_read observations_getMany {"sessionId": "sess-2024-17"}
TOTAL 0/1 passed
=== exit 1
=== arm with 2026-09-28T04:15:56Z
MemAvailable 6444 MiB; the Ollama eval stack needs at least 5120 MiB
building the server
ollama {"version":"0.34.4"}; KV cache f16
NAME        ID              SIZE      PROCESSOR    CONTEXT    UNTIL
qwen3:8b    500a1f067a9f    7.5 GB    100% GPU     16384      29 minutes from now
ollama-1  | load_tensors: offloaded 37/37 layers to GPU
model qwen3:8b; max tokens 4096; fake Langfuse 4.46.0 events_only; run date 2026-09-28 UTC; tools describe_operation, execute_read, get_trace_tree, search_operations; system prompt append 3153 bytes
PASS 03 [observations] Which observations belong to session sess-2024-17?
       execute_read observations_getMany {"isRootObservation": "true", "level": "all", "page": 1, "pageSize": 1000, "sessionId": "sess-2024-17"}
       execute_read observations_getMany {"isRootObservation": "true", "limit": 1000, "sessionId": "sess-2024-17"}
       execute_read observations_getMany {"isRootObservation": "true", "limit": 100, "sessionId": "sess-2024-17"}
       execute_read observations_getMany {"isRootObservation": true, "limit": 100, "sessionId": "sess-2024-17"}
TOTAL 1/1 passed
=== exit 0
```
