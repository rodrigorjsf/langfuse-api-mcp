---
status: accepted
---
# Endpoint scope: all current operations except deprecated, ingestion and org admin mutations

"Implements all endpoints" means every operation in `docs/langfuse-openapi.json` **except**: the 15 operations marked deprecated (each has a v2/v3 replacement that is in scope), `POST /api/public/ingestion` (legacy, shut down on Langfuse Cloud on 2026-11-16) and `POST /api/public/otel/v1/traces` (trace ingestion is the SDKs' job, not an agent's). Organization-scoped operations (Organizations, Projects admin, SCIM) are exposed only when an organization-level key is configured, because project keys cannot call them.

## Organization operations (decided 2026-09-25, issue #1)

Organization **read** operations are exposed when an organization key is configured. Organization **admin mutations** are excluded from the catalog entirely, even in write mode: creating/updating/deleting projects, creating/deleting API keys, membership changes, and SCIM user create/update/delete. Minting or revoking credentials is the highest-impact action a prompt-injected agent could take, and the Langfuse UI remains the place for it.

## Consequences

- A test asserts the catalog equals the spec minus this explicit exclusion list (deprecated, ingestion, org admin mutations), so a spec update that adds an operation fails CI until it is triaged.
