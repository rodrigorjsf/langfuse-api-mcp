---
paths:
  - "internal/langfuse/**"
  - "internal/catalog/**"
  - "internal/server/**"
  - "skills/**"
---
# Langfuse API facts that bite

Source of truth for API facts: `docs/research/langfuse.md` (cited). Verify anything new with the `langfuse-docs` MCP (`searchLangfuseDocs`, `getLangfuseDocsPage`) before coding it; the OpenAPI spec `docs/langfuse-openapi.json` wins over prose docs when they disagree, and record the mismatch in `docs/research/langfuse.md` §6.

- **Auth**: Basic `pk-lf-…:sk-lf-…`. Base URL `<host>/api/public`. Config name `LANGFUSE_BASE_URL` (docs' current name), `LANGFUSE_HOST` accepted as alias. Region presets: EU `cloud.langfuse.com`, US `us.cloud.langfuse.com`, JP `jp.cloud.langfuse.com`, HIPAA `hipaa.cloud.langfuse.com`; keys only work in their own region.
- **Org keys**: ~25 routes (projects admin, apiKeys, memberships, SCIM, blob-storage) need an organization key and are Enterprise features. Reads exposed with an org key; admin mutations (projects, API keys, memberships, SCIM users) never exposed — ADR-0004.
- **Deprecated** (v1 traces/observations/sessions, scores v1/v2, metrics v1, dataset-runs, `/ingestion`): excluded (ADR-0004); served on Cloud only until **2026-11-16**.
- **Rate limits** (Cloud, per org, fixed windows): honor `Retry-After` on 429, never blind-retry. Metrics v2 is 100/**day** on Hobby → budget and cache metric calls. General API 30/min on Hobby → frugal tree walks. 5 MB request/response cap. Self-hosted: no limits.
- **Pagination**: cursor (`meta.cursor`) for `v2/observations`, `v3/scores`, experiments, experiment-items, v2 evaluators/rules; page/limit elsewhere; metrics v2 uses `config.row_limit` (≤1000). Scores v3 `limit` > 100 → 400.
- **Observations v2**: sorted by `startTime` desc, no `orderBy`; no get-by-id (filter on `id`); `filter` JSON overrides fixed params; `fields` groups default `core,basic` — unrequested fields are **absent**; prices are strings; io/metadata projections likely need traceId/id or ≤14-day window (≤50 rows).
- **Units**: Metrics v2 latency in **ms**; Observations v2 latency fields/filters in **seconds**.
- **Scores v3**: `traceId`/`sessionId`/`experimentId` mutually exclusive; `observationId` requires `traceId`; no `userId` filter.
- **Prompts**: fetch by label *or* version, not both (400); missing label → 404 (no fallback); promote = PATCH `newLabels`; protected labels may refuse.
- **Experiments**: `fromStartTime` required.
