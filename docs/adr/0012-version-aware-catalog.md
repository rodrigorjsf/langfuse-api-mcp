---
status: accepted
---
# Version-aware catalog: expose exactly the operations the connected deployment answers

Replaces the earlier "legacy reads only on v3" decision, which was rewritten before any code existed. Works together with ADR-0004, which lists the operations excluded on every deployment.

## Context

Whether a Langfuse operation answers depends on the **version** and on the **write mode** (`LANGFUSE_MIGRATION_V4_WRITE_MODE`, `…_ALLOW_PREVIEW_OPT_IN`). Evidence is in `docs/research/langfuse-api-versions.md`.

- **Self-hosted 3.80.0** lacks the v4 read APIs, which return an HTML 404.
- **Self-hosted 3.225.11** routes `/v2/observations`, but answers it with a 404 JSON: "only available in a Langfuse v4 write mode".
- **v4 in `events_only` mode** (the default) returns 404 for the legacy reads.
- **v4 in `dual` mode** answers everything.
- **Cloud** serves both families until 2026-11-16.
- **Self-hosted v3** is patched until January 2027.

Users on old and on new deployments must both get every operation their deployment can serve, without configuring anything.

## Decision

1. **Union catalog.** A maintainer script builds the catalog from the OpenAPI spec of every Langfuse release tag from **v3.0.0** onward, and the result is committed and embedded.
   - Each operation keeps the schema from the last spec that contained it.
   - Each operation carries its version range: `introduced`, and `removed` if it left the spec.
   - Each operation may carry a gated **operation family**.
   - An operation whose description opens with a deprecation notice carries `x-summary`, its operation index line: what it does plus "legacy: prefer <replacement> when it is available", the replacement being the operation at the path the notice names, or "(legacy)" alone when the notice names none (#79).
   - Where the docs state an earlier floor than the spec (OTLP 3.22.0, `/v3/scores` 3.179.0), the earlier floor wins.
   - CI fails if the committed catalog is stale against the script's output.
2. **Families gated by write mode.** Each family has one sentinel probe:
   - **legacy family**: deprecated reads, dataset runs, run items. Sentinel: `GET /traces?limit=1`.
   - **v4 read family**: `/v2/observations`, `/v2/metrics`. Sentinel: `GET /v2/observations?limit=1&fields=core`.
   - **experiments family**: `/experiments`, `/experiment-items`. Sentinel: `GET /experiments?limit=1&fromStartTime=<now>`.
   - `/v2/metrics` is never probed, because it spends the Cloud Hobby plan's 100-per-day budget.
3. **Startup resolution, once per process.**
   - `GET /api/public/health` (no credentials) gives the version. The catalog keeps the operations whose range contains it.
   - Each sentinel then decides its family. A 404 whose JSON names a write mode, or a 404 with an HTML body, turns the family off. A 200 or 400 turns it on.
   - Anything else leaves the family on and logs one stderr warning: health unreachable, version unparsable, 401, network error.
   - An unknown **newer** version is treated as the newest known version.
   - The tool set is then fixed until the process exits. Hosts restart stdio servers per session, so a later migration is picked up on the next start.
4. **Supported floor: v3.0.0.** Langfuse no longer supports v2. On an older version the server still starts and logs "unsupported Langfuse version"; the catalog is then filtered by the ranges alone.
5. **Cloud gets no special case.** The probes decide, as everywhere. The descriptions of legacy operations say "legacy: prefer <replacement> when it is available", and `search_operations` ranks the v4 family first when both families are on.
6. **Workflow tools require the v4 read family.** Without it they are not registered, and `execute_read` with the legacy operations covers those reads. Legacy-family workflow adapters are a ROADMAP item.
7. **Runtime errors.** Any 404 that means "unavailable" maps to the tool error `operation_unavailable`, whose hint names the detected version and the family. This covers the HTML body, the `events_only` message and the "v4 write mode" message. The body is never echoed. It is told apart from `langfuse_not_found`: JSON `LangfuseNotFoundError` for a missing resource.

## Considered Options

- **Version number only.** Rejected: v3 ≥ 3.141 and v4 in `legacy`, `dual` or `events_only` mode share version ranges but answer different families.
- **Latest spec only.** Rejected: it drops operations still live on supported deployments (v1 `GET /scores` before 3.53, `/metrics/daily` before 3.62, `unstable/evaluators` from 3.170 to 4.30).
- **Probe every operation.** Rejected: about 150 startup requests, a burden on the rate limits, for no gain over one probe per family.
- **v4-only (the original ADR-0004 stance).** Rejected: it leaves self-hosted v3 without trace, observation, score and metric reads.

## Consequences

- The catalog test changes. For a table of `(version, families)` fixtures, the resolved catalog must equal the expected operation set; the generated file is also checked for freshness.
- Integration tests pin one deployment per family combination:
  - 3.80.0, legacy only;
  - latest 3.x (3.225.11), legacy only (corrected 2026-09-27; see the amendment below);
  - 4.x `events_only`;
  - 4.x `dual`.
  Old v3 composes need `postgres:17` and `clickhouse-server:24.3`, and v3 seeding uses OTLP/protobuf.
- Startup costs one unauthenticated health call plus 2–3 authenticated GETs.
- The startup log lists the detected version, the families and the operation count. It never lists payloads.
- **When the migration modes go away** (planned by Langfuse for a future major), the probes still work; only the fixtures change.

## Amendment: freshness, startup budget and test cadence (2026-09-26, M3 grilling)

- **Freshness (replaces "CI fails if the committed catalog is stale" in §1).** Regenerating needs the spec of hundreds of release tags from GitHub, and a pull request of ours cannot change upstream. So every pull request checks offline only: the embedded catalog parses and the deployment-profile fixture test passes. A weekly job runs the generator and opens a pull request when the output changes, like the Go toolchain bump workflow. A new Langfuse operation stays unlisted for at most about a week; nothing breaks meanwhile.
- **Startup budget (adds to §3).** The health call and the sentinel probes run in parallel within a total budget of about 5 seconds. A probe that has not answered by then counts as "anything else": its family stays on and one stderr warning is logged. A slow or down Langfuse never holds up the host's `initialize`.
- **Integration cadence (changes Consequences).** Every pull request runs the integration suite against 4.x `events_only`, as today. The four pinned deployments (3.80.0, latest 3.x, 4.x `events_only`, 4.x `dual`) run in the weekly job next to the Cloud run. A v3-only regression can surface up to a week after merge.

## Amendment: the latest 3.x serves the legacy family only (2026-09-27, #84)

- **Corrects Consequences.** This record first said the latest 3.x deployment serves the legacy family "plus experiments". A live 3.225.11 (the latest 3.x) serves the legacy family only: `/experiments` and `/experiment-items` are routed but answer 404 JSON "The experiments API is only available in a Langfuse v4 write mode", like `/v2/observations` (observed 2026-09-26, #73; `docs/research/langfuse-api-versions.md` §1). The earlier claim came from a prototype scan that counted every spec operation as routed and did not tell a gated 404 from an answer.
- **Detection is unchanged.** The experiments sentinel reads that 404 as "family off", which is correct; the integration pin and the catalog fixture for 3.225.11 expect the legacy family only.
- **The pins no longer cover one family combination each.** 3.80.0 and 3.225.11 are both legacy only; they differ by version range (operations added across 3.x), which is why both stay pinned. No 3.x release observed so far serves the experiments family.

## Amendment: startup holds the host's first message for up to the budget (2026-09-28, #145)

- **Corrects the startup budget amendment.** "A slow or down Langfuse never holds up the host's `initialize`" overstated it. The server reads stdin only after detection, so the host's first message waits for up to the 5 s budget; the budget bounds the wait, it does not remove it.
- **Consequence found in #145.** Claude Code probes `server/discover` first and, after 3 s without an answer, falls back to a legacy `initialize`. With both queued, go-sdk v1.8.0 refused the `initialize` as a duplicate. The stdio transport therefore offers no protocol from 2026-07-28, so the probe negotiates nothing and the `initialize` succeeds ([record](../research/raw/2026-09-28-claude-code-slow-startup-handshake.md)). The startup order and the fixed tool set are unchanged.
