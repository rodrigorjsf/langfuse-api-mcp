---
status: accepted
---
# Endpoint scope: every operation the deployment answers, except ingestion and org admin mutations

"Implements all endpoints" means every operation in the version-aware union catalog (ADR-0012) that the connected deployment answers, deprecated ones included, **except** two operations excluded on every deployment: `POST /api/public/ingestion` (legacy, shut down on Langfuse Cloud on 2026-11-16) and `POST /api/public/otel/v1/traces` (trace ingestion is the SDKs' job, not an agent's). Deprecated operations are no longer excluded here: ADR-0012 decides per deployment whether they answer. Organization-scoped operations (Organizations, Projects admin, SCIM) are exposed only when an organization-level key is configured, because project keys cannot call them.

## Organization operations (decided 2026-09-25, issue #1)

Organization **read** operations are exposed when an organization key is configured. Organization **admin mutations** are excluded from the catalog entirely, even in write mode: creating/updating/deleting projects, creating/deleting API keys, membership changes, and SCIM user create/update/delete. Minting or revoking credentials is the highest-impact action a prompt-injected agent could take, and the Langfuse UI remains the place for it.

## Consequences

- A test asserts the union catalog contains no excluded operation (ingestion, OTLP, org admin mutations), and ADR-0012's fixture test asserts the resolved set per deployment, so a spec update that adds an operation fails CI until it is triaged.

## Amendment: four write operations an agent cannot use safely (2026-09-27, M4 grilling)

Also excluded on every deployment, even in write mode:

- `media_getUploadUrl` and `media_patch`: a media upload is a PUT of the bytes straight to the presigned storage URL, which this server never makes; without it the two calls only leave half-registered media behind.
- `llmConnections_upsert` and `blobStorageIntegrations_upsertBlobStorageIntegration`: their bodies carry a third-party credential (the LLM provider's `secretKey`; the storage `accessKeyId`/`secretAccessKey`), which would pass through the model's context and the chat transcript as a tool argument — the same exposure the rule "keys never come from tool arguments" forbids for Langfuse keys. Their deletes stay. The rule is the credential, not the operation: any write whose request body schema has a property whose name contains `secret`, `password` or `accessKey` (case-insensitive) is excluded, and a catalog test fails when a regenerated spec adds one. `token` and `key` alone are not in the pattern: `models_create` has `tokenizerId`.

The catalog test that asserts no excluded operation is present covers these four.
