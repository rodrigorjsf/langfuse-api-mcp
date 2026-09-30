---
paths:
  - "**/*.go"
  - "go.mod"
  - "go.sum"
---
# Go conventions — safe, lean, fast end to end

Measure before optimizing; never trade a security control for speed. Error handling has its own rule: `errors.md`.

## Baseline
- `go.mod`: `go 1.27` + `toolchain go1.27.1` (`.github/workflows/go-toolchain.yml` bumps it to the latest 1.27.x; Dependabot cannot); CI builds releases with the latest 1.27.x; `gofmt`/`goimports` clean; `go vet ./...`, `golangci-lint run` (with `gosec`, `staticcheck`, `errcheck`, `bodyclose`, `noctx`, `contextcheck`) and `govulncheck ./...` pass.
- Standard library first (`net/http`, `crypto/*`, `encoding/json`, `log/slog`, `context`). Allowed extras: `golang.org/x/sync` (errgroup), `golang.org/x/time/rate`, `golang.org/x/crypto/x509roots/fallback`, `golang.org/x/net/http/httpproxy` (proxy selection from resolved settings, ADR-0006), test-only `go.uber.org/goleak`, `github.com/google/jsonschema-go` (the go-sdk's own schema library, already pinned): in tests, to validate `structuredContent` against an `outputSchema`; allowed in `internal/catalog` production code for write request body validation against the JSON Schema 2020-12 body schema (no stdlib alternative exists; #111, used since #112); allowed in `scripts/registry-server-json` to validate the MCP Registry entry offline against the Registry's published draft-07 schema (#154: the Registry's own `mcp-publisher validate` calls the live Registry, which must never block a release). Anything else needs a justification in the PR.
- Packages under `internal/`; `cmd/` only wires. No `util`/`common`/`helpers` packages. No package-level mutable state (one exception: `main.version`, the linker's `-X` target, set at build time and never changed at run time); config and catalog are built once at startup and shared **read-only** (no locks needed).
- Accept interfaces at a seam, return concrete types; declare an interface in the consumer only when two adapters exist.

## Security
- No `unsafe`, no cgo (`CGO_ENABLED=0` builds; only `go test -race` needs cgo, because the race detector does), no `os/exec` in production code (tests may start the test binary as a child process, spec #7, a binary they `go build` with an injected version, or the installed artifact under test, spec #119 seam S2; depguard enforces both), no `reflect` on untrusted input, no `text/template`/`html/template` over payloads.
- Randomness for secrets: `crypto/rand` only. Token comparison: `crypto/subtle.ConstantTimeCompare`.
- TLS: `tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}`; `InsecureSkipVerify` must never appear in the codebase (lint rule).
- Validate every tool input before use: enums, bounds, formats, max lengths; reject unknown fields. Guard integer conversions (`limit`, page) against overflow and negative values.
- Secrets live in a type whose `Format()`/`String()`/`LogValue()`/`MarshalJSON()` render `[REDACTED]`, so no fmt verb (`%d` and `%t` included), `slog` or JSON leaks them.
- Fuzz (`go test -fuzz`) every parser of untrusted input: config parsing, operation-param validation, output sanitizer, error-body parsing.

## Memory and I/O
- One shared `*http.Client` with a tuned `*http.Transport` (connection reuse, `MaxIdleConnsPerHost`, `TLSHandshakeTimeout`, `ResponseHeaderTimeout`, `IdleConnTimeout`, `ForceAttemptHTTP2`); never `http.DefaultClient`, never a client per request.
- Every response body: read through `io.LimitReader(body, maxBytes+1)` (detect overflow), decode by streaming with `json.NewDecoder`, then drain and `Close()` in a `defer`. No unbounded `io.ReadAll`.
- Keep upstream JSON as `json.RawMessage` when the server only forwards it; unmarshal into typed structs only for fields it acts on.
- Preallocate slices/maps when size is known; `strings.Builder`/`bytes.Buffer` for assembly; `sync.Pool` only with a benchmark proving it helps.
- Builds: `-trimpath -ldflags="-s -w"`, static binary. Respect `GOMEMLIMIT` in containers (document it in `docs/reference/installation.md`).
- Performance claims need `go test -bench . -benchmem` numbers or a `pprof` profile in the PR.

## Concurrency
- Goroutines only where they buy latency (e.g. a workflow tool fetching observations and scores in parallel). Always bounded: `errgroup.WithContext` + `SetLimit`, fed by the request `context.Context`.
- Every goroutine has an owner and an exit path through `ctx.Done()`; no fire-and-forget. Tests use `goleak.VerifyNone`.
- Outbound calls share one `rate.Limiter` and a concurrency cap (security.md); cancellation of the MCP request cancels every in-flight Langfuse call.
- Prefer immutable data over mutexes; if a mutex is needed, keep it unexported next to the data it guards.
- `context.Context` is the first parameter of every I/O function; every HTTP call has a deadline.

## Logging
- `log/slog` JSON handler to **stderr** only (stdout is the stdio transport). Metadata only: tool, operationId, status, latency, bytes. Never payloads, never secrets.

## Cross-platform
- `filepath` for file paths, `os.ReadFile`/`os.ReadDir` for CA sources; OS-specific code only in `_windows.go` files and their `_other.go` twins under `//go:build !windows` (Go reads no `_unix` filename suffix); path-list variables split with `filepath.SplitList`.

## Tests
- Details in `testing.md`: table-driven, `t.Parallel()` where safe, `httptest.Server` for Langfuse, no network in unit tests.
