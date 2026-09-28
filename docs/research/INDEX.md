# Research index

Evidence behind the ADRs. Labels: `[verified 3-0]` passed 3-vote adversarial check; `[sourced]` read on a primary source; `[sourced — unverified]` secondary or inferred. `[verified]` = executed by a prototype (code + verbatim output kept on a `prototype/*` branch, linked from the section).

| File | Answers | Feeds |
|---|---|---|
| [stack-and-sdk.md](stack-and-sdk.md) | which language/SDK; MCP spec 2026-07-28; what `mcp-server-dev` skills recommend | ADR-0001, 0002, 0005 |
| [tls-and-corporate-networks.md](tls-and-corporate-networks.md) | why corporate TLS breaks; per-language trust stores; Go 1.27 `SSL_CERT_*` trap; containers | ADR-0006 |
| [langfuse.md](langfuse.md) | auth, regions, org vs project keys, rate limits, pagination, deprecations, glossary, workflows, official MCP limits, spec-vs-docs mismatches | ADR-0004, `CONTEXT.md`, M6 skills |
| [security.md](security.md) | OWASP LLM 2025/2026, Agentic 2026, MCP Top 10 2025, MCP spec security, Anthropic review criteria, supply chain → control mapping | ADR-0003, `.claude/rules/security.md` |
| [engineering-process.md](engineering-process.md) | Matt Pocock skill vocabulary, workflow, doc rules | `.claude/rules/engineering-process.md`, `testing.md` |
| [langfuse-api-versions.md](langfuse-api-versions.md) | which operations exist per Langfuse version and write mode; how to detect them at runtime; per-family version floors | #16 version-aware catalog ADR |
| [go-tls-facts.md](go-tls-facts.md) | Go 1.27 status, `SSL_CERT_*` override semantics per OS, Linux default cert paths, fallback roots, go-sdk min Go | ADR-0006 |
| [mcp-hosts-env.md](mcp-hosts-env.md) | how each MCP host (Claude Code/Desktop, Cursor, VS Code, Codex, Gemini, Windsurf, Docker) passes env to a stdio server; which filter it | install docs, config guidance |
| prototype branches | `prototype/tls-trust-pool` (capture-and-unset proof, Linux ×3 + Windows 11) → `go-tls-facts.md` §7; `prototype/langfuse-io-window` (self-hosted 4.46.0 io/metadata window, limits, error shapes; self-hosted 3.80.0 operation availability) → `langfuse.md` §1.7–1.8 | #4/#13, #2/#15, M1 error mapping |
| `raw/2026-09-26-*` | verbatim integration-test probe of io/metadata windows and row limits on Cloud and self-hosted 4.46.0 (answered: no REST enforcement), Cloud back-dating limit | `langfuse.md` §1.6, #2/#15 |
| `raw/2026-09-27-small-model-eval-qwen3-8b.md` | the first recorded small-model discovery eval (local qwen3:8b, 7/11) and its reruns | ROADMAP M3, #88, follow-ups #97–#100 |
| `raw/2026-09-27-small-model-eval-intent-11-metrics-guidance.md` | intent 11 rerun four times while the metrics query guidance was worded (qwen3:8b; shipped wording passes) | ROADMAP M3, #104, follow-up #105 |
| `raw/2026-09-27-small-model-eval-intent-11-run-date.md` | intent 11 rerun with the run date in the prompt and the time window checked (qwen3:8b: window right, query sent over-escaped or as an object; FAIL) | ROADMAP M3, #105, follow-up #106 |
| `raw/2026-09-27-small-model-eval-intent-11-query-encoding.md` | intent 11 rerun after both metrics query refusals and the guidance got one query-encoding wording (qwen3:8b: PASS, window from the run date; a first wording saying "escaped" failed) | ROADMAP M3, #106 |
| `raw/2026-09-27-mcp-host-proof.md` | the README client snippets run on Linux: Claude Code end to end over npx against a local Langfuse (PASS, two forms), Gemini CLI startup with and without `env` (keys redacted without it), Codex CLI not completed (no credentials, local model did not load) | README "Client configuration", #125 |
| `raw/2026-09-27-small-model-eval-local-model-vram.md` | four local model configurations on an RTX 3060 12 GB, all 11 intents: qwen3:8b Q4_K_M stays the default (8/11); qwen3:14b with a q8_0 KV cache 9/11 but regresses intent 11 | ROADMAP M3, #114 |
| `raw/2026-09-28-small-model-eval-intent-08-no-match-hint.md` | intent 08 (and 09, 11) rerun five times around the #114 fix: before it qwen3:8b stops after the "billing" no-match, after it searches again every run; 08 passes 1 run in 4 | ROADMAP M6, #114 |
| `raw/2026-09-28-small-model-eval-intent-08-filter-parameter.md` | intent 08 rerun three times after the #114 no-match hint fix (qwen3:8b: searches again, then sends `prompts_list` `filter` instead of `tag`; FAIL) | ROADMAP M6, #133, follow-up #141 |
| `raw/2026-09-28-small-model-eval-intent-03-skill-ab.md` | intent 03 A/B with and without the user skill appended to the system prompt (qwen3:8b: 0/2 without, 2/2 with; no server text needed) | ROADMAP M6, #98, spec #132 |
| `raw/2026-09-28-small-model-eval-intent-05-skill-ab.md` | intent 05 A/B with and without the user skill (qwen3:8b: 4/4 both arms; with the skill the first call is exactly `name` plus `traceId`) | ROADMAP M6, #99, #135 |
| `raw/2026-09-28-small-model-eval-intent-12-prompt-injection.md` | the prompt-injection intent 12 in write mode with the skill's entry file and `prompts` reference (qwen3:8b: PASS, no `execute_write` call) | ROADMAP M6, #137 |
| `raw/2026-09-28-npx-skills-add.md` | `npx skills add` installs the user skill from a checkout into a scratch project (byte-identical); the repository form clones the private repo but finds no skill before the merge | ROADMAP M6, #139 |
| `raw/2026-09-28-small-model-eval-skill-ab-full.md` | M6 exit A/B: all 12 intents with and without the whole skill appended (qwen3:8b: 9/12 twice with, 10/12 after the prompts fix, 8/12 twice without; 03, 05, 08 and injection intent 12 pass with the skill; 08 without passes 2 of 3; the prompts table fix for intent 07) | ROADMAP M6, #140, #141 |
| `raw/2026-09-28-small-model-eval-intent-10-dataset-items.md` | intent 10 (dataset items) with the full skill before and after the #143 row (qwen3:8b: 0/1 before, 0/2 with a first wording, 3/3 with the final row; the full run with the final skill passes 11/12, only 02 fails) | ROADMAP M6, #143 |
| `raw/2026-09-28-claude-code-skill-run.md` | M6 exit run: Claude Code installs the skill with `npx skills add`, loads it on its own for the trace, cost and experiment workflows (not for prompts, #142), and completes all four against a local Langfuse, the label promotion after an accepted confirmation | ROADMAP M6, #140, #142 |
| [2026-09-25-deep-research-run1.md](2026-09-25-deep-research-run1.md) | raw verified findings + all extracted claims of the first deep-research run | all of the above |

## Open questions

- Version-aware catalog decisions (floor version, Cloud legacy until 2026-11-16, workflow tools per family, schemas for removed operations) — #16, `langfuse-api-versions.md`.
- macOS behavior of the `SSL_CERT_*` capture-and-unset design — CI proof in #13.
- Cursor, Windsurf env inheritance is undocumented (`mcp-hosts-env.md`); Claude Code's is undocumented but observed on Linux (`raw/2026-09-27-mcp-host-proof.md`). README tells users to pass the keys explicitly.
- Official Langfuse MCP/CLI TLS failure root cause (Node default trust is an unreproduced inference).
