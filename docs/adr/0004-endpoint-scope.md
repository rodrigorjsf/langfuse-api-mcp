---
status: accepted
---
# Endpoint scope: all current operations except deprecated and ingestion

"Implements all endpoints" means every operation in `docs/langfuse-openapi.json` **except**: the 15 operations marked deprecated (each has a v2/v3 replacement that is in scope), `POST /api/public/ingestion` (legacy, shut down on Langfuse Cloud on 2026-11-16) and `POST /api/public/otel/v1/traces` (trace ingestion is the SDKs' job, not an agent's). Organization-scoped operations (Organizations, Projects admin, SCIM) are exposed only when an organization-level key is configured, because project keys cannot call them.

## Open question (pending user decision)

The spec marks ~25 routes org-scoped (docs list 10) and they are Enterprise features. Among them, `POST/PUT/DELETE /projects`, `POST/DELETE /projects/{id}/apiKeys` and SCIM user management let an agent create or delete projects and **mint or revoke credentials** — research (`docs/research/security.md`) recommends excluding those mutating admin operations entirely, even with writes enabled.

## Consequences

- A test asserts the catalog equals the spec minus this explicit exclusion list, so a spec update that adds an operation fails CI until it is triaged.
