---
name: e2e-campaign
description: Run the end-to-end campaign of this MCP server against one pinned Langfuse version and write its report. Usage: /e2e-campaign <version>
argument-hint: <langfuse version, e.g. 3.80.0>
---

# End-to-end campaign

You are the **orchestrator**. Workers (subagents, Sonnet and Haiku only) reach Langfuse through the server's MCP tools and nothing else; you brief them, wait for each report, re-check what matters yourself, and write one curated report. The server's **audit line** is the evidence of what a worker called; a worker's report is a claim to check against it.

Langfuse version asked for: `$ARGUMENTS`

## Rules of the campaign

- **Audit, do not repair.** Production code stays as it is and GitHub issues stay untouched. Every finding goes to the report, for the maintainer to decide.
- **One stack at a time**, started through its script, and the preflight memory guard decides. If the guard refuses, put the choice to the user (free memory, or an explicit override they approve).
- **One worker per audit file at a time.** A worker on `langfuse-ro` and one on `langfuse-rw` may run together; two on the same server may not. `python3 scripts/e2e-audit.py mark <label>` before and after each worker.
- **Blind briefs.** A task given to a worker holds the user's question only: no operation ID, no parameter name, no path of the answer key.
- **Destructive calls** touch only records the campaign created (prefix `e2e-`), and the person at the host answers every confirmation dialog.

## Resolve the deployment

Map the version to one deployment of `scripts/langfuse-selfhosted.sh` (its header lists them): `3.80.0`, `3.225.11`, `4.46.0-events_only`, `4.46.0-dual`. For `4.46.0` with no mode, ask the user which. A version with no pinned deployment stops the campaign here: tell the user that a pinned compose file must be added first (`docs/development/integration-tests.md`), which is its own change.

## Pick the phase

The campaign has two phases because an agent host loads MCP servers and agent definitions only when it starts.

- The tools `mcp__langfuse-ro__search_operations` and `mcp__langfuse-rw__execute_write` are **absent** from this session: follow [`prepare.md`](prepare.md). It ends by asking the user to restart the host and invoke this skill again.
- They are **present**, and `python3 scripts/e2e-audit.py call ro execute_read '{"operationId":"health_health"}'` answers the version asked for: follow [`run.md`](run.md).
- They are present but the health call fails or answers another version: the stack is down or stale. Follow `prepare.md` from its first step.


