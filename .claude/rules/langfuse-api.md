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
- **Version-aware catalog** (ADR-0012): union catalog from every release spec ≥ v3.0.0; operations filtered by version range, then by family probes (legacy `GET /traces`, v4 reads `GET /v2/observations`, experiments `GET /experiments`); never probe `/v2/metrics` (Hobby budget). Facts and floors: `docs/research/langfuse-api-versions.md`.
- **Deprecated/legacy** (v1 traces/observations/sessions, scores v1/v2, metrics v1, dataset runs): exposed only where they answer; Cloud serves them until **2026-11-16**; 404 in v4 `events_only`. Legacy routes cap `limit` at 100. `/ingestion` and OTLP are excluded everywhere (ADR-0004).
- **Rate limits** (Cloud, per org, fixed windows): honor `Retry-After` on 429, never blind-retry. Metrics v2 is 100/**day** on Hobby → budget and cache metric calls. General API 30/min on Hobby → frugal tree walks. 5 MB request/response cap. Self-hosted: no limits.
- **Pagination**: cursor (`meta.cursor`) for `v2/observations`, `v3/scores`, experiments, experiment-items, v2 evaluators/rules; page/limit elsewhere; metrics v2 uses `config.row_limit` (≤1000). Scores v3 `limit` > 100 → 400.
- **Observations v2**: sorted by `startTime` desc, no `orderBy`; no get-by-id (filter on `id`); `filter` JSON overrides fixed params; `fields` groups default `core,basic` — unrequested fields are **absent**; prices are strings; `limit` max 1000; io/metadata windows and >50 rows are **not** enforced by REST — Cloud and self-hosted 4.46.0, self-hosted 3.80.0 (`docs/research/langfuse.md` §1.6; weekly regression `TestLiveLangfuseServesPayloadQueriesBeyondFourteenDaysAndFiftyRows`). The 14-day / 50-row rule is the official MCP tool's own guard. Cloud accepted an OTLP-seeded span 40 days old but did not serve it within 90 s.
- **Time windows (decided, #2; Planned, M3 — nothing enforces it yet, see #42)**: workflow tools default to the last 24h. A **payload query** (requests the `io` or `metadata` field groups) without a `traceId` or `id` filter is capped at a 14-day window and `limit` 50 — **our own guard**, not Langfuse's, for budget (5 MiB read cap, 100 KiB result cap, 30 req/min on Cloud Hobby). A wider window or larger limit is **refused** with `invalid_argument` (hint: window ≤14 days, limit ≤50, or filter by traceId/id), never clamped. Only `traceId` and an `id` filter exempt; `sessionId`/`parentObservationId` do not. `execute_read` passes queries through unchanged (limit default 50 / max 100, byte caps).
- **Units**: Metrics v2 latency in **ms**; Observations v2 latency fields/filters in **seconds**.
- **Scores v3**: `traceId`/`sessionId`/`experimentId` mutually exclusive; `observationId` requires `traceId`; no `userId` filter.
- **Prompts**: fetch by label *or* version, not both (400); missing label → 404 (no fallback); promote = PATCH `newLabels`; protected labels may refuse.
- **Experiments**: `fromStartTime` required.
