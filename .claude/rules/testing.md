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
- Security properties are tests, not comments: write tool absent by default; GET-only `execute_read`; redirect to another host refused; secrets absent from logs/results; oversized responses truncated.
- Integration tests (`//go:build integration`): run against a self-hosted Langfuse from the official `docker compose` in CI on every PR, and against a dedicated Langfuse Cloud test project weekly + on manual dispatch (keys only in GitHub secrets `LANGFUSE_TEST_PUBLIC_KEY`/`LANGFUSE_TEST_SECRET_KEY`/`LANGFUSE_TEST_BASE_URL`). Never point tests at a project with real data.
- Golden files live in `testdata/`; update with an explicit `-update` flag, never silently.
- `go test -race ./...` must pass on linux, macOS and windows.
