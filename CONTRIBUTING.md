# Contributing

How to build, test and check a change before you open a pull request. Deeper material lives in [`docs/development/`](docs/development/), listed in the [documentation index](docs/INDEX.md). Found a vulnerability? Follow [SECURITY.md](SECURITY.md), never a public issue.

## Setup and reading order

Start with [CLAUDE.md](CLAUDE.md) (agent and contributor index, which points to the design decisions and research), [CONTEXT.md](CONTEXT.md) (glossary) and [ROADMAP.md](ROADMAP.md).

## Run the checks locally

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs the same five checks on Linux, macOS and Windows for every push and pull request that changes more than docs (a change touching only Markdown files, `docs/`, `.claude/` or `LICENSE` starts no CI or integration run; Markdown under `skills/` and `docs/research/security.md` still starts CI, for the user skill check and the docs check below). Run them from the repository root before you push:

| Check | Command | What it proves |
|---|---|---|
| Build | `go build ./...` | everything compiles |
| Vet | `go vet ./...` | no suspicious constructs the compiler accepts |
| Lint | `golangci-lint run` | the rules in [`.golangci.yml`](.golangci.yml): security (`gosec`), closed response bodies, context use, no `InsecureSkipVerify` anywhere, and the package dependency direction (a lower package never imports a higher one) |
| Tests | `go test -race ./...` | tests pass under the race detector (needs a C compiler, which the race detector requires) |
| Vulnerabilities | `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` | no known vulnerability is reachable from the code |

**The cross-OS trust proof runs only in CI.** `TestExecutableTrustsTheOSStoreAndAmbientCAsAtOnce` (in `cmd/langfuse-mcp`) proves that the running executable trusts a CA from the OS certificate store and a CA exported through `SSL_CERT_FILE` at the same time, and that nothing loads certificates before `SSL_CERT_FILE`/`SSL_CERT_DIR` are captured and removed. It needs a test CA installed in the system trust store, so CI generates a throwaway one with [`scripts/gen-os-test-ca`](scripts/gen-os-test-ca/main.go) and installs it on each runner (Linux `update-ca-certificates`, macOS `security add-trusted-cert`, Windows `certutil -addstore Root`). Locally the test is skipped: never install that CA on your own machine. The rest of the proof (the two variables are gone from the environment after startup) runs everywhere.

What you need installed:

- **Any Go from 1.21 on.** `go.mod` pins the exact toolchain (`toolchain go1.27.1`). An older local Go downloads that toolchain automatically the first time you run a `go` command in this repository (the default `GOTOOLCHAIN=auto`).
- **golangci-lint v2 built with Go 1.27 or newer.** A binary built with an older Go refuses a `go 1.27` module: it exits with code 3, sometimes without printing anything. Check with `golangci-lint version` ("built with go1.27…"). If yours is older, build it with the module's toolchain:

  ```bash
  GOTOOLCHAIN=go1.27.1 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
  ```

Dependencies stay current through [Dependabot](.github/dependabot.yml) (Go modules, GitHub Actions and the generator's Python requirements). Dependabot does not bump the `toolchain` line, so a weekly workflow ([`.github/workflows/go-toolchain.yml`](.github/workflows/go-toolchain.yml)) opens a pull request when a newer Go 1.27.x patch release exists.

More on the checks, the release pipeline, the integration tests and the small-model eval: [CI checks in depth](docs/development/checks.md), [Release pipeline](docs/development/release-pipeline.md), [Integration tests and container stacks](docs/development/integration-tests.md), [Small-model discovery eval](docs/development/small-model-eval.md).

## Stack

| Part | Choice | Why |
|---|---|---|
| Language | Go | Single static binary for every OS, small memory footprint, full control of TLS trust |
| MCP SDK | [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) (official, Tier 1) | Supports MCP spec 2026-07-28; the stdio transport offers only the protocols negotiated through `initialize`, up to 2025-11-25 (see **Protocol.** under [Claude Code](docs/reference/configuration.md#claude-code--proven)) |
| API source of truth | Union catalog ([internal/catalog/spec/langfuse-union-catalog.json](internal/catalog/spec/langfuse-union-catalog.json)), built from the OpenAPI spec of every Langfuse release since v3.0.0 | Each operation carries its version range and operation family, and a write operation the JSON request body schema of its own release, which the `execute_write` body gate reads: object, size, depth, then full JSON Schema 2020-12 validation |
| Container | `gcr.io/distroless/static-debian13:nonroot`, pinned by digest | No shell, non-root |
