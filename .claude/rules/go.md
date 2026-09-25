---
paths:
  - "**/*.go"
  - "go.mod"
  - "go.sum"
---
# Go conventions

- Target the Go version in `go.mod`; `gofmt`/`goimports` clean; `golangci-lint run` and `go vet ./...` pass; `govulncheck ./...` clean before release.
- Standard library first (`net/http`, `crypto/tls`, `crypto/x509`, `encoding/json`, `log/slog`). New dependency only with a one-line justification in the PR; pin via `go.sum`.
- Packages under `internal/`; `cmd/` only wires. No package named `util`/`common`/`helpers`.
- Accept interfaces at the seam, return concrete types; define an interface in the consumer, and only when there is a second implementation or a test seam.
- `context.Context` is the first parameter of every I/O function; honor cancellation; set timeouts on every HTTP call.
- Errors: wrap with `fmt.Errorf("…: %w", err)`; sentinel/typed errors for cases callers branch on; never `panic` on input.
- Logging: `log/slog` to **stderr** only (stdout is the stdio transport); redact secrets and Authorization headers.
- TLS: build `tls.Config.RootCAs` in code (ADR-0006); never set `InsecureSkipVerify`; `MinVersion: tls.VersionTLS12`.
- Tests (details in `testing.md`): table-driven, `t.Parallel()` where safe, `httptest.Server` for Langfuse, no network in unit tests; `go test -race ./...` in CI on linux, macOS, windows.
- Cross-platform: `filepath` not `path` for files; no shell-outs; no OS-specific code outside `_windows.go`/`_unix.go` files.
