# The report

Write one record: `docs/research/raw/<YYYY-MM-DD>-e2e-langfuse-<version>.md` (the date the campaign ran, UTC), in plain English, and add its row to `docs/research/INDEX.md`. The first campaign's record, `docs/research/raw/2026-10-10-e2e-langfuse-3.80.0.md`, is the model for depth and tone. Where the same deployment was tested before, say for each earlier finding whether it is still there, gone or changed.

Sections, in this order:

1. **Title and opening paragraph**: what ran, when, on which Langfuse, who did what (orchestrator, workers), and that nothing in the code or the issue tracker was changed.
2. **Setup**: Langfuse deployment, seed counts and seed time, server commit and detected deployment profile (version, families, operation counts), agent host version, the tools offered.
3. **Verdict in one paragraph**: what is solid, what is weak, and what the skill did.
4. **Findings**, by severity (high = an agent gets wrong data or cannot finish a normal task; medium = an agent is misled or wastes many calls; low = cosmetic or a wording gap). Each one: what happens, the exact call and the decisive returned text, where the cause lies (the server, the catalog, Langfuse, the skill, the host) and how you know. Mark "verified" only what you reproduced yourself outside the worker that found it; a cause you inferred is labelled a hypothesis.
5. **What held**: validation, sanitizing, the write gate, confirmations (accepted and declined), writes, operations coverage, the published package, each with its numbers.
6. **Skill A/B**: the table (per pair and arm: correct, audit calls, audit errors), the reading, and the limits (one repetition, the contamination the pilot measured, anything that differed between arms).
7. **Open issues checked**: one row per issue with result and evidence.
8. **Small-model eval**: the PASS and FAIL lines and the total, or why it did not run.
9. **What this campaign did not cover.**
10. **How to repeat it**: a pointer to `docs/development/integration-tests.md`.

Every number in the report comes from a command output or an audit window of this campaign. Before committing, re-derive each count you quote (calls, errors, operations, records) from the audit files and the answer key.
