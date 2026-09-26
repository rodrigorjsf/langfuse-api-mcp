# Langfuse public API availability by version and write mode

Access date: **2026-09-25**. This page merges three kinds of evidence:

- **[spec]** The OpenAPI spec committed at each of 648 release tags of `langfuse/langfuse`, from v1.0.1 to v4.46.0 (168 distinct specs). Source: branch `prototype/langfuse-api-versions`, `internal/catalog/prototype_versions/opmatrix.json`. A row reads "first tag whose spec lists the operation".
- **[docs]** Official Langfuse docs and changelog, checked by a 3-vote adversarial deep-research run. Source: `raw/2026-09-25-deep-research-api-versions.json`.
- **[runtime]** Self-hosted containers probed on this machine: 3.80.0, 3.225.11, 4.46.0 (`events_only`) and 4.46.0 (`dual`). Sources: branches `prototype/langfuse-io-window` and `prototype/langfuse-api-versions`. All runtime rows are `[verified]`.

## 1. Headline: availability is decided by version and by configuration

The version alone does not decide which operations answer.

| Deployment | Legacy read family | v4 read family (`/v2/observations`, `/v2/metrics`) |
|---|---|---|
| self-hosted before 3.141 (e.g. 3.80.0) | answers | **absent**: HTML 404 `[runtime]` |
| self-hosted 3.141 to 3.x (e.g. 3.225.11) | answers | routed, but returns 404 JSON *"The observations v2 API is only available in a Langfuse v4 write mode"* `[runtime]`. The experiments family is off too: `/experiments` and `/experiment-items` return 404 JSON *"The experiments API is only available in a Langfuse v4 write mode"* (3.225.11, 2026-09-26, #73), so its deployment profile is legacy only, not ADR-0012's "legacy plus experiments" (#84) `[runtime]` |
| v4, `LANGFUSE_MIGRATION_V4_WRITE_MODE=legacy` | answers | returns no data; turned off when `ALLOW_PREVIEW_OPT_IN=false` `[docs]` |
| v4, `dual` | answers | answers; all 124 spec operations are routed on 4.46.0 `[runtime]` |
| v4, `events_only` (the default, and every fresh install) | 404 JSON *"not available on deployments running in Langfuse v4 events_only mode"* `[runtime]` | answers |
| Cloud until 2026-11-16 | answers | answers `[docs]` |
| Cloud after 2026-11-16 | removed | answers `[docs]` |

- **Cloud** runs 4.46.0 today; `GET /api/public/health` without credentials returned `{"status":"OK","version":"4.46.0"}` `[runtime]`.
- **Self-hosted v3** has no forced cutover. It gets security patches through January 2027 `[docs]`, and v3 releases are still shipping: 3.225.11 came out on 2026-09-24 `[spec]`.
- **The `legacy` and `dual` modes are temporary.** They are migration utilities that will be removed in an upcoming major version `[docs]` (upgrade-v3-to-v4 guide).

## 2. Three ways to tell whether an operation is unavailable `[runtime]`

| Response | Meaning |
|---|---|
| 404 with an HTML body (Next.js page) | the route does not exist in this version |
| 404 JSON naming `events_only` | the legacy family is off (v4 default) |
| 404 JSON naming "v4 write mode" | the v4 read family is off (v3 ≥ 3.141, or v4 in legacy mode) |

A plain 404 JSON with `LangfuseNotFoundError` and a resource message means the object was not found. It is not an availability signal.

## 3. Version floors per operation family

Where the spec and the docs disagree, the route shipped before it entered the spec, or the docs name a gated floor. Take the earlier version and let the runtime probe decide.

| Family | First in spec | Docs floor | Deprecated / removed |
|---|---|---|---|
| Legacy reads: traces, v1 observations, sessions | v1.x (sessions list v2.61.1) | — | spec marks them deprecated from v4.12; 404 in `events_only`; Cloud until 2026-11-16 |
| v1 `GET /scores`, `/scores/{id}` | v1.0.1 | — | **removed from the spec in v3.53.0**, replaced by `/v2/scores` |
| `/v2/scores` | v3.53.0 | — | deprecated from v4.12; 404 in `events_only` |
| `/metrics` (v1) | v3.57.0 | — | same as the legacy reads; `/metrics/daily` was removed from the spec in v3.62.0 |
| Dataset runs, run items | v1.0.1 (list v3.57.1) | — | deprecated from v4.12, replaced by experiments |
| OTLP `POST /otel/v1/traces` | **v3.120.0** | **3.22.0** `[docs 3-0]` | — |
| `/v2/observations`, `/v2/metrics` | v3.141.0 | "v4+" | gated by write mode (§1) |
| `/v3/scores` | v3.180.0 | 3.179.0 `[docs 3-0]` | answers on 3.225.11 `[runtime]` |
| `/experiments`, `/experiment-items` | v3.206.0 | "v4+" | `fromStartTime` required; whether v3 gates it by mode was not tested |
| v2 datasets (`/v2/datasets`) | v2.58.0 (v1 datasets removed then) | — | — |
| `/v2/prompts` | v2.38.0 (v1 prompts removed then) | — | prompt version PATCH path renamed in v3.18.0 |
| score configs | v2.56.0 (PATCH v3.113.0) | — | — |
| models | v2.59.0 (PUT upsert v4.19.0) | — | — |
| comments | v2.86.0 | — | — |
| media | v2.90.0 | — | — |
| annotation queues | v3.42.0 (assignments v3.95.0, create v3.97.3) | — | — |
| organizations, projects admin, SCIM | v3.50.0 (org projects v3.63.0, org API keys v3.124.0, membership DELETE v3.99.0) | — | — |
| LLM connections | v3.92.0 (DELETE v3.170.0) | — | — |
| blob-storage integrations | v3.110.0 (get by id v3.158.0) | — | — |
| `unstable/evaluators`, `unstable/evaluation-rules` | v3.170.0 | — | deprecated v4.23, **removed from the spec in v4.31** |
| `/v2/evaluators`, `/v2/evaluation-rules` | v4.23.0 | — | — |
| `unstable/dashboards`, dashboard widgets | v3.205.0 (widgets create), v3.216.0 (rest) | — | — |
| `feedback` | v4.0.0 | — | — |
| `unstable/skills` | v4.46.0 | — | — |
| `health` | v1.3.0 | — | — |

The per-operation table, which covers all 148 operations, is `opmatrix.json` on the prototype branch.

## 4. Runtime detection

- **Version.** `GET /api/public/health` needs no credentials and returns `version` on self-hosted (3.80.0, 3.225.11, 4.16.0, 4.46.0) and on Cloud `[runtime]`.
- **Write mode.** No endpoint exposes the write mode `[docs: open question, nothing found]`. It can only be inferred by probing each family:
  - legacy family: `GET /traces?limit=1`;
  - v4 read family: `GET /v2/observations?limit=1&fields=core`.
  Read each answer with §2. Do not probe `/v2/metrics`: on the Cloud Hobby plan it costs 1 of a 100-per-day budget.
- **Cloud.** It may need probing too. Whether a single Cloud project can already be v4-only before 2026-11-16 is unconfirmed `[docs: open question]`.

## 5. Refuted or unconfirmed claims (do not rely on them)

- "3.63.0 is a floor for the v2/v3 read resources" was refuted 0-3.
- "v4 introduced Observations and Metrics v2" was refuted 1-2. They were announced 2025-12-17, reached Cloud before v4 went GA, and entered the spec at 3.141.
- "`fromStartTime`/`toStartTime` are required on `/v2/observations`" was refuted 0-3. At runtime a request with no window returns 200 (§1.7 of `langfuse.md`).
