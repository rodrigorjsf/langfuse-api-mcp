---
status: accepted
---
# Legacy read operations are exposed only on Langfuse v3 deployments

Partially supersedes ADR-0004. That ADR excluded the 15 deprecated operations. Self-hosted Langfuse v3 (tested: 3.80.0) has none of their replacements: `/v2/observations`, `/v2/metrics`, `/v3/scores` and `/experiments` return an HTML 404. Under ADR-0004, a v3 user therefore could not read traces, observations, sessions, scores or metrics at all. Evidence: `docs/research/langfuse.md` §1.8.

## Decision

- **Detect the generation once.** At startup the server calls `GET /api/public/health`, which needs no credentials and returns `{"status","version"}`.
- **Legacy deployment.** If the major version is below 4, the deployment is a **Legacy deployment**. The catalog then also contains the **12 deprecated GET operations** (the Legacy read operations):
  - traces and trace by id;
  - v1 observations and observation by id;
  - sessions and session by id;
  - v2 scores and score by id;
  - v1 metrics;
  - dataset runs, run by name, and run items.
- **What stays out on every generation.** The deprecated writes (`datasetRunItems_create`, `datasets_deleteRun`, `/ingestion`) remain excluded.
- **Workflow tools are v4-only.** On a Legacy deployment they are not registered. The tool set stays fixed at startup: it never changes per request or per client.
- **Detection failure.** If the health call fails, or the version cannot be parsed, the server assumes v4, logs one stderr warning, and does not block startup.
- **Version skew error.** An operation the deployment does not have answers with an HTML 404, on any generation. It maps to the tool error `operation_unavailable`, whose hint names the detected version. The HTML body is never echoed.
- **No override flag.** There is no setting to force a generation; one will be added only if detection proves wrong in practice.

## Considered Options

- **v4 only (keep ADR-0004).** One code path, and Langfuse Cloud already runs v4 and sunsets the legacy routes on 2026-11-16. Rejected: corporate self-hosters, the audience of the TLS work, lag on upgrades and would lose the main use case.
- **Full v3 support, workflows included.** This needs a second implementation of every workflow (legacy `limit` max 100 against 1000, a different trace-tree shape). Rejected: high cost for a generation Langfuse is retiring.

## Consequences

- The ADR-0004 catalog test becomes two assertions, one per generation: the spec minus the exclusions, and that set plus the 12 Legacy read operations.
- The generic executor (ADR-0010) runs the Legacy read operations with no extra code. Their descriptions must say that they exist only on Langfuse v3.
- Integration tests gain a pinned v3 deployment. The v3.80.0 upstream compose needs `postgres:17` and `clickhouse-server:24.3`, and seeding goes through OTLP/protobuf (§1.8).
