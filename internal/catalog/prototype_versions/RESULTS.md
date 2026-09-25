# PROTOTYPE — Langfuse public API availability by version and write mode (throwaway)

Question: which Langfuse versions add, deprecate or remove each public API operation, and what decides at runtime whether an operation answers? Goal: the MCP exposes, transparently and per deployment, every operation that deployment supports. Run 2026-09-25.

## Static matrix — `opmatrix.py` → `opmatrix.json`

- Source: the OpenAPI spec committed at each of 648 release tags of `langfuse/langfuse` (v1.0.1 … v4.46.0). There are 168 distinct specs.
- Spec paths: `web/public/generated/api/openapi.yml`, or `generated/openapi-server/openapi.yml` on old tags.
- Tags are mapped to spec blobs in `tagblob.txt`, read from a blobless bare clone.
- Result: 148 operations ever existed. For each one the matrix records `introduced`, `removed` (the first tag without it), `deprecatedSince` and `operationIds` (renames).
- Milestones:
  - Up to v2.58: v1 datasets.
  - Up to v3.53: v1 `GET /scores`.
  - Up to v3.62: `metrics/daily`.
  - v3.42: annotation queues.
  - v3.50: organization, projects admin and SCIM operations.
  - v3.53: `/v2/scores`.
  - v3.57: v1 metrics.
  - v3.120: OTLP.
  - v3.141: `/v2/observations` and `/v2/metrics`.
  - v3.180: `/v3/scores`.
  - v3.206: experiments.
  - v3.216: dashboards.
  - v4.0: feedback.
  - v4.12: legacy reads marked deprecated.
  - v4.23: v2 evaluators and rules. The `unstable/evaluators` routes were removed in v4.31.
  - v4.46: `unstable/skills`.
- The repo's embedded spec matches the operation set of v4.31.0–v4.45.x exactly.

## Runtime — `probe-version.sh <tag>` (every operation of that tag's spec), `functional.sh` (seeded reads)

| Deployment | Result |
|---|---|
| 3.80.0 (see `prototype/langfuse-io-window`) | ops absent from its own era's spec → HTML 404; legacy reads work |
| 3.225.11 (latest v3, 2026-09-24) | all 113 spec ops routed; **`/v2/observations`, `/v2/metrics` → 404 JSON "The observations v2 API is only available in a Langfuse v4 write mode"**; `/v3/scores` works |
| 4.46.0 default (`events_only`) | legacy reads → 404 JSON "not available on deployments running in Langfuse v4 events_only mode" |
| 4.46.0 `LANGFUSE_MIGRATION_V4_WRITE_MODE=dual` | all 124 ops routed; legacy and v2 families both answer |

The docs add one more setting: `LANGFUSE_MIGRATION_V4_ALLOW_PREVIEW_OPT_IN=false` disables the v2 APIs while dual-writing. `[sourced]` upgrade-v3-to-v4 guide.

## Verdict

Availability = f(**version**, **write mode**, **preview opt-in**). The version, read from `GET /api/public/health` without credentials, bounds the set through the static matrix. Two cheap probes then settle the mode-dependent families:
- legacy: `GET /traces?limit=1`
- v4 reads: `GET /v2/observations?limit=1&fields=core`

A JSON 404 whose message names a mode means that family is off. An HTML 404 means the route does not exist in that version.
