---
paths:
  - "**/*_test.go"
  - "internal/**/testdata/**"
---
# Tests

- Test at the module's interface (the seam); name tests after a capability using `CONTEXT.md` terms; one logical assertion per test.
- Langfuse is a true external: fake it with `httptest.Server` (or `httptest.NewTLSServer` + a test CA for TLS slices). Never call a real Langfuse in unit tests; a real instance is only for opt-in integration tests behind a build tag `integration`.
- Tests live beside the code (`project-structure.md`); fixtures in `<package>/testdata/`.
- MCP surface: drive the server through the go-sdk in-memory client transport, asserting on `tools/list` (names, annotations, write tool absent/present) and `tools/call` results.
- Security properties are tests, not comments: write tool absent by default; GET-only `execute_read`; redirect to another host refused; secrets absent from logs/results; oversized responses truncated; unknown/unsafe params refused before any request; instruction-like text and hidden characters in payloads come back only inside the untrusted envelope, stripped (`security.md` "Every slice").
- Integration tests (`//go:build integration`, files `<package>/*_integration_test.go`): run against a self-hosted Langfuse 4.46.0 `events_only` from the official `docker compose` in CI on every PR; weekly + on manual dispatch against each pinned deployment (`LANGFUSE_DEPLOYMENT`: 3.80.0, 3.225.11, 4.46.0 `events_only`/`dual`; expected profiles in `pinnedDeployments`) and a dedicated Langfuse Cloud test project (`.github/workflows/integration.yml`; keys only in GitHub secrets `LANGFUSE_TEST_PUBLIC_KEY`/`LANGFUSE_TEST_SECRET_KEY`/`LANGFUSE_TEST_BASE_URL`). Never point tests at a project with real data. Set up or rotate the Cloud project with `scripts/setup-ci-langfuse-cloud.sh` (writes `.env.integration` + the 3 secrets).
- Live tests start from `liveSession`, or from `liveClient` when they need client options or a resolved catalog (the per-deployment check); both skip with a message naming the unset `LANGFUSE_TEST_*` variables. They call through `readLive` (setup calls straight to Langfuse: `liveTarget.send`), which waits `Retry-After` once on a 429 and never retries blind; keep Cloud calls to a minimum (Hobby: 30 req/min).
- Run locally: self-hosted — `scripts/langfuse-selfhosted.sh up` (writes `.env.integration.selfhosted`; images pinned by digest there), then `set -a; . ./.env.integration.selfhosted; set +a; go test -tags integration -count=1 ./internal/server/`, then `scripts/langfuse-selfhosted.sh down`. Cloud — the same `go test` after `set -a; . ./.env.integration; set +a`.
- Golden files live in `testdata/`; update with an explicit `-update` flag, never silently.
- `go test -race ./...` must pass on linux, macOS and windows.
