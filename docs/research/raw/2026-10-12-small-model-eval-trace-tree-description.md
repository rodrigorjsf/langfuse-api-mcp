# The small-model eval lost three intents to one paragraph of the `get_trace_tree` description (2026-10-12)

**Question.** The second end-to-end campaign on Langfuse 4.46.0 `dual` ([record](2026-10-12-e2e-langfuse-4.46.0-dual.md)) ran `scripts/small-model-eval-local.sh` (qwen3:8b, temperature 0, the shipped skill as `--system-append`) and got 8 of 13 twice on `bcbae74`, against 11 of 13 on `909df6e` the same day. Which change did it, and why?

**Answer (verified).** One paragraph that #209 added to the `get_trace_tree` description (commit `6e9b52b`). A shorter sentence with the same warning does not have the effect. The description now holds the short sentence.

## What the model reads

The eval gives the model each tool's `name`, `description` and `inputSchema`, and the tool results. It never gives it an `outputSchema`. The tool list, built as the eval builds it, was hashed on 18 commits from `909df6e` to `bcbae74`: it changes once, at `6e9b52b` (read mode 6983 to 7362 bytes; the `get_trace_tree` description 1841 to 2218 characters). The text of 15 tool calls on the eval's fake Langfuse is identical on `909df6e`, `23023fb`, `6e9b52b`, `2877fb6` and `bcbae74`. So the `note` of #212 is ruled out: it is in the output schema, and its entries appear only on a deployment with both read families, while the eval's fake is `events_only`.

## The texts

Added by `6e9b52b` (375 characters):

> Where a deployment serves both the v4 read APIs and the legacy APIs, the tree is built from Observations v2, which can lack observations that were sent through the legacy ingestion API (late, or not at all), so a tree that is empty or shorter than expected can be checked against the legacy observations list through execute_read (legacy_observationsV1_getMany with traceId).

The sentence that replaces it (185 characters, same place):

> On a deployment that serves both, Observations v2 can lack observations sent through the legacy ingestion API: check an empty or short tree with legacy_observationsV1_getMany (traceId).

## Runs

Every run: qwen3:8b (`500a1f067a9f`), 100% on the GPU, Ollama 0.34.4, 2026-10-12. "Position" is the place of the run after a model load; the variants are `bcbae74` with only the description edited.

| Load | Position | Build | Paragraph | Score | Failed intents |
|---|---|---|---|---|---|
| campaign, twice | 1 | `bcbae74` | yes | 8/13 | 02 08 11 12 13 |
| campaign control | 1 | `909df6e` | no | 11/13 | 02 11 |
| batch 1 | 1 | `23023fb` (last before #209) | no | 11/13 | 02 11 |
| batch 1 | 2 | `6e9b52b` | yes | 10/13 | 02 12 13 |
| batch 1 | 3 | paragraph removed | no | 11/13 | 02 11 |
| batch 1 | 4 | a neutral paragraph of the same length | no | 11/13 | 02 11 |
| batch 1 | 5 | the short sentence | short | 11/13 | 02 11 |
| batch 1 | 6 | paragraph moved to the end | yes | 11/13 | 02 12 |
| batch 2 | 1 | `bcbae74` | yes | 8/13 | 02 08 11 12 13 |
| batch 2 | 2 | `6e9b52b` | yes | 10/13 | 02 12 13 |
| batch 2 | 3 | paragraph removed | no | 11/13 | 02 11 |
| batch 2 | 4 | the short sentence | short | 11/13 | 02 11 |
| batch 2 | 5 | a neutral paragraph of the same length | no | 11/13 | 02 11 |
| batch 3 | 1 | `6e9b52b` | yes | 8/13 | 02 08 11 12 13 |
| batch 3 | 2 | `bcbae74` | yes | 10/13 | 02 12 13 |
| the fix, alone | 1 | this change (the short sentence) | short | 11/13 | 02 11 |

## What is measured

- Intents 12 and 13 fail in all 7 runs with the paragraph in its place and pass in all 9 runs without it, with the neutral paragraph or with the short sentence.
- Length is not the cause: a neutral paragraph of the same length keeps 11 of 13. Context is not the cause: 341 requests in the Ollama log, none truncated, about 7,100 to 8,500 of 16,384 tokens; the paragraph adds 78 input tokens.
- Moving the paragraph to the end does not help: intent 12 still fails.
- The eval is not repeatable by build alone, only by build and position after a model load. With the paragraph, the first eval after a load gives 8 of 13 and a later one in the same load 10 of 13 (intents 08 and 11 pass there); batch 3 swapped the two builds and the two results swapped with them. Without the paragraph the result is the same at positions 1 and 3 to 5. A baseline is therefore taken as the first eval after a load, which is what `scripts/small-model-eval-local.sh` does when run once.
- How the three intents fail on the first turn (`6e9b52b`): on 12 the thinking decides on `prompts_get` with `label` `latest` and the turn ends with no tool call and no text; on 13 it states that `datasetItems_list` needs the dataset's ID and calls `datasets_get` first; on 08 it goes to `search_operations` instead of `prompts_list` with `tag`.

## What is not shown (hypotheses)

- Why this wording hurts and the short sentence does not; why the call the model decided on for intent 12 is never emitted.
- The mechanism of the order effect. The likely one is prompt-cache reuse in llama.cpp (later requests read about 5,950 cached input tokens).
- Any model other than qwen3:8b. The Haiku and Sonnet workers of the campaign read the long paragraph and answered 12 of 12 without the skill.

## Options weighed

1. Remove the paragraph: restores 11 of 13, and the warning for a tree that is short and not empty leaves the description (the hint of an empty tree, #209, still names the legacy reads).
2. The short sentence (chosen): keeps the warning and the operation ID; 11 of 13 as the first eval after a load.
3. Show the paragraph only on a deployment with both families: not taken. It would pass the eval by hiding the text from the `events_only` fake, a small model on a real `dual` deployment would still read it, and the description is one static text for every deployment.

Working files (not committed): `dist/e2e/eval-bisect.md`, `dist/e2e/eval-bisect/`, `dist/e2e/small-model-eval-vshort-first.log`.
