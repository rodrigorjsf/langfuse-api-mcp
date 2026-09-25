# PROTOTYPE — Observations v2 io/metadata window and row limits (throwaway)

Question (issues #2, #8, #15; input for #14): does `GET /api/public/v2/observations` enforce the official MCP's "io/metadata needs traceId, an id filter, or ≤14-day window; ≤50 rows" rule? Run 2026-09-25, **self-hosted only**.

## Setup

- Upstream `docker-compose.yml` from `langfuse/langfuse` main (commit `fd5c9ee18e07`), images `langfuse:4` / `langfuse-worker:4` = **4.46.0** (`/api/public/health`), with headless init from `compose.env` (throwaway keys, local only).
- Cold start to `/api/public/ready`: **54 s** with images cached; steady memory ≈ **2.8 GiB** total (web 1.4 GiB, worker 0.8 GiB, ClickHouse 0.35 GiB).
- **Pull before use:** a cached `:4` image was 4.16.0 (Aug 2026) and lacked `/v2/evaluators` and `/v2/evaluation-rules` (HTML 404). Pin a digest in CI.
- `seed.py` posts OTLP/HTTP JSON to `/api/public/otel/v1/traces`: spans aged 1/13/14.5/16/20/40 days plus a 61-span trace aged 2 days, each with input/output/metadata attributes. **Back-dated start times are accepted and preserved**; rows are queryable ≈10 s after the 200.

## Results (`probe-4.46.0.txt`, verbatim)

- io and metadata projections with **no window**, a 14 d+1 s, 15 d, 30 d window, or only `fromStartTime` → **200** with rows (the 30 d window returned the 16 d and 20 d rows with `input` populated).
- io with `limit=51` → 51 rows; `limit=1000` → all rows. **No 50-row cap.**
- `limit=1001` → 400 `{"message":"Invalid request data","error":[{"origin":"number","code":"too_big","maximum":1000,...,"path":["limit"],...}]}`.
- Unknown `fields` value is silently ignored (200).

## Verdict

Self-hosted 4.46.0 REST does **not** enforce the 14-day window or the 50-row cap on io/metadata projections; the rule is the official MCP tool's own guard. Cloud is **still unprobed** (needs the wizard's CI project) — a Cloud-only guard remains possible, so #2 stays open.

## Side findings (for the M1 catalog and error mapping)

- A fresh v4 deployment runs in **`events_only`** mode: every `deprecated: true` GET in the spec (12 ops: traces, sessions, v1 observations, v2 scores, v1 metrics, dataset runs/run-items) returns 404 `{"message":"This endpoint is not available on deployments running in Langfuse v4 events_only mode. ..."}` — consistent with ADR-0004's exclusion.
- An operation missing from an older deployment (version skew) returns **404 with an HTML body** (Next.js page), same as an unknown route; never echo that body.
- Error body shapes vary: `{"message","error":"<Name>Error"}`, `{"message","code":"resource_not_found"}`, `{"message","error":[zod issues]}`, `{"error":"..."}` (org endpoints), SCIM `{"schemas":[...],"detail":...}`.
- Project key on org-scoped routes → 403 "Organization-scoped API key required for this operation."
- 401 bodies: `No authorization header` / `Invalid credentials. Confirm that you've configured the correct host.`; 405 `MethodNotAllowedError`.
