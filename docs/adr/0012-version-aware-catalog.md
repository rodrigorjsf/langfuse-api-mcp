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
   - Each operation keeps the schema from the last spec that contained it. *Amended 2026-10-11 (M8): the request body of a write operation keeps every distinct body, each with the releases it holds for; see the amendment "a request body per release span" (#202).*
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
6. **Workflow tools require the v4 read family.** Without it they are not registered, and `execute_read` with the legacy operations covers those reads. Legacy-family workflow adapters are a ROADMAP item. *Amended 2026-10-10 (M8): the workflow tool is registered on every deployment, and for the trace tree there is a legacy-family adapter; see the [ADR-0002 amendment](0002-hybrid-tool-surface.md) "`get_trace_tree` builds the trace tree from the legacy observations list where only the legacy family answers".*
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
- **Consequence found in #145.** Claude Code probes `server/discover` first and, after 3 s without an answer, falls back to a legacy `initialize`. With both queued, go-sdk v1.8.0 refused the `initialize` as a duplicate. The stdio transport therefore offers no protocol from 2026-07-28, so the probe negotiates nothing and the `initialize` succeeds ([record](../research/raw/2026-09-28-claude-code-slow-startup-handshake.md)). The startup order and the fixed tool set are unchanged. The SDK records the protocols a transport offers only after it has started reading from it, so a probe already in the pipe could still be handled first and stored; the stdio transport therefore also reads nothing until its session is connected (#191, same record), and the probe's answer is the same as before.

## Amendment: a version range per parameter, and a filter scan for two of them (2026-10-10, M8 grilling)

Served parameters shipped in M8 (#173); the filter scan shipped in #183; `describe_operation` shows the filters the server scans apart from the served parameters (#199).

The end-to-end campaign on Langfuse 3.80.0 ([record](../research/raw/2026-10-10-e2e-langfuse-3.80.0.md), finding H1) showed that the union catalog offers a parameter on a deployment whose version does not declare it: `traceId` on `scores_get-many`, `level` on `legacy_observationsV1_getMany` and `fields` on `trace_get`. Langfuse 3.80.0 ignores each of them and answers the unfiltered list with no error, so the agent gets a plausible, wrong result. The cause is the generator: for each operation it keeps the parameters of the last release spec that contains the operation.

Decision:

- The generator records, for every path and query parameter, the release that introduced it and, when it left the spec, the release that removed it, the same way it already does per operation.
- At startup the catalog resolves each operation's **served parameters** for the detected version. `describe_operation` lists only those in `parameters` (the two filters the server applies itself on such a deployment are shown apart from them, in `scannedFilters`, #199). A call that carries a parameter the version does not serve is refused with `invalid_argument` before any request; the message names the parameter, the release that serves it and the deployment's version, so that it cannot be mistaken for a misspelled name, and the hint says to call without it and filter the rows over every page.
- When the version could not be detected, the newest release's parameters are served, as today, and the startup log says so.
- A change of a parameter's type or enum between releases is not tracked.

Refusing alone turns the most common investigation ("which traces had errors") into dozens of paged calls, so for a closed, static list of `(operation, parameter, row field)` the server answers the call with a **filter scan** instead of refusing: it reads the list without the parameter, keeps the rows whose field equals the value, and stops after 10 pages. A scan that reached the end returns a complete answer; one that hit the page limit is marked incomplete and says how many rows it read of how many exist. The list starts with the two pairs proven on 3.80.0 (`scores_get-many`/`traceId`, `legacy_observationsV1_getMany`/`level`). Whether a pair qualifies is never decided from a payload: an empty page shows no field. This part is the last slice of M8 and may ship after the rest.

As built (#183): the scan lives in `internal/workflows` (`filterscan.go`) and the catalog decides when it applies (`Operation.Scan`); a call is scanned only when it carries a pair of the list with a non-null value and the version does not serve the parameter. The call is checked as if the deployment served the filter, so its value gets every check of a sent one. The scan reads from page 1 at the page size maximum whatever `limit` the call names (still checked), and a `page` together with the filter is refused with `invalid_argument`. It ends where Langfuse's `meta.totalPages` says the list ends (else at the first short page). Each page is sanitized and redacted before its rows are compared. The result is page-shaped: the matching rows in `data` and a static summary in `meta.filterScan` (`appliedBy`, `parameter`, `rowField`, `complete`, `pagesRead`, `rowsRead`, `rowsExist`, `rowsMatched`), with none of Langfuse's own pagination metadata. A failed page ends the scan with the tool error of that failure and no rows.

Considered and rejected: documenting the gap only (the agent keeps getting wrong data); filtering the one page Langfuse returned (a short, clean and incomplete answer is worse than the long, visibly unfiltered one); tracking the introduction only (a parameter a later release removed would stay unavailable on the deployments that have it).

Consequences: the catalog grows by two fields per parameter; the OpenAPI spec of a release becomes the authority for what that release filters by, and where a spec is wrong the server will be wrong the same way; the filter scan is a second way to run an operation (ADR-0010 amendment) and multiplies the requests of one call by up to 10, inside the existing rate limit and counted in the audit line.

## Amendment: a request body per release span (2026-10-11, #202)

_Replaces "Each operation keeps the schema from the last spec that contained it" (§1) for the request body of a write operation. It is worth changing for the reason the previous amendment gave for the parameters._

A write operation had the request body of the newest release that holds it, for every deployment: `describe_operation` returned that schema and `execute_write` checked every body against it. Operations and parameters have a version range; the body had none, and the specs show that it changes. Read from the release specs (not run against a live deployment):

- `models_create`: `unit` is required before 3.8.0, optional and nullable from 3.8.0 to 4.18.x, required again from 4.19.0 (the spec of 3.28.2 alone has the 3.0.0 body). On both v3 pins of this milestone `execute_write` refused a model without `unit` with `invalid_argument` before any request, although the spec of that release does not require it: a false refusal.
- `scores_create` on 3.30.0 was described with `sessionId` and `datasetRunId` and no required `traceId`; its release spec requires `traceId` and has neither property (`sessionId` from 3.53.0, `datasetRunId` from 3.55.0). The agent learned the difference from a Langfuse 400 or from a key Langfuse dropped.
- Of the 35 write operations the server offers, 15 have more than one distinct body, `scores_create` 11. Changes sit at the top level (a property introduced, removed or made nullable, a `required` entry that goes and comes back) and below it (inside a `oneOf` or a nested property: `prompts_create`, `scoreConfigs_create`, `annotationQueues_createQueueItem`, `datasets_create`).

Decision:

- The generator keeps **every distinct body** of an operation, each with the releases it holds for. A body is its `required` flag and its schema, `$ref`s inlined from the spec of its release and converted to JSON Schema 2020-12; it carries `x-spans`, the runs of consecutive releases it holds for, each from `x-introduced` up to, not including, `x-removed`. A body that comes back after another one is stored once, with both spans. Two bodies that differ only in the `description` and `title` annotations (told apart from a property of that name by the rule the discovery cap of #190 uses) are one body, with the newest text. A release that lacks the operation is ignored, like a gap in an operation's range. The first span of an operation starts at the operation's own `x-introduced`, and the last body stays open-ended even when the operation left the spec. The file keeps `requestBody` out: the bodies are `x-request-bodies`; a plain OpenAPI spec still loads, its one `requestBody` holding for every release.
- At startup the catalog resolves **one body per write operation**, by the rule the served parameters use: the body of the newest release that is not newer than the deployment. With no detected version it is the newest body, as before, and the startup log already says the newest release is assumed. A version newer than any known one is the newest; one older than the floor is taken at the floor.
- `describe_operation` returns the resolved body's `required` flag and schema, reduced for the result size cap as in #190. `execute_write` checks a body against that same schema, in full at every cap, with the same checks and refusal text; the size, depth, object and absent-body checks and the confirmation of a destructive write are unchanged and come in the same order. A body is never checked against a schema of another version.
- The body guidance of #192 gives the same texts for every version. Its release table is gone: the resolved body schema is the deployment's, so naming the target properties it has is enough (the existing tests pass unedited).
- A schema is compiled when a resolution first chooses it and then shared, not when the catalog loads, so that loading does not compile 76 bodies where 34 are offered (the memory of the race suite, #201). A schema that does not compile stops startup (`Catalog.BodyError`, checked in `cmd/langfuse-mcp`) and `CheckBody` refuses every body for it; a catalog test compiles every stored body once.
- A keyword of an older body with no conversion to JSON Schema 2020-12 stops generation, as it does for the newest. The first regeneration found one: the bodies of `POST /ingestion` and `POST /scores` of the first releases (280 of them each) and of three `unstable/` operations carry the OpenAPI 3.0 `example`, which the generator now converts to `examples: [value]`; it changes no body's validity. No body is dropped, cut or left unconverted.

Considered and rejected:

- **A version range per body property and per `required` entry.** It cannot say what the specs show: a change inside a `oneOf`, a property that becomes nullable, a `required` entry that goes and comes back.
- **Documenting the gap only** (the Known limits entry #192 wrote). It leaves the false refusal.
- **A looser check for older versions** (drop `required`, or the body check). It would remove a security control (`security.md` "Write gating").

Size, measured on the same 403 releases and 118 distinct specs the committed file was built from (the tags up to 4.50.0): the embedded catalog was **504,215 bytes** and is **955,700 bytes** now (+89.5%, 0.91 MiB of the 1 MiB budget a catalog test keeps). Keeping the history of every operation would make it 2,072,165 bytes (+311%): the 17 operations the server excludes on every deployment (ADR-0004; 14 of them take a body) hold 43 bodies and about half of those bytes, `ingestion_batch` alone 16 bodies and about 980 KB. Growth over a half is the ticket's threshold, so those operations keep **only their newest body** (`x-newest-body-only`, no spans), and a catalog test fails when an operation the server offers is marked so (load refuses it). The list of operations is the generator's `NEWEST_BODY_ONLY`, the `excluded` list of `internal/catalog`; a change to ADR-0004's list changes both.

Reproduction: regenerating from those tags changes the committed file only in body data. The 138 operations, their parameters, version ranges, families, summaries, descriptions and the header (`x-newest-version` 4.50.0, 403 releases, 118 distinct specs) are identical, and the last body of each of the 49 operations that take one equals the body the file held (compared on regeneration; a catalog test pins the SHA-256 of the 34 resolved newest bodies, computed with the code before this change).

Consequences: the Langfuse release spec is the authority for a body, as ADR-0012 decided for a parameter: where a spec is wrong the server is wrong the same way (the 3.28.2 spec above is an example), and no live step checks it. The weekly regeneration diffs get larger when a body changes. Every stored body is checked as the newest is (stripped of hidden characters at load, compiles, no property named like a credential, fits the smallest result size cap reduced); a failed credential-name check on an older body stops the ticket that brings it, since an exclusion is the maintainer's decision. A change of a body below the annotations is a new body, so an enum value added in a later release makes a new span.
