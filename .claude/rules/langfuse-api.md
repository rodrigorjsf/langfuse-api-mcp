---
paths:
  - "internal/langfuse/**"
  - "internal/catalog/**"
  - "internal/server/**"
  - "internal/workflows/**"
  - "skills/**"
---
# Langfuse API facts that bite

Source of truth for API facts: `docs/research/langfuse.md` (cited). Verify anything new with the `langfuse-docs` MCP (`searchLangfuseDocs`, `getLangfuseDocsPage`) before coding it; the OpenAPI spec `internal/catalog/spec/langfuse-openapi.json` wins over prose docs when they disagree, and record the mismatch in `docs/research/langfuse.md` §6.

- **Auth**: Basic `pk-lf-…:sk-lf-…`. Base URL `<host>/api/public`. Config name `LANGFUSE_BASE_URL` (docs' current name), `LANGFUSE_HOST` accepted as alias. Region presets: EU `cloud.langfuse.com`, US `us.cloud.langfuse.com`, JP `jp.cloud.langfuse.com`, HIPAA `hipaa.cloud.langfuse.com`; keys only work in their own region.
- **Org keys**: ~25 routes (projects admin, apiKeys, memberships, SCIM, blob-storage) need an organization key and are Enterprise features. Reads exposed with an org key; admin mutations (projects, API keys, memberships, SCIM users) never exposed — ADR-0004.
- **Deprecated** (v1 traces/observations/sessions, scores v1/v2, metrics v1, dataset-runs, `/ingestion`): excluded (ADR-0004); served on Cloud only until **2026-11-16**. Exception: on a **Legacy deployment** (self-hosted major < 4, detected via `/api/public/health` at startup) the 12 deprecated GETs are in the catalog and workflow tools are not registered (ADR-0012). Legacy routes cap `limit` at 100.
- **Generations**: self-hosted v3 lacks `/v2/observations`, `/v2/metrics`, `/v3/scores`, experiments (HTML 404); fresh v4 runs `events_only` (deprecated routes 404 with a JSON message). `docs/research/langfuse.md` §1.7–1.8.
- **Rate limits** (Cloud, per org, fixed windows): honor `Retry-After` on 429, never blind-retry. Metrics v2 is 100/**day** on Hobby → budget and cache metric calls. General API 30/min on Hobby → frugal tree walks. 5 MB request/response cap. Self-hosted: no limits.
- **Pagination**: cursor (`meta.cursor`) for `v2/observations`, `v3/scores`, experiments, experiment-items, v2 evaluators/rules; page/limit elsewhere; metrics v2 uses `config.row_limit` (≤1000). Scores v3 `limit` > 100 → 400.
- **Observations v2**: sorted by `startTime` desc, no `orderBy`; no get-by-id (filter on `id`); `filter` JSON overrides fixed params; `fields` groups default `core,basic` — unrequested fields are **absent**; prices are strings; `limit` max 1000; io/metadata windows and >50 rows are **not** enforced on self-hosted (3.80.0, 4.46.0); Cloud unverified (#2).
- **Time windows (decided)**: workflow tools default to the last 24h and cap at 14 days when requesting `io`/`metadata` without `traceId`/id filter; `execute_read` passes queries through unchanged and maps any upstream rejection to a tool error with a hint. Whether REST enforces 14 days itself: issue #2.
- **Units**: Metrics v2 latency in **ms**; Observations v2 latency fields/filters in **seconds**.
- **Scores v3**: `traceId`/`sessionId`/`experimentId` mutually exclusive; `observationId` requires `traceId`; no `userId` filter.
- **Prompts**: fetch by label *or* version, not both (400); missing label → 404 (no fallback); promote = PATCH `newLabels`; protected labels may refuse.
- **Experiments**: `fromStartTime` required.
