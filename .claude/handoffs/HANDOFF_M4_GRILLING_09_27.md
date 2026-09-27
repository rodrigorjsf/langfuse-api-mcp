# Handoff: M4 Gated writes — grilling closed, ready for `/to-spec`

Date: 2026-09-27. Input for `/to-spec` on milestone M4 (no open M4 issues to absorb; M5–M7 issues stay where they are).

## Decisions (see the ADR-0003 and ADR-0004 amendments, CONTEXT.md, ROADMAP.md M4)

- Catalog: every in-scope write operation, minus `media_getUploadUrl`, `media_patch`, `llmConnections_upsert`, `blobStorageIntegrations_upsertBlobStorageIntegration` (ADR-0004 amendment; 51 write operations remain).
- Destructive operation = DELETE, PUT, PATCH. It runs only after a confirmation. POST is not confirmed by the server.
- Confirmation: multi round-trip `InputRequests` with a signed `RequestState` (HMAC over tool + canonical args + expiry, per-process random key). Fail-closed when the client advertises no elicitation or only URL mode. Decline/cancel is a tool error; nothing is sent.
- One `execute_write` tool (`readOnlyHint:false`, `destructiveHint:true`, `idempotentHint:false`, `openWorldHint:true`); the create/update/delete split is a recorded deviation, reassessed in M7.
- Body check: the generator converts the OpenAPI 3.0 schemas to 2020-12; `internal/catalog` validates with `github.com/google/jsonschema-go` in production (update `.claude/rules/go.md`, which allows it only in tests today).
- `describe_operation` shows the write operation's body schema, in write mode only.
- Audit: a write call logs at `Warn` with `confirmation` = `accepted` / `declined` / `not_required`.
- Live tests: self-hosted 4.46.0 on every PR runs a write cycle through `execute_write` with a go-sdk client that accepts the elicitation (prompt create, PATCH label, DELETE; score create). No writes against Cloud.
- Later: an operation allowlist/denylist in config. Not in M4: small-model eval of writes.
- Execution: `run-spec`, three tickets in sequence.

## Planned slices (1 → 2 → 3)

1. **Tracer bullet, non-destructive POST.** `LANGFUSE_MCP_ALLOW_WRITES` parsed; `execute_write` registered only with it (absence and presence tests); `Client.Do` sends a JSON body with `Content-Type`; body must be a JSON object, size- and depth-capped; the four exclusions plus the credential-property catalog test; audit at `Warn`; every destructive operation refused from day one; live `scores_create`.
2. **Body schema check.** Generator conversion, catalog regenerated, weekly regeneration job stays green; validation with a static refusal that names the JSON path and keyword, never the value; `describe_operation` shows the body schema.
3. **Confirmation.** Signed `RequestState` flow for PUT/PATCH/DELETE; capability check; confirmation text; Folder-name allow-list entries for write operations with `promptName`/`datasetName`; live prompt create → PATCH label → DELETE.

## Acceptance criteria the spec must carry (from the adversarial pass)

- Every in-scope body schema compiles under `jsonschema-go` after conversion, tested over the whole catalog. `nullable` is not the only OpenAPI 3.0 difference: boolean `exclusiveMinimum`/`exclusiveMaximum` would break compilation too.
- The confirmation text says when the body summary was cut and gives the body size. For `trace_deleteMultiple` it gives the ID count. Injected text past the cut must not be hidden silently.
- A client that advertises only URL-mode elicitation is treated as unable to confirm.
- The prompt-injection and dangerous-parameter cases of `.claude/rules/security.md` "Every slice", per slice. Confirmation-specific cases: an accept sent on the first call, an accept re-sent with other arguments, a forged or expired state, hidden/bidi characters and markup in arguments shown in the confirmation text.
- Stale facts to fix in the slice that touches them: `catalog.go:10` says the JSON is "minus the ADR-0004 exclusions" (it is not; they are dropped at load); `writes_disabled` in `.claude/rules/errors.md` has no code; `security.md` promises a higher log level for writes.

## Evidence

- Prototype: branch `prototype/m4-mrtr-confirmation`, `internal/mrtrproto/`. Without the signature, an accept on the first call deleted `victim`, and an accept re-sent with other arguments deleted `b` after the user saw `a`. With it, both were refused with `confirmation_invalid`, a client without elicitation got `confirmation_unavailable`, and an identical replay inside the expiry passed.
- go-sdk v1.8.0: `ServerSession.Elicit` errors on protocol 2026-07-28 (`mcp/server.go:1619-1628`); `CallToolResult.InputRequests`/`RequestState` (`mcp/protocol.go:307-318`); server middleware for older clients (`mcp/mrtr.go:122-165`).
- Credential properties: `llmConnections_upsert` has `secretKey`; `blobStorageIntegrations_upsertBlobStorageIntegration` has `accessKeyId` and `secretAccessKey` (union catalog JSON).
