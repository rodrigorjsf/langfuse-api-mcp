# Langfuse platform research for `langfuse-api-mcp`

Research date: 2026-09-25. Primary source: the `langfuse-docs` MCP (`getLangfuseDocsPage`, `searchLangfuseDocs`,
`getLangfuseOverview`), which serves the langfuse.com Markdown pages. Secondary sources: the raw MCP reference at
`https://mcp.reference.langfuse.com`, fetched and grepped as HTML; two GitHub threads surfaced by docs search; and the
local OpenAPI spec `internal/catalog/spec/langfuse-openapi.json`, inspected with Python.

**Labels.** `[sourced]` means the claim is stated on the cited page. `[sourced — unverified]` means it is inferred,
paraphrased from a summary, or is an absence claim. Nothing here was executed against a live Langfuse, so no claim is
marked as executed or verified. Doc paths are relative to `https://langfuse.com`.

---

## 1. Public API

### 1.1 Auth, base URLs, API groups

| Topic | Fact | Label / source |
|---|---|---|
| Auth | HTTP Basic: username = public key `pk-lf-…`, password = secret key `sk-lf-…`. | `[sourced]` `/docs/api-and-data-platform/features/public-api`; the spec's `components.securitySchemes.BasicAuth` agrees |
| Key scope | Project keys belong to a single project. Use one key pair per project. | `[sourced]` `/docs/api-and-data-platform/features/cli#authentication`, `/docs/glossary#api-key` |
| EU (default) | `https://cloud.langfuse.com/api/public` | `[sourced]` `/docs/api-and-data-platform/features/public-api#base-urls` |
| US | `https://us.cloud.langfuse.com/api/public` | same |
| Japan | `https://jp.cloud.langfuse.com/api/public` | same |
| HIPAA US | `https://hipaa.cloud.langfuse.com/api/public` | same |
| Self-hosted | `<your-host>/api/public`. v4.36.0+ serves its own spec at `/api/openapi.yaml` and an interactive reference at `/api/docs`. | same |
| Smoke test | `GET /api/public/projects` with a project key returns that key's project, including `organization.{id,name}`. | same |
| API groups | (1) Project-level APIs. (2) Organization-level APIs: projects, SCIM users, memberships. (3) Instance Management API, self-hosted only, for administering organizations. | `[sourced]` `/docs/api-and-data-platform/features/public-api` |
| Specs | Project API: `cloud.langfuse.com/generated/api/openapi.yml`. Org API: `cloud.langfuse.com/generated/organizations-api/openapi.yml`. | same |
| Env vars (CLI/SDK) | `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY`, `LANGFUSE_BASE_URL` (defaults to EU). | `[sourced]` `/docs/api-and-data-platform/features/cli#authentication` |
| Data freshness | The spec says OTel ingest plus Observations v2 plus Metrics v2 are "the only real-time path". Other endpoints "can delay data by about 10 minutes". The docs say v2 data from old SDKs (Python < 4.7.0, JS < 5.4.0) or from OTel exporters without `x-langfuse-ingestion-version: 4` can be delayed **up to 15 minutes**. The two sources give different numbers, so both are reported. | `[sourced]` spec `info.description`; `/docs/api-and-data-platform/features/public-api#v2`; `/docs/compatibility` |

### 1.2 Org-scoped keys and project-scoped keys

Organization keys are created in Organization Settings, or through the Instance Management API. SCIM and the org API are
**Enterprise** features on Cloud, and **Enterprise Edition** features when self-hosted. `[sourced]` `/docs/administration/scim-and-org-api`

| Endpoint family | Key type | Docs list it? | Spec marks it org-scoped? |
|---|---|---|---|
| `POST /projects`, `PUT/DELETE /projects/{id}` | org | yes | yes |
| `GET/POST /projects/{id}/apiKeys`, `DELETE /projects/{id}/apiKeys/{apiKeyId}` | org | yes | yes |
| `GET/PUT /organizations/memberships` | org | yes | yes |
| `DELETE /organizations/memberships` | org | **no** | yes |
| `GET/PUT/DELETE /projects/{id}/memberships` | org | PUT and DELETE only | yes (GET too) |
| `GET /organizations/projects`, `GET /organizations/apiKeys` | org | **no** | yes |
| `/scim/{ServiceProviderConfig,ResourceTypes,Schemas,Users,Users/{id}}` | org (Basic auth with org pk/sk) | yes, as SCIM | yes |
| `GET/PUT/DELETE /integrations/blob-storage[/{id}]` | org | **no** | yes |
| `GET /projects` | **project** key. Its description says to use `/organizations/projects` with an org key instead. | — | project-scoped |
| Everything else (traces, observations, prompts, datasets, scores, metrics, …) | project | — | — |

Sources: `[sourced]` `/docs/administration/scim-and-org-api`, and the local spec descriptions ("requires
organization-scoped API key"). **Mismatch:** the docs list 10 org routes, while the spec marks about 25 as org-scoped.
Treat the spec as authoritative. `[sourced — unverified]`

### 1.3 Rate limits (Langfuse Cloud)

Limits are applied **per organization and per resource bucket**, shared by all projects and keys in the org. Windows are
**fixed**, not rolling: the first request to a bucket starts its window. A request over the limit gets `429` with a
`Retry-After` header in seconds, which is the authoritative wait time. Self-hosted instances have **no enforced limits**.
Payloads are capped at 5 MB per request and 5 MB per response. For trace DELETE, the docs strongly advise no more than
30–50 IDs per request. Authenticated calls to `/api/public/mcp` count against the **General API** bucket.
`[sourced]` `/faq/all/api-limits`

| Bucket | Applies to | Hobby | Core | Pro/Team/Enterprise |
|---|---|---|---|---|
| Tracing | `/ingestion`, `/otel` | 1,000/min | 4,000/min | 20,000/min |
| Deprecated tracing | deprecated ingestion endpoints | 100/min | 400/min | 400/min |
| Trace data deletion | deletes of traces, observations, scores | 50/**day** | 200/day | 1,000/day |
| Prompts | prompt GETs | no limit | no limit | no limit |
| **Metrics API v2** | `GET /v2/metrics` | **100/day** | 100/**hour** | 500/hour |
| Deprecated Metrics v1 | `GET /metrics` | 100/day | 2,000/day | 2,000/day |
| Datasets | datasets and experiments APIs | 100/min | 200/min | 1,000/min |
| Deprecated read APIs | `GET /traces[/{id}]`, `/observations[/{id}]`, `/sessions[/{id}]` | 15/min | 30/min | 100/min |
| General API | everything else, including `GET /v2/observations` and `/api/public/mcp` | 30/min | 100/min | 1,000/min |

> **Design constraint.** Metrics v2 is the tightest bucket: 100 requests **per day** on Hobby and 100 per hour on Core.
> An MCP server should cache metric results, count its calls against a budget, and never retry a `429` without honoring
> `Retry-After`. Hobby General is 30 requests per minute, so paging through a trace tree has to be frugal too.
> `[sourced — unverified]` (design inference from `/faq/all/api-limits`)

### 1.4 Pagination styles (classified from the local spec)

| Style | Endpoints | Limits |
|---|---|---|
| **Cursor** (`limit` plus `cursor`; next page from `meta.cursor`, finished when it is absent or null) | `GET /v2/observations`, `/v3/scores`, `/experiments`, `/experiment-items`, `/v2/evaluators`, `/v2/evaluators/{id}/versions`, `/v2/evaluation-rules` | observations: default 50, max **1,000**. scores v3: default 50, max **100** (above 100 returns 400). experiments and items: default 50, max 100, and `scoreLimit` ≤ 50. |
| **Page/limit** (`page`, `limit`; response `meta` = `{page, limit, totalItems, totalPages}`) | annotation-queues (+ items), comments, dataset-items, `v2/datasets`, llm-connections, models, `v2/prompts`, score-configs, unstable dashboards and widgets, plus the deprecated traces, observations, sessions, `v2/scores`, dataset-runs and dataset-run-items | legacy observations max 100 |
| **None / row-limit** | `GET /v2/metrics` takes `config.row_limit`: default 100, range 1–1,000. `histogram` takes `config.bins` from 1 to 100 (default 10). | — |

Sources: `[sourced]` local spec (script over every GET: `cursor` vs `page` params); `/docs/api-and-data-platform/features/public-api#observations-pagination`, `#scores-filtering`;
`/faq/all/deprecated-api-migration#dataset-runs`; `/docs/metrics/features/metrics-api#v2`.
Observations v2 is **always sorted by `startTime` descending**. It has no `orderBy`. `[sourced]` public-api page.

**Page numbers start at 1.** `[verified]` the end-to-end campaign of 2026-10-10 against 3.80.0 ([finding M7](raw/2026-10-10-e2e-langfuse-3.80.0.md)): `page` 0 and -1 come back as a Langfuse 400, as does a `date-time` parameter that is not a timestamp. The server refuses both before any request (#179): the catalog gives every `page` the minimum 1, and a `date-time` parameter must be an RFC 3339 timestamp.

**Observations v2 cursor format (#93).** `meta.cursor` is base64 of a JSON keyset position: the sort key of the last row served, never an offset and never the page size. Two forms are documented; the first was also observed live:

- `{"lastStartTimeTo":"2026-09-24T01:53:32.683Z","lastTraceId":"a15f…1457","lastId":"dbd1a6c259d1b876"}`: decoded from a live cursor, self-hosted 4.46.0 `events_only`, `GET /v2/observations?limit=2&fields=core`, 2026-09-27. `[verified]` The cookbook `/guides/cookbook/example_data_migration` shows the same form. `[sourced]`
- `{"lastStartTime":"2025-12-15T10:30:00Z","lastId":"obs-100"}`: the example cursor of the Observations API docs page as quoted at triage of #93 (`/docs/api-and-data-platform/features/observations-api`, which then said "the cursor encodes only the read position, not your query"). `[sourced]` That path now redirects to the public-api page, whose pagination section says only "pass this cursor … to continue where you left off" and shows the truncated `eyJsYXN0...` (checked with the langfuse-docs MCP, 2026-09-27).

Consequences:

- The cursor holds no `limit`, so a cursor issued at one page size continues correctly at another. `[verified]` `TestLiveObservationsCursorContinuesAtADifferentLimitWithoutGapsOrRepeats` (`internal/server/cursor_integration_test.go`) seeds one trace of 5 spans, two of them with the same start time, and reads it through `execute_read` twice: at limit 2 throughout, and at limit 2 then limit 1 from the returned cursor. The second and third newest spans share a start time, so the first cursor falls inside a sort-key tie. Both reads return the same 5 IDs in the same order, with no gap and no repeat (self-hosted 4.46.0 `events_only`, 2026-09-27). This is what makes `get_trace_tree`'s continuation safe: its cursor comes from a page read at limit 1000, and `execute_read` continues it at a limit of at most 100.
- The cursor does not encode the query, so every call must repeat the filters of the first request (`traceId`, `fields`, time window, `filter`). `[sourced]` triage quote above, and the decoded cursors hold no filter; the current public-api page only shows it by example (it repeats `fields` and `traceId` next to the cursor). Not tested live: the probe always repeats the filters.
- The server treats the cursor as opaque: it never decodes it, sends it back as one query parameter, and returns it only inside the untrusted-data envelope.

### 1.5 Field projection (`fields`)

Seven operations of the union catalog take `fields` (`x-introduced` of the parameter in parentheses). The text of the parameter is not kept in the catalog, so the values were read in the OpenAPI spec of **every release from v3.73.0 to v4.56.0** (296 tags, `web/public/generated/api/openapi.yml` at each tag, 2026-10-11): the description of `fields` changes only at the releases named in the table, and the newest release of the union catalog (4.50.0) and 4.56.0 read alike. Checked against the API reference (`langfuse-docs` MCP, `searchLangfuseDocs`, 2026-10-11): the same texts. `[sourced]`

| Endpoint | Groups | Without `fields` | Notes |
|---|---|---|---|
| `GET /v2/observations` (3.141.0) | `core` (always: id, traceId, startTime, endTime, projectId, parentObservationId, type), `basic`, `time`, `io`, `metadata`, `model`, `usage`, `prompt`, `metrics`; `trace_context` (tags, release, traceName) **from 3.175.0** | `core,basic` | A field from a group you did not request is **absent**, not null. `modelId`, `inputPrice`, `outputPrice` and `totalPrice` are always present but null unless you request `model`. The three prices are **strings**. I/O comes back as raw strings. `parseIoAsJson=true` returns 400. `expandMetadata=<keys>` lifts the 200-character metadata truncation. An unknown value is silently ignored (§1.6, §1.7). |
| `GET /traces/{traceId}` (deprecated; 3.166.0) | `core` (always), `io` (input, output, metadata), `scores`, `observations`, `metrics` | every group | The text of `trace_list`. A group left out comes back empty: `scores` and `observations` as empty lists, `metrics` as `-1` for `totalCost` and `latency`. Langfuse 3.80.0 declares no `fields` here and ignores it (§1.10). `[sourced]` release specs |
| `GET /traces` (deprecated; 3.73.0) | `core` (always), `io` (input, output, metadata), `scores`, `observations`, `metrics` | every group | The sentence about empty arrays and `-1` is in the spec from 3.110.0; `fields=core` returned 537 bytes against 144,257 for the same `trace_list` page on 3.80.0 (campaign 2026-10-10). The server's static guidance on `describe_operation trace_list` (#178) names these values. `[sourced]` API reference `GET /api/public/traces` |
| `GET /v2/scores` (deprecated; 3.144.0) | `score` (the core score fields), `trace` (trace properties: userId, tags, environment; **sessionId from 3.156.0**) | both | When the call filters by a trace property (`userId` or `traceTags`), `trace` must be in `fields`, otherwise Langfuse answers **400** (said in the spec from 3.144.0, in every release since). `[sourced]` release specs, not run live |
| `GET /v3/scores` (3.179.0) | `details` (comment, configId, metadata), `subject` (kind: trace, observation, session or experiment; id; traceId for an observation-level score), `annotation` (authorUserId, queueId), **in addition to** the core fields that are always returned (id, projectId, name, value, dataType, source, timestamp, environment, createdAt, updatedAt) | the core fields only | An unknown name returns **400**. **`core` is not a value**: from 3.182.0 the spec lists only the three groups ("in addition to the always-returned core fields"). The specs of 3.180.0 and 3.181.0 still listed `core` among the allowed names ("Allowed: core, details, subject, annotation. Defaults to core") and no spec has listed it since; whether `fields=core` is answered 400 on a current release was not run live, so the server's guidance does not suggest it. The documented floor of the route is 3.179.0 (`DOCUMENTED_FLOORS` of the generator), but no spec before 3.180.0 contains it. `[sourced]` release specs |
| `GET /experiments` (3.206.0) | `core`, `metadata`, `scores` | `core` | The spec says nothing more about what a group adds, or whether `core` stays when other groups are asked. `[sourced]` release specs |
| `GET /experiment-items` (3.206.0) | `core`, `dataset`, `io`, `metadata`, `itemMetadata`, `experimentMetadata`, `scores` | `core,dataset` | As above. `[sourced]` release specs |

The server describes the values of every one of these in `describe_operation` and in the truncation hints (#178, #194). It checks nothing: Langfuse ignores an unknown group on most of them and answers 400 on `GET /v3/scores`.

Sources: `[sourced]` `/docs/api-and-data-platform/features/public-api#field-groups`, `#scores-field-groups`; the release specs; the API reference (`langfuse-docs` MCP).

### 1.6 Filtering and date ranges

- **Observations v2.** The first-class params are `fromStartTime` (≥), `toStartTime` (<), `traceId`, `name`, `type`,
  `level`, `userId`, `sessionId`, `environment` (repeatable), `version`, `parentObservationId`, `isRootObservation`.
  Anything else goes through `filter`, a URL-encoded JSON array of `{type, column, operator, value[, key]}`. **When
  `filter` is present it overrides the fixed params.** There is no `tags` param: filter tags with an `arrayOptions`
  condition. `input` and `output` accept only `=` or `matches` (full-text search). There is **no get-by-id**: use a
  filter on `id`. `[sourced]` `/docs/api-and-data-platform/features/public-api#observations-filters`
- **Scores v3.** Comma-separated values are OR-ed within one param and AND-ed across params. `traceId`, `sessionId` and
  `experimentId` are **mutually exclusive**. `observationId` **requires** `traceId`. `value` needs a single `dataType`;
  `valueMin` and `valueMax` need `NUMERIC`. `fromTimestamp` is inclusive and `toTimestamp` exclusive. Invalid
  combinations return 400. `[sourced]` same page, `#scores-filtering`
- **Experiments and experiment-items.** `fromStartTime` is **required**. `[sourced]` spec; `/faq/all/deprecated-api-migration#dataset-runs`
- **Date-range limits.**
  - The REST docs say only "always include a bounded time range". I found **no documented maximum window** for REST.
    `[sourced — unverified]` (absence claim)
  - The official **MCP** `listObservations` tool is stricter: "Requests that project or filter input, output, or
    metadata must include traceId, an id filter, or a date range of at most **14 days**. Date-scoped input/output
    projections support a maximum limit of **50**." `[sourced]` `mcp.reference.langfuse.com` (raw HTML)
  - **Self-hosted 4.46.0 REST does not enforce it** (2026-09-25): io and metadata projections with no window,
    14 d + 1 s, 15 d or 30 d windows returned 200 with rows, and `limit=51`/`1000` returned 51/all rows. `limit`
    max is 1000 (`1001` → 400 zod `too_big`). `[verified]` §1.7
  - **Neither Langfuse Cloud nor self-hosted REST enforces it** (2026-09-26, both Langfuse **4.46.0**; Cloud = the
    US-region Hobby test project; issue #2 answered by #15). The integration test
    `TestLiveLangfuseServesPayloadQueriesBeyondFourteenDaysAndFiftyRows` (`internal/server/iowindow_integration_test.go`,
    through `execute_read`) seeds back-dated spans over OTLP, probes, and deletes them. Every probe succeeded (a **2xx**:
    `execute_read` does not expose the status, and the Langfuse client turns any non-2xx into a tool error, so the
    status is inferred, not captured; the raw files' `→ 200` lines are that inference, logged as `→ 2xx` since), no
    error body; rows returned, identical on both deployments:

    | Probe (`GET /v2/observations`) | Rows | Field group served |
    |---|---|---|
    | `fields=core,io`, 13-day window | 1 of 1 | `input` |
    | `fields=core,io`, 14-day window | 1 of 1 | `input` |
    | `fields=core,io`, 15-day window (row 14.5 days old) | 1 of 1 | `input` |
    | `fields=core,io`, 30-day window (row 20 days old) | 1 of 1 | `output` |
    | `fields=core,io`, no window (row 20 days old) | 1 of 1 | `input` |
    | `fields=core,metadata`, 13-day window | 1 of 1 | `metadata` |
    | `fields=core,metadata`, 14-day window | 1 of 1 | `metadata` |
    | `fields=core,metadata`, 15-day window (row 14.5 days old) | 1 of 1 | `metadata` |
    | `fields=core,metadata`, 30-day window | 1 of 1 | `metadata` |
    | `fields=core,io`, `traceId`, no window | 1 of 1 | `input` |
    | `fields=core,io`, `traceId`, 15-day window (row 14.5 days old) | 1 of 1 | `input` |
    | `fields=core,io`, `filter` on `id`, no window | 1 of 1 | `input` |
    | `fields=core,io`, `filter` on `id`, 15-day window (row 14.5 days old) | 1 of 1 | `input` |
    | `fields=core,io`, 13-day window, no other filter, `limit=50` | 50 | `input` on all 50 |
    | `fields=core,io`, 13-day window, no other filter, `limit=51` | **51** | `input` on all 51 |

    The row-limit probes use exactly the combination the official MCP caps at 50: a date window, the `io` group, and
    no `traceId` or `id` filter. Verbatim output: `raw/2026-09-26-iowindow-probe-cloud.txt`,
    `raw/2026-09-26-iowindow-probe-selfhosted.txt`. `[verified]`
  - **Verdict: the 14-day / 50-row rule is the official MCP tool's own guard, not a REST limit.** Our workflow tools
    keep a guard of their own anyway (`.claude/rules/langfuse-api.md`, "Time windows"), for budget reasons. The probe
    stays as a weekly regression on Cloud and on self-hosted 4.46.0 `events_only` and `dual`
    (`.github/workflows/integration.yml`), so a guard added upstream fails the suite. It does not run on 3.x (§1.8:
    no `/v2/observations`, OTLP/JSON `traceId` bug), and no v3 variant is planned: Cloud retires v3 on 2026-11-16,
    and seeding v3 needs OTLP/protobuf (#85).
    The Cloud evidence above comes from the same test run locally with the Cloud test project's keys; the `cloud` job
    then passed on GitHub too (`workflow_dispatch` run 36239908860 on `main`, 2026-09-26, Langfuse **4.46.0**, every
    live test `PASS`, none skipped; #40). That run predates #41; the first green `cloud` run with the goleak ignores removed is
    `workflow_dispatch` run 36244125560 on `main` at `988b3d2` (2026-09-26, #51). `[verified]`
  - **Seeding limitation on Cloud.** Cloud accepted (200) an OTLP export of a span **40 days** old but did not serve it
    within 90 s (the 1- and 20-day spans of the same export were queryable within 45 s); self-hosted serves it. The probe therefore
    seeds nothing older than 20 days. Verbatim: `raw/2026-09-26-cloud-backdated-40d.txt`. `[verified]` Hypothesis, not
    tested: the Hobby plan's 30-day data access window. `[sourced — unverified]`
  - On Cloud, rows requested with `fields=core` came back without `name` (verbatim file above); the probe requests
    `core,basic` when it needs names. `[verified]`
  - `DELETE /api/public/traces` with `{"traceIds":[...]}` answers 200 `{"message":"Traces deleted successfully"}` on
    both deployments; the probe uses it for cleanup. `[verified]`
- **Unknown `fields` values are silently ignored** (200, default groups). `[verified]` §1.7

### 1.7 Observed behavior on self-hosted v4 (prototype, 2026-09-25) `[verified]`

Branch `prototype/langfuse-io-window`, `internal/workflows/prototype_iowindow/` (`RESULTS.md`, verbatim probe
output). Upstream `docker-compose.yml` at commit `fd5c9ee18e07`, images `:4` = **4.46.0**, headless init.

- **Seeding.** OTLP/HTTP JSON to `/api/public/otel/v1/traces` accepts back-dated spans (40 days) and keeps their
  start times. Rows are queryable about 10 s after the 200.
- **Stack cost.** Cold start to `/api/public/ready` 54 s (images cached); about 2.8 GiB RAM in total.
- **Stale images.** A cached `:4` image was 4.16.0 and lacked `/v2/evaluators` and `/v2/evaluation-rules`. Pin
  the image digest.
- **`events_only` mode.** A fresh v4 deployment runs in this mode. All 12 `deprecated: true` GETs in the spec
  return 404 `{"message":"This endpoint is not available on deployments running in Langfuse v4 events_only mode.
  ..."}`. This matches the ADR-0004 exclusion.
- **Version skew.** An operation the deployment does not have returns **404 with an HTML body**, like any unknown
  route. Cloud (4.46.0) answers 401 JSON for the same routes without auth.
- **Error body shapes vary**:
  - `{"message","error":"<Name>Error"}`
  - `{"message","code":"resource_not_found"}`
  - `{"message":"Invalid request data","error":[zod issues]}`
  - `{"error":"..."}` (organization routes)
  - SCIM `{"schemas":[...],"detail":...}`
- **Auth errors.**
  - 401 bodies: `No authorization header`, and `Invalid credentials. Confirm that you've configured the correct
    host.`
  - A project key on org-scoped routes gets 403 `Organization-scoped API key required for this operation.`
  - `experiments_list` and `experiment-items` return 400 without `fromStartTime`.


### 1.8 Old self-hosted: Langfuse 3.80.0 (prototype, 2026-09-25) `[verified]`

Same branch, `internal/workflows/prototype_iowindow/v3.80.0/`. Compose from tag `v3.80.0`, released 2025-07-09.

- **Operation availability.** Every spec operation was probed with every method.
  - **All 15 deprecated operations are present.**
  - **40 of the 102 non-deprecated operations are missing** and return an HTML 404: `/v2/observations`,
    `/v2/metrics`, `/v3/scores`, `/experiments`, `/experiment-items`, evaluators, evaluation rules, LLM connections,
    dashboards, blob-storage integrations, annotation-queue assignments, feedback, and organization API keys.
  - **Consequence.** Under ADR-0004's "exclude deprecated" rule, a v3 deployment exposes **no way to read traces,
    observations, sessions, scores or metrics**.
  - The docs agree: OSS v3 uses the legacy APIs, and v4 removes them. `[sourced]`
    `/self-hosting/upgrade/versioning` ("Public API & querying — OSS v3: use the legacy APIs `observations_v1` /
    `metrics_v1`; deprecated read APIs are removed on v4").
- **Legacy read limits.** `GET /observations` and `GET /traces` return input/output by default.
  - There is **no window enforcement**.
  - **`limit` max is 100**, against 1000 on v4's `/v2/observations`.
  - `GET /traces/{id}` embeds every observation, together with its input.
- **OTLP/JSON bug in 3.80.0.** A hex `traceId` is stored as the hex of its ASCII bytes. **OTLP/protobuf is correct.**
- **The upstream compose file for v3.80.0 no longer starts as shipped.**
  - `postgres:latest` now pulls PostgreSQL 18, which rejects the `/var/lib/postgresql/data` mount. Pin it with
    `POSTGRES_VERSION=17`.
  - The untagged `clickhouse-server` now pulls ClickHouse 26.9. With it, every timestamp is stored as
    `9999-12-31 23:59:59`. Pin `24.3`.
  - Any test matrix for old versions must pin every image.

### 1.9 Union catalog: what the release specs show (generator, 2026-09-26) `[verified]`

Source: `scripts/gen-union-catalog.py` (#71) over the OpenAPI spec of every plain `vX.Y.Z` release tag of
`langfuse/langfuse` from v3.0.0 to v4.46.0; output `internal/catalog/spec/langfuse-union-catalog.json`.
Per-family floors stay in `langfuse-api-versions.md`.

- **Size.** 399 release tags, 116 distinct specs, **137 operations** ever listed (method + path); 13 are ADR-0004
  exclusions. The 4.46.0 spec alone has 124.
- **An operation ID can name two paths.** `promptVersion_update` is `PATCH /v2/prompts/{promptName}/version/{version}`
  up to 3.17.x and `PATCH /v2/prompts/{name}/versions/{version}` from 3.18.0. The catalog keeps both, each with its
  range; an ID resolves to the newest operation that carries it.
- **Release specs are not monotonic.** `PATCH /v2/prompts/{name}/versions/{version}` is missing from the v3.28.2 spec
  only, and back in v3.28.3. The union range ignores such gaps; the generator prints them.
- **Operation IDs of operations that left the spec:** v1 `GET /scores` is `score_get` and `GET /scores/{scoreId}` is
  `score_get-by-id` (both removed in 3.53.0); `GET /metrics/daily` is `metrics_daily` (removed in 3.62.0). In older
  specs the v1 `GET /observations` was `observations_getMany`, the ID the v2 observations operation carries now; the
  catalog keeps the newest spec's IDs (`legacy_observationsV1_getMany`).
- **`deprecated: true` is not only the legacy family.** The 4.46.0 spec flags 15 operations: the 12 legacy
  GETs, `datasetRunItems_create`, `datasets_deleteRun` and `ingestion_batch` (the v4.12 deprecation, §1.7); the
  catalog puts them in the legacy family. `unstable/evaluators` and
  `unstable/evaluation-rules` were flagged in v4.23 and removed in v4.31: they are bounded by their range, not gated
  by a family.
- **Spec ranges match 3.80.0 at runtime.** Every operation the union puts in range at 3.80.0 answered a live 3.80.0
  (§1.8), and nothing out of range answered, except six paths that exist there for another method and answered 405:
  `POST /annotation-queues`, `PUT /models/{id}`, `DELETE /organizations/memberships`,
  `DELETE /projects/{projectId}/memberships`, `DELETE /v2/prompts/{promptName}`, `PATCH /score-configs/{configId}`.
- **Every operation of the 4.46.0 spec is routed on 4.46.0 `dual`** (124 of 124, `langfuse-api-versions.md` §1), so
  the 4.x `dual` profile resolves to all 111 non-excluded operations.

### 1.10 Served parameters: a version range per parameter (generator, 2026-10-10) `[verified]`

Source: `scripts/gen-union-catalog.py` (#173) over the release specs from v3.0.0 to v4.50.0. Each path and query parameter
of an operation carries the release that introduced it (`x-introduced`) and, when it left the spec while the operation
stayed, the release that removed it (`x-removed`). Before this, an operation kept the parameters of the last spec that
listed it, so a parameter a later release added was offered on an older deployment that ignores it (end-to-end campaign
finding H1, `raw/2026-10-10-e2e-langfuse-3.80.0.md`).

- **The three parameters of H1.** A 3.80.0 spec declares none of them; Langfuse 3.80.0 ignores each and answers the
  unfiltered data with no error.

  | Operation | Parameter | Introduced |
  |---|---|---|
  | `scores_get-many` (`GET /api/public/v2/scores`) | `traceId` | 3.133.0 |
  | `legacy_observationsV1_getMany` (`GET /api/public/observations`) | `level` | 3.82.0 |
  | `trace_get` (`GET /api/public/traces/{traceId}`) | `fields` | 3.166.0 |

  `scores_get-many` also gained `datasetRunId` (3.133.0), `sessionId` (3.112.0), `fields` (3.144.0), `observationId`
  and `filter` (3.153.0); 23 parameters of 12 operations were introduced after their operation.
- **No parameter has left a spec while its operation stayed** (v3.0.0 to v4.50.0): no `x-removed` on any parameter. The
  generator and the catalog still support it, proven on minimal specs.
- **A spec error is dropped.** Releases 3.57.1 to 3.78.x list the response schema of `datasetRunItems_list` as a
  required query parameter named `response`. The generator drops a parameter whose schema is an object, since a URL
  carries none; kept, it would be a required parameter of the operation on those deployments.
- **A change of a parameter's type or enum between releases is not tracked**: the definition of the newest release that
  lists the parameter applies.

- **What the filter scan relies on** (#183, live on Langfuse 3.80.0, 2026-10-10). Both list routes answer
  `{"data": [...], "meta": {"page", "limit", "totalItems", "totalPages"}}`; an empty list has `totalItems` 0 and
  `totalPages` 0. A score of `GET /api/public/v2/scores` carries `traceId`, an observation of
  `GET /api/public/observations` carries `level` (`ERROR` for an observation ingested with that level). Both ignore
  the filter they do not declare, so the unfiltered list comes back. `TestLiveLangfuse3800AFilterScanOfScoresByTraceReturnsOnlyThatTracesScores`
  and `TestLiveLangfuse3800AFilterScanOfLegacyObservationsByLevelReturnsOnlyThatLevel` seed rows through the ingestion
  batch and read them back through the scan.

### 1.11 Dataset runs routes with a Folder name (live test, 2026-10-10) `[verified]`

Source: `TestLiveLangfuseServesTheRunsOfADatasetReadByItsFolderName` and
`TestLiveLangfuseAnswersOperationUnavailableOnTheDatasetRunsRoutesWhereTheLegacyFamilyIsOff`
(`internal/server/foldername_integration_test.go`, #181), run on each pinned deployment. The test creates a dataset
named `it-folder-<id>/runs-dataset`, one item and one run item of the run `it-run-<id>` straight through Langfuse, then
reads the runs through `execute_read` (`datasets_getRuns`, `datasets_getRun`, the Folder name sent as `%2F`) and
sends `DELETE` straight to the route `datasets_deleteRun` and `datasets_getRun` share (the operation itself is not run
through `execute_write` here: that cycle is live-tested on 4.46.0 only). It replaces the statement, kept in the
Folder-name hint and two docs since #33, that these routes fail for Folder names in Langfuse itself
(langfuse/langfuse#13933; a Langfuse maintainer answered there on 2026-07-15 that the OSS route supports the encoded
name, and the issue was still open on 2026-10-10). The report rested on one reporter, not on a deployment.

| Deployment | `datasets_getRuns` | `datasets_getRun` | `DELETE` of the run |
|---|---|---|---|
| 3.80.0 | 200, the one run of that dataset | 200, run and its run item | 200 |
| 3.225.11 | 200, the one run of that dataset | 200, run; its run item after about 6 s | 200 |
| 4.46.0 `events_only` | 404, the legacy family is off | 404 | 404 |
| 4.46.0 `dual` | 200, the one run of that dataset | 200, run; its run item after about 8 s | 200 |

- **"Belongs to that dataset"** means the run's `datasetId` is the ID Langfuse returned when it created the dataset and
  its `datasetName` is the Folder name; `datasets_getRun` also lists the run item of the item created.
- **4.46.0 `events_only` is no Folder-name failure.** The three routes are legacy-family routes. Langfuse answers 404
  to all three for a dataset that exists, and the executor reports `operation_unavailable` (the test asserts the code
  and the HTTP status 404). The live test session runs under an unknown profile (every family on), so the request does
  reach Langfuse; a shipped server that detected the profile would refuse before any request, because the resolved
  catalog of `events_only` offers neither read operation (ADR-0012; `internal/catalog/resolve_test.go`, "4.x
  events_only").
- **Run items are listed late on 3.225.11 and 4.46.0 `dual`.** A run item created with `POST /dataset-run-items` is
  listed by `GET /datasets/{name}/runs/{run}` only after a delay: the `datasets_getRun` subtest took 6.1 s on 3.225.11
  and 8.1 s on `dual`, 0.01 s on 3.80.0. The run itself is listed at once (`datasets_getRuns` took 0.02 s or less on
  every deployment that serves it). The test polls for the run item for up to 90 s. The delay was not compared with a
  dataset without folders, so it is not claimed to be Folder-name related.
- **A run item needs no trace.** `POST /dataset-run-items` accepted a `traceId` that no trace carries, on every
  deployment, so the test seeds none.

### 1.12 The legacy observations list as the source of a trace tree (live on 3.80.0, 2026-10-11) `[verified]`

Source: `TestLiveLangfuse3800GetTraceTree*` (`internal/server/tracetree_legacy_integration_test.go`, #187), and direct
`GET /api/public/observations` calls to the pinned self-hosted 3.80.0 after it ingested the seeded traces; the
operation is `legacy_observationsV1_getMany`. API reference `GET /api/public/observations` (`langfuse-docs` MCP,
2026-10-11) for the shape `[sourced]`.

- **`traceId` filter.** The union catalog serves `traceId` on this operation from 3.0.0 (`x-introduced`), and 3.80.0
  honours it: a call with `traceId` returned only that trace's rows (4 of the project's 8 observations). The campaign
  of 2026-10-10 saw the same (`raw/2026-10-10-e2e-langfuse-3.80.0.md`, H1). `level` is not served there (§1.10).
- **Page size cap.** `limit` above 100 is a 400: `limit=101` answered `{"message":"Invalid request data","error":[{"origin":"number","code":"too_big","maximum":100,"inclusive":true,"path":["limit"],…}]}`.
  The documentation table gives the same, v1 maximum 100 against 1,000 for v2 `[sourced]`.
- **`meta`.** `{"page", "limit", "totalItems", "totalPages"}`, accurate: a trace of 130 observations gave
  `totalItems` 130 and `totalPages` 2 on pages 1 and 2 (100 rows, then 30). A trace that does not exist is a 200 with
  `data: []` and `totalItems` 0, `totalPages` 0 (also on page 999), not a 404. There is no cursor.
- **Order.** Newest first (`startTime` descending): the first row of page 1 was the last observation started, the
  last row of page 2 the first. The trace tree sorts the rows itself, so it does not depend on this.
- **Row fields.** An observation row carries, besides `id`, `traceId`, `parentObservationId`, `type`, `name`,
  `level`, `statusMessage`, `startTime`, `endTime`, `version`, `environment`, `projectId`, `createdAt` and `updatedAt`:
  `input`, `output`, `metadata`, `model`, `modelId`, `modelParameters`, `promptId`, `promptName`, `promptVersion`,
  `completionStartTime`, `timeToFirstToken`, `latency`, `usage`, `usageDetails`, `costDetails`, `unit`,
  `promptTokens`, `completionTokens`, `totalTokens`, `inputPrice`, `outputPrice`, `totalPrice`,
  `calculatedInputCost`, `calculatedOutputCost` and `calculatedTotalCost`; 39 keys, null where Langfuse has no value.
  There is no field selection: `input`, `output` and `metadata` come with every row. **Without them a row is about
  870 bytes** as Langfuse sends it, so with every key kept about 75 rows fit the default 64 KiB result size cap before
  rows are left out at a row boundary (a 130-observation trace read through `get_trace_tree` on 3.80.0 counted 130 in
  `meta.observations` and returned 76 rows, #187).
- **Which keys are `null` (#200).** One row of a span with no model, usage or cost was recorded from the pinned 3.80.0
  (2026-10-11; `internal/server/testdata/legacy-observation-3.80.0.json`, 865 bytes, keys as test placeholders). 18 of
  its 39 keys are `null`: `parentObservationId`, `statusMessage`, `version`, `input`, `output`, `modelParameters`,
  `completionStartTime`, `promptId`, `model`, `promptName`, `promptVersion`, `timeToFirstToken`, `modelId`,
  `inputPrice`, `outputPrice`, `totalPrice`, `calculatedInputCost` and `calculatedOutputCost`. A count of `0` is not
  `null`: `promptTokens`, `completionTokens`, `totalTokens` and `calculatedTotalCost` are `0`, `metadata`,
  `usageDetails` and `costDetails` are `{}`, `usage` is `{"unit":"TOKENS","input":0,"output":0,"total":0}`, and
  `latency` is `1000` for the 1-second span. `get_trace_tree` leaves out the top-level keys that are `null`, keeping
  `id`, `parentObservationId` and `startTime` (ADR-0002 amendment of 2026-10-11): the recorded row is **527 bytes** in
  the result (key order sorted, `depth` added, `input`, `output` and `metadata` removed), so **124 such observations
  fit the default cap whole**; of 130 the tree returns 123 rows, against 78 before the rule. The live
  130-observation trace of `TestLiveLangfuse3800GetTraceTreeReadsASecondPageOfTheLegacyList` (spans with a start time
  and no end time, so `endTime` and `latency` are `null` too) returns **125 of 130** rows, against 76. So 130
  observations do not fit the default cap either way; the result is marked truncated, `meta.observations` says 130
  and the hint says how many rows were kept.
- **Latency unit.** An observation of 1 s (`startTime` and `endTime` one second apart) has `latency` **1000** in this
  list, and one of 2 s has 2000: **milliseconds**. The API reference documents the v2 `latency` filter and field in
  seconds `[sourced]`, and §6 row 12 says the legacy observation schema also uses seconds; on 3.80.0 the observation
  rows disagree with that. The trace's own `latency` (`GET /api/public/traces/{traceId}`) is in seconds (128 for a
  trace whose observations were started over about two minutes), as the campaign recorded ("trace `latency` is in seconds and observation
  `latency` in milliseconds"). The trace tree does not convert: a row is what the deployment's Langfuse returns, and
  the tool description names both units.

### 1.13 Behaviours of Langfuse the server relays unchanged, seen on 3.80.0 and checked (2026-10-11) `[sourced]`

Source: the "Low" findings of the end-to-end campaign on a self-hosted 3.80.0 (`raw/2026-10-10-e2e-langfuse-3.80.0.md`,
#188). The server relays each of them as Langfuse sends it: correcting one would mean inventing data. Before they were
written down here each was checked against the OpenAPI spec of the `v3.80.0` tag (`web/public/generated/api/openapi.yml`;
the `v4.50.0` spec for what changed since), the Langfuse docs (`langfuse-docs` MCP, 2026-10-11) and, where those are
silent, the Langfuse source at the `v3.80.0` tag (paths below are in `langfuse/langfuse`). None was run again against a
live Langfuse: the observations are the campaign's. **Confirmed** means the spec, docs or source agree with what the
campaign saw. **Contradicted** means the spec or the docs say otherwise: the row then gives what the source and any
live measure show, and the behaviour stays recorded as seen on 3.80.0, not as a documented guarantee.

| Seen on 3.80.0 | What the check found | Verdict |
|---|---|---|
| `meta.totalItems` over-counts after a record is written twice with the same ID (2 for 1 row) | Spec: `utilsMetaResponse.totalItems` is the "number of total items given the current filters/selection (if any)"; duplicates are not mentioned. Source, `GET /api/public/traces` (`web/src/pages/api/public/traces/index.ts`): the count is `SELECT count() … FROM traces` (`getTracesCountForPublicApi`, `web/src/features/public-api/server/traces.ts`) with no deduplication, while `generateTracesForPublicApi` deduplicates the rows it lists (`FROM traces t FINAL`, or `LIMIT 1 BY t.id, t.project_id` when a skip-index filter is used, or an aggregating table on its newer execution path). The campaign does not name the list it saw this on; only the traces list was read | Confirmed for the traces list: the count includes row versions that the list collapses into one. The other lists were not checked |
| `trace_delete` of a trace that does not exist answers 200 `Trace deleted successfully` | Spec: the response is `{message}`; the spec lists a 404 for every operation and says nothing about a missing trace. Source (`web/src/pages/api/public/traces/[traceId].ts`): the `DELETE` handler writes an audit entry, adds a job to the trace delete queue and returns that message; it never looks the trace up. Docs (`/docs/administration/data-deletion`): trace deletion is asynchronous and "there is no deletion confirmation or notification; to verify that your data got deleted, query it again" | Confirmed. The message means a job was queued, for a trace that exists and for one that does not; a deleted trace can stay readable for a while |
| A 403 for an organization-scoped operation says `Invalid API key` | Spec: the 403 response is declared with no description or schema. Docs: no docs page states the text. Source: every organization-scoped route of 3.80.0 (projects, organizations, memberships, API keys, SCIM) answers 403 `Invalid API key. Organization-scoped API key required for this operation.`; the projects route of `v3.225.11` and the source on `main` (found through the `langfuse-docs` search) have the same text. The campaign record quotes the first sentence only. §1.7 recorded, on 4.46.0, the second sentence alone, from a route it does not name; that was not compared | Confirmed, with the full text. The key is valid but project-scoped: the first sentence misleads, the second names the cause |
| `prompts_list` returns both `meta` and `pagination` | Spec: `PromptMetaListResponse` declares `data` and `meta` only (3.80.0 and 4.50.0). Source (`web/src/features/prompts/server/actions/getPromptsMeta.ts`): the action returns `meta` and an identical `pagination` object (`page`, `limit`, `totalPages`, `totalItems`), kept "for backwards compatibility as we initially released the /v2/prompts endpoint with this structure which did not match the api spec" (langfuse/langfuse#2068) | Confirmed, and on purpose; `pagination` is undeclared in the spec. Both objects hold the same four numbers |
| A trace's `latency` is in seconds and an observation's `latency` in milliseconds | Trace: the spec says "Latency of trace in seconds" (`TraceWithDetails.latency`), the traces list divides `latency_milliseconds` by 1000, and §1.12 measured seconds on a trace. Observation: the spec says "The latency in seconds" (`ObservationsView.latency`), but `GET /api/public/observations` builds it in `convertObservation` (`packages/shared/src/server/repositories/observations_converters.ts`) as `endTime − startTime` in milliseconds, and §1.12 measured 1000 for an observation of one second | Trace unit: confirmed. Observation unit: **contradicted** by the spec, which says seconds; the source and §1.12 say milliseconds. For the legacy list on 3.80.0 read the spec's unit as wrong. Other releases of the legacy list were not checked |
| `comments_get` returns no `meta`, and its `objectType` has no enum | Spec: `GetCommentsResponse` requires `data` and `meta` (`utilsMetaResponse`), in 3.80.0 and in 4.50.0; `objectType` is a plain string whose description lists `trace, observation, session, prompt`. Source (`web/src/pages/api/public/comments/index.ts`): the `GET` handler returns `{ data: comments }`, and its response schema (`GetCommentsV1Response`, strict) allows `data` only, so there is no pagination metadata on 3.80.0; the query schema validates `objectType` against the enum `CommentObjectType` (`TRACE`, `OBSERVATION`, `SESSION`, `PROMPT`, upper case; `packages/shared/prisma/schema.prisma`) | `meta` absent: **contradicted** by the spec, which requires it; confirmed by the source. Page until a page comes back empty. `objectType` without an enum: confirmed in the spec; the route takes only the upper-case names (read in the source, not run), although the spec's description lists them in lower case. Later releases (4.50.0 sends the list through `listCommentsForApi`) were not checked |

The server's side of these is documented in the [tools reference](../reference/tools.md): `meta.totalItems` and the two
latency units under "Calling an operation", the rest under "Known limits".

---

### 1.14 What the body of `comments_create` and `scores_create` needs (release specs and sources, 2026-10-11) `[verified]`

Source: the "Low" findings of the end-to-end campaign on a self-hosted 3.80.0 (`raw/2026-10-10-e2e-langfuse-3.80.0.md`,
split out of #188 into #192): an agent learned from a Langfuse 400 where the `projectId` of a comment comes from and
that a score needs a target. The server now says both in `describe_operation` (write mode), as static guidance. The facts
below were checked before the text was coded: the OpenAPI specs at the tags `v3.0.0`, `v3.30.0`, `v3.35.0`, `v3.40.0`,
`v3.45.0`, `v3.50.0`, `v3.51.0`, `v3.51.1`, `v3.52.0`, `v3.53.0`, `v3.54.0`, `v3.54.1`, `v3.55.0`, `v3.60.0`, `v3.80.0`,
`v3.100.0`, `v3.112.0` and `v3.133.0` (`web/public/generated/api/openapi.yml` of `langfuse/langfuse`), the source at the
tags named in the rows (paths are in `langfuse/langfuse`), the Langfuse docs (`langfuse-docs` MCP) and the union catalog
(4.50.0). None was run again against a live Langfuse: the 400s are the campaign's.

| Fact | Evidence | What the server does with it |
|---|---|---|
| `projectId` of `CreateCommentRequest` is required, and the spec describes it only as "The id of the project to attach the comment to." | Spec: `required` is `projectId`, `objectType`, `objectId`, `content` at every tag above from `v3.0.0` to `v3.133.0` and in the union catalog | The body schema already says it is required; the guidance says what to send |
| A comment is stored under the project of the API key, whatever `projectId` the body holds | Source at `v3.80.0` (`web/src/pages/api/public/comments/index.ts`): the `POST` handler creates the comment with `data: { ...body, id: v4(), projectId: auth.scope.projectId }`. Source at `v3.225.11`, `v4.46.0` and `main` (`web/src/features/comments/server/publicCommentService.ts`, `createCommentForApi`): the comment is created with `projectId: auditScope.projectId`, the key's project, and the body's `projectId` is overwritten before the reference object is validated. The source of the tags between `v3.0.0` and `v3.79.x` was not read | The guidance says `projectId` is the ID of the project the server's keys belong to |
| `projects_get` (`GET /api/public/projects`) returns the project of the API key, with its `id` | Spec summary "Get Project associated with API key" (`v3.80.0`; the union catalog adds "(requires project-scoped API key)"); response `Projects` = `{ data: [Project] }`, `Project` = `{ id, name, metadata, retentionDays }` (`v3.80.0`). `projects_get` is in every release since `v3.0.0` | The guidance names `projects_get` as the operation that returns that project, and its `id` as the value, only when the resolved catalog offers `projects_get` |
| `CreateScoreRequest` requires a `traceId` and has no `sessionId` or `datasetRunId` up to `v3.52.0` | Spec: `required` is `traceId`, `name`, `value` and the properties lack both at `v3.0.0`, `v3.30.0`, `v3.35.0`, `v3.40.0`, `v3.45.0`, `v3.50.0`, `v3.51.0`, `v3.51.1` and `v3.52.0` | Before 3.53.0 the guidance names `traceId` (with an optional `observationId`) as the target |
| `sessionId` joins `CreateScoreRequest` in `v3.53.0` (`required` becomes `name`, `value`) and `datasetRunId` in `v3.55.0` | Spec at `v3.52.0` (neither), `v3.53.0` and `v3.54.0`/`v3.54.1` (`sessionId` only), `v3.55.0` (both) | From 3.53.0 the guidance names `sessionId` too, from 3.55.0 `datasetRunId` too |
| A score needs exactly one target, and `observationId` needs `traceId` | Source `packages/shared/src/utils/scores.ts`. `v3.53.0` and `v3.54.1`: valid only with a `traceId` and no `sessionId`, or a `sessionId` and neither `traceId` nor `observationId`; message "Either provide traceId (with optional observationId) or sessionId, but not both. ObservationId requires traceId." `v3.55.0`, `v3.80.0`, `v3.112.0`, `v3.225.11`, `v4.46.0`, `v4.50.0` and `main`: the same with a `datasetRunId` as a third target; message "Provide exactly one of the following: traceId (with optional observationId), sessionId or datasetRunId. ObservationId requires traceId." Docs `/docs/evaluation/evaluation-methods/scores-via-sdk`: a trace or observation score needs `traceId` (an observation score gives both IDs), a session score gives only `sessionId`. Langfuse answers 400 (the campaign on 3.80.0; langfuse/langfuse#9323 shows the same message on a self-hosted 3.112.0) | The guidance says a score needs exactly one target (a single target before 3.53.0, where `traceId` is required), names the target properties the body schema and the deployment's version have (`traceId`, `sessionId`, `datasetRunId`; `observationId` with `traceId`), and says a body with none, or more than one, is refused with HTTP 400 |

The unknown that remained here (#192) was that the body schema of every write operation was the newest release's, not the
deployment's, so `describe_operation` returned and `execute_write` checked a schema with `sessionId` and `datasetRunId` and no
required `traceId` for a deployment before 3.55.0. It is closed by #202, below.

#### Request bodies change between releases (generator over v3.0.0 to v4.50.0, 2026-10-11) `[verified]`

Source: `scripts/gen-union-catalog.py` (#202) over the OpenAPI spec of the 403 release tags from v3.0.0 to v4.50.0 (118 distinct
specs, the set the committed catalog was built from), `$ref`s inlined from each spec and converted to JSON Schema 2020-12.
Compared by `required` flag and schema, the `description` and `title` annotations apart. Read from the specs; none was run
against a live deployment.

- **Of the 35 write operations the server offers, 15 have more than one distinct body: 76 bodies in all** (the 17 operations
  the server excludes keep one each). `scores_create` has 11, `models_create` 5 (7 spans), `prompts_create` 4 (6 spans),
  `datasets_create` 3, `comments_create` 2, `scoreConfigs_create` 2, `evaluators_create` and `evaluators_update` 2 each, and the
  three `unstable/` operations that left the spec in 4.31.0 up to 6.
- **`scores_create`** (`POST /api/public/scores`, `score_create` in the oldest specs): `traceId`, `name` and `value` are required
  up to 3.52.0; `environment` joins in 3.35.0, `metadata` in 3.49.0; **3.53.0** adds `sessionId` and requires only `name` and
  `value`; **3.55.0** adds `datasetRunId`; `queueId` in 3.118.0; `source` in 3.171.0. Spans: 3.0.0, 3.35.0, 3.49.0, 3.53.0,
  3.55.0, 3.118.0, 3.131.0, 3.143.0, 3.165.0, 3.171.0 and 3.188.0 each start a body.
- **`models_create`** (`POST /api/public/models`): `unit` is required in the body of 3.0.0 to 3.7.x, **optional and nullable
  (`"unit": null`) from 3.8.0 to 4.18.x**, required again from 4.19.0. The `PUT /models/{id}` body (4.19.0 on) requires it.
  `pricingTiers` joins in 3.135.0.
- **The release specs are not monotonic for bodies either.** The spec of v3.28.2 has the 3.0.0 bodies of `models_create` and
  `prompts_create` (it also lacks one operation, §1.9): the generator stores each such body once, with both spans.
  `comments_create` gains `objectStartTime` in 4.33.0.
- **Changes below the top level**: `prompts_create` (inside its `oneOf`), `scoreConfigs_create` (from 3.165.0),
  `annotationQueues_createQueueItem` (from 3.121.0), `datasets_create` (4.45.2), `evaluators_update` (a message
  `role` constant becomes an enum in 4.27.0), `unstable_dashboardWidgets_update` (the `view` enum gains `scores-boolean` in
  3.224.0).
- **No older body has a property named like a credential** (`secret`, `password`, `accessKey`), so no exclusion follows. The
  excluded operations that take one have 43 bodies; the generator keeps only the newest of each.
- **The bodies of the first releases use the OpenAPI 3.0 `example` keyword** (`POST /ingestion` and `POST /scores` in 280
  releases each, three `unstable/` operations in 23): the generator converts it to `examples: [value]`.
- **Size.** The embedded catalog was 504,215 bytes and is 955,700 with the history (2,072,165 if the excluded operations kept
  theirs too). The ADR-0012 amendment of #202 has the decision.

The server now resolves one body per write operation for the detected version, the body of the newest release that is not newer
than the deployment (the newest when no version was detected), and the `scores_create` guidance above names a target property
only when that body has it.

### 1.15 The shape of `filter` per operation (release specs 4.42.0 and 4.46.0, API reference, 2026-10-11) `[verified]`

Source: ticket #195 (a JSON escape of a hidden character inside a `filter` reached Langfuse unchecked, found while
the text checks of #179 were built). The server now decodes, checks and re-encodes a `filter` that
is a JSON text, so which `filter` parameters are JSON had to be known per operation, not guessed from the name. Checked
against the `description` of the parameter in the release specs `v4.42.0` and `v4.46.0`
(`web/public/generated/api/openapi.yml` of `langfuse/langfuse`; the union catalog keeps no parameter description) and the
API reference (`langfuse-docs` MCP, `api.reference.langfuse.com`). None was run against a live Langfuse.

| Operation | `filter` served from (union catalog) | What the 4.46.0 release spec says | The server reads it as |
|---|---|---|---|
| `trace_list` | 3.116.1 | "JSON string containing an array of filter conditions" | a JSON filter |
| `legacy_observationsV1_getMany` | 3.120.0 | the same | a JSON filter |
| `observations_getMany` | 3.141.0 | the same | a JSON filter |
| `scores_get-many` | 3.153.0 | "A JSON stringified array of filter objects" | a JSON filter |
| `experiments_list`, `experiments_listItems` | 3.206.0 | "JSON string containing an array of structured filter conditions" | a JSON filter |
| `prompts_list` | 4.42.0 | "JSON string containing an array of filter conditions, combined with AND" (finding below) | a JSON filter |
| `scim_listUsers` | 3.50.0 | "Filter expression (e.g. userName eq "value")": a SCIM filter | an ordinary query parameter value |

The `prompts_list` finding. The release specs from `v4.42.0` (identical text at `v4.46.0`) give `GET /api/public/v2/prompts` a
`filter` parameter: a JSON string of filter conditions combined with AND, which "takes precedence over name, version, label,
tag, fromUpdatedAt, and toUpdatedAt", where "an empty array applies no filters". A condition needs `type`, `column`,
`operator` and `value`; the types and columns the spec lists are `string` and `stringOptions` on `id`, `name` and `type`,
`number` on `version`, `datetime` on `createdAt` and `updatedAt`, `arrayOptions` on `labels` and `tags`, and
`stringObject` on `config` (with a `key`). The spec adds that a backslash escapes `%` and `_` in the search operators and
that the backslash is JSON-escaped in the filter value: the server reads that as an escaped backslash, ordinary text, and
sends it as it was written. The current API reference lists no `filter` for the operation (`name`, `label`, `tag`, `page`,
`limit`, `fromUpdatedAt` and `toUpdatedAt` only), so the reference lags the release spec (section 6, row 14). The earlier
reading that its meaning was unverified (#141) is settled: it is a JSON filter, and a plain word such as `billing` is no
valid value for it. The static guidance of `describe_operation` on `filter` (it is not the tag filter, send `tag`) is
unchanged.

What the server does with a JSON filter: a string holding the JSON text of one list, at most 16384 bytes and 10 levels
deep, with no control or invisible character in the raw text or in any decoded string or key; the decoded list is sent
written again as compact JSON (order of conditions and list items kept, numbers as written, object keys sorted, a
duplicated key once with its last value). The grammar (types, columns, operators, required keys) stays Langfuse's.

### 1.16 Two `date-time` query parameters that bound no time window (release specs 4.33.0 and 4.46.0, API reference, 2026-10-11) `[verified]`

Source: ticket #204 (the hints of #203 told an agent to use "a shorter time window" for an operation whose only
`date-time` parameter is none). Fourteen operations of the union catalog have a query parameter with the format
`date-time`. Twelve take a `from...`/`to...` pair that bounds the time range of a list: `fromStartTime`/`toStartTime`
(`legacy_observationsV1_getMany`, `observations_getMany`, `experiments_list`, `experiments_listItems`),
`fromTimestamp`/`toTimestamp` (`trace_list`, `sessions_list`, `scores_get-many`, `scoresV3_getManyV3`, `score_get`,
`metrics_daily`) and `fromUpdatedAt`/`toUpdatedAt` (`prompts_list`, `unstable_skills_list`). The other two bound no window:

| Operation | Parameter (served from) | What Langfuse says | What it means for a hint |
|---|---|---|---|
| `legacy_observationsV1_get` | `startTime` (4.33.0) | "The start time of the observation (ISO 8601 with offset, e.g. 2024-01-01T00:00:00Z). Optional performance hint - when provided, Langfuse narrows the lookup to make the request substantially faster. It only affects speed - an incorrect or omitted value never changes the result." Read from the `description` of the parameter in the release specs `v4.33.0` and `v4.46.0` (`web/public/generated/api/openapi.yml` of `langfuse/langfuse`; the union catalog keeps no parameter description). The API reference page of the operation (`api.reference.langfuse.com`) lists only `observationId`. | One observation is read by its ID: there is no list, no `limit`, no `fields` and no paging, and `startTime` never makes the answer smaller. The operation has no way of narrowing. |
| `datasetItems_list` | `version` (3.153.0) | API reference: "ISO 8601 timestamp (RFC 3339, Section 5.6) in UTC (e.g., "2026-01-21T14:35:42Z"). If provided, returns state of dataset at this timestamp. If not provided, returns the latest version. Requires datasetName to be specified." | A point in time: an earlier timestamp is another state of the dataset, not a smaller page. The ways of narrowing the operation has are `limit` and `page`. |

The server keeps two closed lists in `internal/catalog` (`timeWindowParams`, the six parameter names above, and
`notTimeWindows`, the two `(operationId, parameter)` pairs of the table); a test fails, naming the operation and the
parameter, when a `date-time` query parameter of the union catalog is on neither or on both, so the next release's
snapshot or lookup parameter is classified by a person. An operation has a time window when the deployment's version
serves at least one parameter of the first list. The value check of a `date-time` parameter (RFC 3339, before any request)
covers all fourteen operations, a window or not.

### 1.17 Rows written through the legacy ingestion API can reach Observations v2 late, or not at all (live on self-hosted 4.46.0 `dual`, 2026-10-11) `[verified]` for the observations, `[hypothesis]` for the cause

Source: the end-to-end campaign on a self-hosted Langfuse 4.46.0 `dual` (`raw/2026-10-11-e2e-langfuse-4.46.0-dual.md`,
finding H2): the Langfuse API straight, the ClickHouse tables and the worker log of that stack. One run on one stack;
nothing here was measured on Langfuse Cloud, on 4.46.0 `events_only` or on 3.x.

- **What was seen.** The seed posted 40 traces (303 observations) through the legacy ingestion API. Afterwards the legacy
  list counted 303 observations and Observations v2 counted 180. `GET /api/public/v2/observations?traceId=…` returned 0
  rows for `seed-trace-001` to `-020`, and for `-021` to `-040` the legacy count plus one (the root row Langfuse adds,
  `t-<traceId>`). `trace_get` and `legacy_observationsV1_getMany` held the 20 lost traces whole. ClickHouse held 303 rows
  in `observations`, 343 in the staging partition `2026-10-11 19:30:00` of `observations_batch_staging` and 180 in
  `events_core`. The worker log said `[DUAL WRITE] Processing partition 2026-10-11 19:30:00 for events table fill`,
  then `Successfully propagated`, then every minute `No partitions available for processing`: the job ran once and
  never came back to that partition, so it was not lag. The tree of an affected trace stayed empty 23 minutes after the
  seed and from then on.
- **What reached Observations v2 late.** Two probe traces ingested directly at 19:54:58 (`e2e-probe-trace-1` and `-2`)
  had no Observations v2 row at 20:02:24, were propagated when the worker processed their partition at 20:05:00, and had
  all their rows at 20:07:59: fresh data reaches Observations v2 whole, about ten minutes after ingestion.
- **Cause.** A hypothesis, seen once: the seed was posted 15 seconds after the Langfuse worker started, and a start-up
  race may have lost its first rows. Whether a second load on a fresh stack loses rows again was not measured.
- **Dataset runs created through the legacy API (campaign finding M2).** `[verified]` once, on one stack, not re-run for
  the index note. On the same 4.46.0 `dual`, `experiments_list` and `experiments_listItems` with `fromStartTime`
  `2026-10-01T00:00:00Z` returned `{"data":[],"meta":{}}`, while `datasets_getRuns` returned the two runs of
  `support/eval/triage-cases` (source: `raw/2026-10-11-e2e-langfuse-4.46.0-dual.md`, M2). Dataset runs created through
  the legacy API are not experiments on that deployment, which is a model difference of Langfuse, not a lag. Nothing
  was measured on Langfuse Cloud or on 4.46.0 `events_only`.
- **What it means for the server.** During the roughly ten minutes after any ingestion through the legacy API, a
  deployment that serves both read families shows an Observations v2 that lacks what the legacy reads hold, and in the
  run above it never caught up for 20 traces. The tree is read from Observations v2 whatever the legacy family's state
  (ADR-0002 amendment: the source is chosen once, from the deployment profile), so the server cannot fall back from a
  Langfuse answer. It says so instead: the hint of an empty tree on such a deployment names the legacy reads that may
  hold the trace, and the tool description says Observations v2 can lack observations the legacy ingestion API sent. A
  tree that holds some of a trace and not all of it shows no sign of the gap.
  The operation index says the same where it can: on a deployment that serves both families, the ten legacy reads whose
  preferred operation belongs to another operation family (`observations_getMany`, `metrics_metrics`,
  `experiments_list`, `experiments_listItems`) carry a note that the preferred operation can return less, or nothing,
  and the trace reads hint of a search about traces names the legacy reads too (`docs/reference/tools.md`). The note
  states a possibility: once the dual write has caught up the preferred operation may hold everything.

---

## 2. Deprecations

All deprecated routes are served on **Langfuse Cloud until 2026-11-16**, which is the v4 cutover. They get security
patches only. `[sourced]` `/faq/all/deprecated-api-migration#endpoints`, `/docs/compatibility`

| Deprecated | Replacement | Gotchas |
|---|---|---|
| `GET /observations`, `/observations/{id}` | `GET /v2/observations` | `page` becomes `cursor`. `limit` max rises from 100 to 1,000. By-id becomes a `filter` on `id`. |
| `GET /traces`, `/traces/{id}` | `GET /v2/observations?traceId=…`, grouped by `traceId` | **v4 has no trace-level input/output**: rebuild it from the root observation. `name` becomes a `traceName` filter. `tags` becomes an `arrayOptions` filter. `orderBy` is gone. Request `fields=…,trace_context`. |
| `GET /sessions`, `/sessions/{id}` | `GET /v2/observations` with a `sessionId` filter | Returns 404 on v4. `sessionId` can be filtered but **not grouped** in Metrics v2. |
| `GET /metrics`, `/metrics/daily` | `GET /v2/metrics` | The `traces` view is removed: use `observations` plus `isRootObservation=true` to count traces. Grouping by `userId`, `sessionId`, `id` or `traceId` is not allowed. `row_limit` defaults to 100. |
| `GET /scores` (v1), `/scores/{id}`, `GET /v2/scores`, `/v2/scores/{id}` | `GET /v3/scores` | Returns 404 on v4. One typed `value`. `datasetRunId` becomes `experimentId`. `userId`, `traceTags` and JSON `filter` are removed. No trace join. v3 can return **more** rows than v2, because v2 dropped scores whose trace was missing. |
| `GET /datasets/{name}/runs[/{runName}]`, `GET /dataset-run-items` | `GET /experiments`, then `GET /experiment-items` | Queried by dataset **ID**: resolve it with `GET /v2/datasets/{name}` first. `fromStartTime` is required. |
| `DELETE /datasets/{name}/runs/{runName}` | none | Workaround: list the items, then `DELETE /traces` in batches of ≤1,000. This also deletes the traces' observations and scores. |
| `POST /dataset-run-items` | experiment runner SDK, or OTel with experiment attributes | The SDK runners still call it internally. |
| Trace and observation events on `POST /ingestion`; `POST /traces`, `/spans`, `/generations`, `/events` | `POST /otel/v1/traces` (OTLP/HTTP) with `x-langfuse-ingestion-version: 4` | **Legacy trace ingestion is shut down on Cloud on 2026-11-16.** `score-create` and `sdk-log` events on `/ingestion` stay supported. |
| `GET /api/public/datasets`, `/datasets/{name}` (v1) | `/v2/datasets[/{name}]` | Replaced independently of the dataset-run migration. |
| Trace-level LLM-as-judge evaluators | observation-level evaluators | Stop running on Cloud on 2026-11-16. `[sourced]` `/docs/compatibility` |

Source for the rows: `[sourced]` `/faq/all/deprecated-api-migration` (quick reference plus the per-endpoint sections).

---

## 3. Data model glossary

| Term | One-line definition | Source |
|---|---|---|
| Observation | One step of an app (LLM call, tool call, retrieval, …). Can be nested. OTel spans become observations. Stored as **one wide table** that carries copies of trace attributes. | `[sourced]` `/docs/observability/data-model` |
| Observation types | `SPAN`, `GENERATION`, `EVENT`, `AGENT`, `TOOL`, `CHAIN`, `RETRIEVER`, `EVALUATOR`, `EMBEDDING`, `GUARDRAIL` (10 in total). | `[sourced]` `/docs/glossary`; spec `ObservationType` |
| Level | `DEBUG`, `DEFAULT`, `WARNING`, `ERROR`, plus a `statusMessage`. | `[sourced]` spec `ObservationLevel` |
| Trace | The logical group of every observation that shares one `trace_id` (one request). In v4 it has **no own I/O**: I/O comes from the root observation. | `[sourced]` `/docs/observability/data-model`; `/faq/all/deprecated-api-migration#traces`. (Conflict: `/docs/glossary#trace` still says traces "contain the overall input, output".) |
| Root observation | `isRootObservation=true`: no physical parent, or marked by the SDK as the app root. It can still have a `parentObservationId`. | `[sourced]` public-api page `#logical-root-observations` |
| Session | Optional grouping of traces that share a `sessionId`, such as a chat thread. | `[sourced]` `/docs/glossary#session` |
| Score | An evaluation result attached to exactly one trace, observation, session or experiment (dataset run). Data types are `NUMERIC`, `BOOLEAN`, `CATEGORICAL`, `TEXT`, `CORRECTION`. Source is `API`, `ANNOTATION` or `EVAL`. | `[sourced]` public-api `#scores-value`; spec `ScoreDataType`, `ScoreSource` |
| Score config | A schema for a score: data type, numeric range, categories. Used to standardize scoring. | `[sourced]` `/docs/glossary#score-config` |
| Dataset | A collection of test cases (items) holding inputs and optional expected outputs. | `[sourced]` `/docs/glossary#dataset` |
| Dataset item | One test case: `input`, optional `expectedOutput`, `metadata`, optional `sourceTraceId`/`sourceObservationId`. Versioned: `GET /dataset-items?version=` needs `datasetName`. | `[sourced]` glossary; spec `CreateDatasetItemRequest` |
| Dataset run / experiment | One execution of a dataset through the app. Items link to traces. "Experiment" is the new canonical term. | `[sourced]` `/docs/glossary#dataset-experiment`; `/faq/all/deprecated-api-migration#dataset-runs` |
| Task | The application function under test in an experiment. | `[sourced]` `/docs/glossary#task` |
| Prompt | A versioned template, either `text` (one string) or `chat` (an array of role messages), with `{{variables}}`. | `[sourced]` `/docs/glossary#text-prompt`, `#chat-prompt`, `#prompt-variables`; spec `PromptType`. Config, tags and folders also exist as features: `[sourced — unverified]` (inferred from the `/llms.txt` page list and the `tag` param on `GET /v2/prompts`) |
| Prompt version | An auto-assigned version number per prompt name. Versions are immutable: to change content, create a new version. | `[sourced]` `/docs/prompt-management/features/prompt-version-control` (auto-assigned); `mcp.reference.langfuse.com` `createTextPrompt` ("Prompts are immutable - cannot modify existing versions") |
| Prompt label | A pointer to one version: `production` is served by default, `latest` is auto-maintained, and custom labels such as `staging` or `tenant-1` are allowed. | same |
| Protected label | Viewer and member roles cannot move or delete it; admin and owner can. Available on Pro with the Teams add-on, Enterprise, and self-hosted EE. | same, `#protected-prompt-labels` |
| Annotation queue | A human-review worklist of TRACE, OBSERVATION or SESSION items. Item status is `PENDING` or `COMPLETED`. Users can be assigned. | `[sourced]` `/docs/glossary#annotation-queue`; spec enums |
| Evaluator | A scoring function, either LLM-as-a-judge or code, managed under `/v2/evaluators` with versions. Also the name of an observation type. | `[sourced]` `/docs/glossary#evaluator`; spec |
| Evaluation rule | Selects incoming observations by filter and sampling rate, then triggers one or more evaluators. | `[sourced]` `/docs/glossary#rule` |
| LLM-as-a-judge | An evaluation method in which an LLM scores outputs against criteria. Needs an **LLM connection**, which holds provider keys. | `[sourced]` `/docs/glossary#llm-as-a-judge`, `#llm-connection` |
| Model definition | Pricing per input and output token, used to derive generation cost automatically. Custom models go through `/models`. | `[sourced]` `/docs/glossary#model-definition` |
| Cost / usage | `usageDetails`, `costDetails`, `inputCost`/`outputCost`/`totalCost` (USD), `usagePricingTierName`. | `[sourced]` public-api `#field-groups` |
| Environment | A deployment context (`production`, `staging`, …) on traces, observations and scores. The default is `default`. | `[sourced]` `/docs/glossary#environment`; sample response on `/docs/api-and-data-platform/features/observations-api` |
| Tags | Free-form labels on traces and observations, used for filtering (`arrayOptions`). | `[sourced]` `/docs/glossary#tags` |
| Metadata | A free-form key/value map. Values are truncated to 200 characters in v2 unless you pass `expandMetadata`. | `[sourced]` public-api `#observations-filters` |
| User ID | The end-user identifier on a trace, used for per-user analytics. It can be filtered but not grouped in Metrics v2. | `[sourced]` `/docs/glossary#user-tracking`; `/docs/metrics/features/metrics-api` |
| Release / version | `release` is the app release (trace context). `version` is the component version. Metrics dimensions `traceRelease` and `traceVersion` map to them. | `[sourced]` spec `/v2/metrics` description |
| Comment | Free text attached to a TRACE, OBSERVATION, SESSION or PROMPT. `POST /comments` requires `projectId`, `objectType`, `objectId` and `content`. | `[sourced]` spec `CreateCommentRequest`, `CommentObjectType` |
| Media | Multimodal attachments. `POST /media` returns a presigned upload URL, and `GET /media/{id}` returns metadata plus a download URL. | `[sourced]` spec (`media_getUploadUrl`, `media_get`); MCP ref `getMedia` |
| Billable unit | Traces + observations + scores ingested per billing period. | `[sourced]` `/docs/glossary#billable-unit` |

---

## 4. Common workflows (endpoint sequences)

General rules for every workflow:

- Always send a bounded time window.
- Observations v2 returns rows sorted by **startTime DESC**, so a client that renders a tree or a replay must reverse
  them. `[sourced]` public-api
- Aggregate with Metrics v2 rather than paging raw rows. `[sourced]` public-api `#v2`

### 4.1 Trace investigation

| Goal | Calls in order |
|---|---|
| Trace by id | `GET /v2/observations?traceId=T&fields=core,basic,time,io,usage,model,metrics,trace_context&limit=1000`. Follow `meta.cursor`, then build the tree from `parentObservationId`. The root has `parentObservationId` null, or `isRootObservation`, and holds the trace's I/O. `[sourced]` `/faq/all/deprecated-api-migration#traces` |
| Traces by user or session | `GET /v2/observations?userId=U` (or `sessionId=S`) `&isRootObservation=true&fromStartTime&toStartTime`. That gives one row per trace; drill into each with the row above. `[sourced — unverified]` (composed from documented params) |
| Errors | `GET /v2/observations?level=ERROR&fromStartTime&toStartTime&fields=core,basic` (`statusMessage` is in `basic`). Group by `traceId`, then fetch each trace tree. To count errors, use Metrics v2 with the `observations` view, a `level = ERROR` filter, and `name` or `traceName` as the dimension. `[sourced]` public-api filters; spec metrics dims |
| Latency outliers | Metrics v2: `{"view":"observations","metrics":[{"measure":"latency","aggregation":"p95"}],"dimensions":[{"field":"name"}],…}`. Then list rows with `filter=[{"type":"number","column":"latency","operator":">","value":X}]&fields=core,basic,metrics`. **Unit trap:** Metrics v2 reports `latency` and `timeToFirstToken` in **milliseconds**, but the Observations v2 `latency` filter column and response field are in **seconds**. Divide the p95 by 1000 before you use it as X. `[sourced]` spec `/v2/metrics` description ("milliseconds") and the `/v2/observations` `filter` description ("Latency in seconds (calculated: end_time - start_time)"); metrics page and cookbook `/guides/cookbook/example_metrics_api_v2` |
| Cost spikes | Metrics v2 `totalCost` `sum` with `timeDimension.granularity=hour|day` and a `providedModelName` dimension. Pick the spike window, then `GET /v2/observations` in that window with a `totalCost` number filter and `fields=core,basic,usage,model`. `[sourced]` `/docs/metrics/features/metrics-api` |
| Scores of a trace | `GET /v3/scores?traceId=T&fields=details,subject`. `[sourced]` public-api `#scores-examples` |
| Record the outcome | `POST /comments` (objectType TRACE or OBSERVATION) or `POST /scores`. `[sourced]` `/docs/observability/features/agentic-access` (example workflows) |

### 4.2 Prompt management

| Step | Call |
|---|---|
| List or discover labels | `GET /v2/prompts[?name=&label=&tag=]` (page/limit). Each row carries its `labels`. |
| Fetch production | `GET /v2/prompts/{name}` with no label or version: returns the `production` version, or **404** if none has that label. |
| Fetch a specific version or label | `?version=N` or `?label=staging`. Both together return **400**. A missing label returns **404** and never falls back. `resolve=false` returns the unresolved composed prompt. |
| Compare versions | Fetch `?version=N` and `?version=M`, then diff on the client. (The diff view exists only in the UI.) |
| Create a new version | `POST /v2/prompts` with the same name; can carry `labels:["staging"]`. |
| Promote or roll back | `PATCH /v2/prompts/{name}/versions/{version}` with body `{"newLabels":["production"]}`. On a protected label this can be refused for member and viewer roles. |

Sources: `[sourced]` `/docs/prompt-management/features/prompt-version-control#label-resolution`; spec (`newLabels`,
`resolve`). Prompt GETs have **no rate limit** on Cloud. `[sourced]` `/faq/all/api-limits`

### 4.3 Datasets and experiments (compare runs)

1. `GET /v2/datasets/{datasetName}` to get the dataset **id**.
2. `GET /experiments?datasetId=…&fromStartTime=…&fields=core,scores`. This lists runs, newest activity first.
3. For each run you compare: `GET /experiment-items?experimentId=…&fromStartTime=…&fields=io,scores`.
4. Join the items on dataset item id on the client and diff the scores and outputs.
5. For any regression, `GET /v2/observations?traceId=<item.traceId>` to walk the full tree.

To add production cases: `POST /dataset-items` with `sourceTraceId` and `sourceObservationId`. `[sourced]`
`/faq/all/deprecated-api-migration#dataset-runs` example; public-api `#experiments`; spec.

### 4.4 Scores and evaluation analysis

- Aggregates:
  - `GET /v2/metrics` with the `scores-numeric` view, `value` `avg` and `name` as the dimension.
  - For booleans, use `scores-boolean`: `avg(value)` gives the true-rate.
  - For categorical scores, use `scores-categorical` grouped by `stringValue`.
  - `[sourced]` spec metrics description; cookbook
- Rows: `GET /v3/scores?name=a,b&source=EVAL&dataType=NUMERIC&valueMax=0.5&fields=details,subject`. `[sourced]` public-api
- Score by user: v3 has no `userId`. Resolve the trace IDs with Observations v2 `userId=`, then pass them to v3
  `traceId=a,b,…`. `[sourced]` `/faq/all/deprecated-api-migration#scores`
- Aggregates by user: the Metrics v2 score views carry `userId` ("User ID from trace") as a filter, not a grouping
  dimension; the migration guide names them as the replacement of the v2 `userId` score filter. `[sourced]`
  `/docs/metrics/features/metrics-api#v2`, `/faq/all/deprecated-api-migration#scores` (checked with the
  `langfuse-docs` MCP, 2026-09-28, #135)
- Configure automated evaluation:
  - `GET/POST /v2/evaluators`, then `POST /v2/evaluation-rules`.
  - LLM connections must exist first (`/llm-connections`).
  - `[sourced — unverified]` (order inferred from `/docs/glossary#rule` and `#llm-connection`)

### 4.5 Metrics API v2 query shape

```json
{
  "view": "observations | scores-numeric | scores-boolean | scores-categorical",
  "metrics": [{"measure": "totalCost", "aggregation": "sum"}],
  "dimensions": [{"field": "providedModelName"}],
  "filters": [{"column": "isRootObservation", "operator": "=", "value": true, "type": "boolean"}],
  "timeDimension": {"granularity": "auto|minute|hour|day|week|month"},
  "fromTimestamp": "ISO", "toTimestamp": "ISO",
  "orderBy": [{"field": "sum_totalCost", "direction": "desc"}],
  "config": {"row_limit": 100, "bins": 10}
}
```

Send it as the URL-encoded `query` param of `GET /api/public/v2/metrics`.

- Aggregations: `sum avg count max min p50 p75 p90 p95 p99 histogram`.
- `orderBy` uses `{aggregation}_{measure}`, or `time_dimension` for the time dimension.
- Measures on the `observations` view: `count`, `latency`, `streamingLatency`, `inputTokens`, `outputTokens`,
  `totalTokens`, `outputTokensPerSecond`, `tokensPerSecond`, `inputCost`, `outputCost`, `totalCost`,
  `timeToFirstToken`, `countScores`.
- Grouping by a high-cardinality dimension (`id`, `traceId`, `userId`, `sessionId`, `parentObservationId`) returns
  **400**.

`[sourced]` spec `/v2/metrics` description; `/docs/metrics/features/metrics-api#v2`.

Worked example, the total cost per day of the observations of traces named `checkout` (the server's static metrics
query guidance on `describe_operation metrics_metrics`, #104; its dates lie in January 2025 so a model never
mistakes them for "the last 7 days" of a run, #105):

```json
{"view":"observations","metrics":[{"measure":"totalCost","aggregation":"sum"}],
 "filters":[{"column":"traceName","operator":"=","value":"checkout","type":"string"}],
 "timeDimension":{"granularity":"day"},"fromTimestamp":"2025-01-01T00:00:00Z","toTimestamp":"2025-01-08T00:00:00Z"}
```

- Required keys: `view`, `metrics`, `fromTimestamp`, `toTimestamp`; `dimensions` and `filters` default to `[]`,
  `timeDimension` and `orderBy` to null.
- A filter item is `{column, operator, value, type}` (`key` only for `stringObject`/`numberObject`); a `string` filter
  takes `=`, `contains`, `does not contain`, `starts with`, `ends with`.
- `traceName` stays a backwards-compatible column of the `observations` view in v2, usable in `filters` and
  `dimensions`.

The legacy v1 query (`GET /api/public/metrics`, `legacy_metricsV1_metrics`) has the same top-level keys as v2 and the
`traces` view besides `observations` and the three `scores-*` views; its documented example, the number of traces per
name (`{"view":"traces","metrics":[{"measure":"count","aggregation":"count"}],"dimensions":[{"field":"name"}],…}`),
is the server's static guidance on `describe_operation legacy_metricsV1_metrics` (#178), with fixed January 2025 dates.
Its documented `orderBy` sample is `{"field":"name","direction":"asc"}`. `[sourced]` API reference
`GET /api/public/metrics` and `/docs/metrics/features/metrics-api` (`langfuse-docs` MCP, 2026-10-10).

`[sourced]` checked on 2026-09-27 with the `langfuse-docs` MCP: API reference `GET /api/public/v2/metrics` (`query`
parameter structure); `/docs/metrics/features/metrics-api#v2`; `/guides/cookbook/example_metrics_api_v2` (daily
`timeDimension`); `/faq/all/deprecated-api-migration#metrics` (`traceName` as a filter column).

### 4.6 Session replay

`GET /v2/observations?sessionId=S&isRootObservation=true&fields=core,basic,io&fromStartTime&toStartTime`, paginated.
Reverse the rows to chronological order. Each root's I/O is one turn. Drill into a turn with `traceId=`. Session
scores: `GET /v3/scores?sessionId=S`. `[sourced]` `/faq/all/deprecated-api-migration#sessions` (root-observation
reconstruction); combining `isRootObservation` with `sessionId` is `[sourced — unverified]`.

### 4.7 Annotation queues

1. `GET/POST /annotation-queues` to find or create a queue.
2. `POST /annotation-queues/{id}/items` with `{objectId, objectType: TRACE|OBSERVATION|SESSION}`.
3. `POST /annotation-queues/{id}/assignments` to assign users.
4. Reviewers score with `POST /scores`, whose body accepts `queueId`.
5. `PATCH /annotation-queues/{id}/items/{itemId}` with `{status: COMPLETED}`.
6. Monitor with `GET …/items?status=PENDING`.

`[sourced]` spec request schemas. The order is `[sourced — unverified]`. The local skill
`~/.claude/skills/langfuse/references/error-analysis.md` covers when to target OBSERVATION versus TRACE.

---

## 5. Official Langfuse MCP server, Agent Skill and CLI

### 5.1 Project MCP (`/api/public/mcp`)

| Aspect | Fact | Source |
|---|---|---|
| Endpoints | `https://{cloud,us.cloud,jp.cloud,hipaa.cloud}.langfuse.com/api/public/mcp`, or `https://<self-host>/api/public/mcp`. Transport is `streamableHttp`. Auth is `Authorization: Basic base64(pk:sk)` with a **project-scoped** key. The server is stateless. | `[sourced]` `/docs/api-and-data-platform/features/mcp-server` |
| Read/write | **Read and write tools are both enabled by default.** Read-only needs a **client-side allowlist**. | same |
| Self-host gotcha | Behind a reverse proxy, keep the public `Host` header or set `LANGFUSE_MCP_ALLOWED_HOSTS`. Otherwise you get `403`. The docs' Claude Code example says "Self-Hosted (HTTPS required)". | same |
| Rate limit | Shares the General API bucket. | `[sourced]` `/faq/all/api-limits` |
| Recommendation | Langfuse recommends the Agent Skill plus CLI over MCP when the agent can run shell commands. | `[sourced]` mcp-server page; `/docs/*/agentic-access` |
| Canonical tool list | `https://mcp.reference.langfuse.com`. It is self-describing, and schemas evolve. | `[sourced]` mcp-server page |

**Tools: 86 under "Langfuse Cloud MCP".** `[sourced]` `mcp.reference.langfuse.com`, by section headings in the raw HTML.

| Group | Tools |
|---|---|
| Prompts | `getPrompt`, `getPromptUnresolved`, `listPrompts`, `createTextPrompt`, `createChatPrompt`, `updatePromptLabels` |
| Observations | `listObservations`, `getObservation`, `getObservationFieldSchema`, `getObservationFilterSchema`, `getObservationFilterValues` |
| Metrics | `queryMetrics`, `getMetricsSchema` |
| Scores and configs | `listScores`, `getScore`, `createScore`, `listScoreConfigs`, `getScoreConfig`, `createScoreConfig`, `updateScoreConfig`, `deleteScoreConfig` |
| Datasets | `upsertDataset`, `listDatasets`, `getDataset`, `upsertDatasetItem`, `batchUpsertDatasetItems` (≤100 items, stops on the first failure, not atomic), `listDatasetItems`, `getDatasetItem`, `deleteDatasetItem`, `createDatasetRunItem`, `listDatasetRunItems`, `listDatasetRuns`, `getDatasetRun`, `deleteDatasetRun` |
| Experiments | `listExperiments`, `listExperimentItems` |
| Annotation queues | `listAnnotationQueues`, `createAnnotationQueue`, `getAnnotationQueue`, `listAnnotationQueueItems`, `getAnnotationQueueItem`, `createAnnotationQueueItem`, `updateAnnotationQueueItem`, `deleteAnnotationQueueItem`, `createAnnotationQueueAssignment`, `deleteAnnotationQueueAssignment` |
| Comments | `createComment`, `listComments`, `getComment` |
| Evaluators and rules | `listManagedEvaluatorTemplates`, `listEvaluators`, `getEvaluator`, `testEvaluator` (executes the evaluator, which may incur cost), `createEvaluator`, `updateEvaluator`, `deleteEvaluator`, `listEvaluationRules`, `getEvaluationRule`, `createEvaluationRule`, `updateEvaluationRule`, `attachEvaluatorToEvaluationRule`, `detachEvaluatorFromEvaluationRule`, `deleteEvaluationRule` |
| Models | `listModels`, `createModel`, `getModel`, `deleteModel` |
| Dashboards | `list/get/create/update/deleteDashboard`, `list/get/create/update/deleteDashboardWidget`, `add/update/deleteDashboardPlacement` |
| Other | `getMedia`, `getHealth`, `submitFeedback`, `listAlerts`, `getAlert`, `getV4MigrationData` |

The **Docs MCP** (`https://langfuse.com/api/mcp`, no auth) is a separate server. Its tools are `searchLangfuseDocs`,
`getLangfuseDocsPage`, `getLangfuseOverview` and `submitFeedback`. `[sourced]` same reference; `/llms.txt`

**Documented limitations of the official MCP.** All `[sourced]` from the raw `mcp.reference.langfuse.com` page:

- An `io`/metadata projection or filter needs a `traceId`, an id filter, or a date range of **≤14 days**. Date-scoped
  I/O projections are capped at **limit 50**.
- Metadata is truncated to **200 UTF-8 characters per key** unless `expandMetadataKeys` is passed.
- **Score reads are eventually consistent** after `createScore`: retry after a short wait.
- `getObservation` and `queryMetrics` are marked "Requires v4".

**Official MCP compared with the local REST spec** (useful when designing our own server). `[sourced — unverified]`: I
compared tool names against spec operationIds by hand.

- **MCP-only tools with no REST route in the spec:** `listAlerts`/`getAlert`, `getObservationFieldSchema`/
  `getObservationFilterSchema`/`getObservationFilterValues`, `getMetricsSchema`, `testEvaluator`,
  `listManagedEvaluatorTemplates`, `attach`/`detachEvaluatorFromEvaluationRule`, `getV4MigrationData`, `getPromptUnresolved`.
  The last may map to `?resolve=false`.
- **MCP tools that still wrap deprecated REST:** `listDatasetRuns`, `getDatasetRun`, `deleteDatasetRun`,
  `createDatasetRunItem`, `listDatasetRunItems`.
- **REST with no MCP tool:** org and SCIM, projects and API keys, blob-storage integrations, LLM connections, trace
  deletion, `DELETE /scores/{id}`, `DELETE /v2/prompts/{name}`, `GET /v2/evaluators/{id}/versions`, media upload, OTel ingestion (a partial list, compiled by hand). `getObservation` exists in MCP, but REST v2 has no
  get-by-id.

### 5.2 Agent Skill and CLI

| Aspect | Fact | Source |
|---|---|---|
| Skill | `github.com/langfuse/skills` (`skills/langfuse`). A `SKILL.md` plus `references/*.md` (cli, instrumentation, prompt-migration, …) with progressive disclosure. Install with `npx skills add langfuse/skills --skill "langfuse"`, or via the Cursor plugin. | `[sourced]` `/docs/api-and-data-platform/features/agent-skill` |
| CLI | `npx @langfuse/cli api <resource> <action>`, generated from the full OpenAPI spec so every endpoint is a command. The old package `langfuse-cli` is kept in sync until the next major. Use ≥ v1.1.0 for the new endpoints. | `[sourced]` `/docs/api-and-data-platform/features/cli`; `/faq/all/deprecated-api-migration` |
| CLI auth | `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY`, `LANGFUSE_BASE_URL`. There is no login step. | `[sourced]` cli page |
| CLI exit codes | 2 usage, 3 config, 4 network, 5 HTTP, 6 local. | `[sourced]` cli page |
| CLI discovery | `langfuse api __schema`, `<resource> --help`, `--curl` (dry-run), `--json`. It can pin or auto-detect the server API version for older self-hosted releases. | `[sourced]` `~/.claude/skills/langfuse/references/cli.md` (local copy); `/docs/glossary#langfuse-cli` |
| Local skill drift | The installed copy at `~/.claude/skills/langfuse` still says `npx langfuse-cli` and mentions `LANGFUSE_HOST`, while the docs use `@langfuse/cli` and `LANGFUSE_BASE_URL`. Its tip "prefer `scores` over `legacy-score-v1s`" is ambiguous now that `/v2/scores` is also deprecated in favor of v3. | `[sourced]` local files `SKILL.md:72-74`, `references/cli.md` |

### 5.3 TLS, CA and proxy configuration

| Component | What is documented | Source |
|---|---|---|
| Python SDK, API client | Pass your own `httpx.Client(verify=<ca-bundle-path>, proxy=…)` as `Langfuse(httpx_client=…)`. The issue thread uses `verify=False`, which disables TLS verification and invites MITM; a replacement server should never offer that, only an extra-CA option. | `[sourced]` GitHub issue https://github.com/langfuse/langfuse/issues/7485 (surfaced by docs search) |
| Python SDK, OTel exporter | `httpx_client` is **not** used for trace export. The workaround in the issue patches the exporter's internal `requests` session, which is fragile. | `[sourced]` same issue |
| JS/TS SDK | A feature request says the JS/TS SDK has "no proxy support". | `[sourced]` https://github.com/orgs/langfuse/discussions/9599 |
| CLI and official MCP (Node) | **No TLS/CA/proxy options are documented.** The likely cause is Node defaults: Node ignores the OS trust store and `HTTPS_PROXY` unless `NODE_USE_SYSTEM_CA`, `NODE_EXTRA_CA_CERTS` or `NODE_USE_ENV_PROXY` is set. | `[sourced — unverified]` (absence claim, plus the inference in `docs/research/2026-09-25-deep-research-run1.md` F3) |
| Self-hosted server | Server-side settings only: `REDIS_TLS_*`, `AUTH_HTTPS_PROXY`/`AUTH_HTTP_PROXY` for SSO, HTTPS terminated at the load balancer. These are unrelated to client trust. | `[sourced]` `/self-hosting/deployment/infrastructure/cache`, `/self-hosting/security/authentication-and-sso`, `/self-hosting/configuration/encryption` |

Conclusion: the official tooling documents no first-class client CA-bundle or proxy knob. This is the gap a replacement
server should close with explicit `ca-file`/`proxy` config. `[sourced — unverified]`

---

## 6. OpenAPI spec (the 4.31–4.45 spec once embedded as `internal/catalog/spec/langfuse-openapi.json`, replaced by the union catalog in #71) compared with the docs

| # | Mismatch | Evidence |
|---|---|---|
| 1 | The spec has **no `servers` block**, so base URLs must come from config. | `d.get('servers') → None` |
| 2 | The spec marks about 25 routes org-scoped. The docs list 10. Missing from the docs: `/organizations/projects`, `/organizations/apiKeys`, `DELETE /organizations/memberships`, `GET /projects/{id}/memberships`, blob-storage. | §1.2 |
| 3 | Deprecated in the docs but absent from the spec: v1 `GET /scores[/{id}]`, `GET /metrics/daily`, `POST /traces\|spans\|generations\|events`, v1 `GET /datasets[/{name}]`. | spec path list vs `/faq/all/deprecated-api-migration` |
| 4 | `DELETE /scores/{scoreId}` has operationId `legacy_scoreV1_delete` but is **not** flagged deprecated. Score writes (`POST /scores`) remain supported. | spec |
| 5 | The spec's observations-v2 `usage` group lists `usageDetails, costDetails, totalCost, usagePricingTierName`. The docs list 9 fields, including `inputUsage`, `outputUsage`, `inputCost` and `outputCost`. | spec description vs public-api `#field-groups` |
| 6 | The `model` group's first field is `model` on the public-api page and `providedModelName` on the observations-api page. The v2 sample response uses `providedModelName`. | `/docs/api-and-data-platform/features/public-api` vs `/docs/api-and-data-platform/features/observations-api` |
| 7 | The spec's filter types include `categoryOptions`, which the docs' operator table omits. | spec `filter` param |
| 8 | The spec's metrics `type` dimension and the observations-api page still say "SPAN, GENERATION, EVENT", while the enum has 10 types. | spec `ObservationType` |
| 9 | The spec still exposes `parseIoAsJson`, and even flags "Deprecated" inside the v2 observations description. The docs say `true` returns 400. | spec |
| 10 | Freshness is "about 10 minutes" in the spec and "up to 15 minutes" in the docs. | §1.1 |
| 11 | The spec contains `unstable/dashboards*` and `unstable/dashboard-widgets*`, which are unversioned and unstable. MCP exposes them. | spec |
| 12 | **Latency units differ by endpoint.** Metrics v2 measures `latency`, `streamingLatency` and `timeToFirstToken` in **ms**. Observations v2 filters and fields (and the legacy trace schema) use **seconds**; the legacy observations list returns an observation's `latency` in **ms** on 3.80.0 (§1.12). This is not a docs error, but any tool that bridges the two endpoints must convert. | spec `/v2/metrics` description vs the `/v2/observations` `filter` description and the `latency` schema descriptions |
| 13 | `ObservationsV2Meta` declares no own properties. It inherits `utilsCursorMetaResponse` through `allOf`, so a code generator must resolve the `allOf` to see `cursor`. | `components.schemas.ObservationsV2Meta` |
| 14 | `GET /api/public/v2/prompts` has a `filter` query parameter in the release specs from 4.42.0 (a JSON string of filter conditions combined with AND) that the current API reference does not list. | Release specs `v4.42.0` and `v4.46.0` against the reference page of `prompts_list` (`langfuse-docs` MCP, 2026-10-11); §1.15 |
