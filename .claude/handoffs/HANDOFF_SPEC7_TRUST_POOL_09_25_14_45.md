# Handoff: implement spec #7 (trust pool = OS roots + explicit/ambient CAs), the first spec on GitHub

**Created:** 2026-09-25 14:45 (UTC-3)
**Branch:** `main`, clean, in sync with `origin/main` at `7121c94`
**Session:** about 6 h of research, prototyping and planning. **No Go code exists yet.**

---

## Summary

M0 is complete: research, 12 ADRs, agent rules, glossary, roadmap, and an architecture diagram. Prototypes answered the evidence questions. The first spec to implement is **#7** (corporate TLS trust pool), split into tickets **#9 → #10 → (#11 ∥ #12) → #13**.

- **#9 bootstraps the Go module and CI.** Every other code ticket builds on it, including the M1 tracer **#18**.
- **Start with #9.**

---

## Work Completed (this session, all pushed)

- [x] Prototype **TLS capture-and-unset**: branch `prototype/tls-trust-pool`, `internal/trust/prototype_tlsproof/RESULTS.md`. Verified on Linux (3 distros) and Windows 11 with go1.27.1.
- [x] Prototype **REST io/metadata limits**: branch `prototype/langfuse-io-window` (4.46.0 and 3.80.0).
- [x] Prototype **API availability by version**: branch `prototype/langfuse-api-versions`. Contains:
  - `opmatrix.json`: 148 operations across 648 release tags;
  - runtime probes of 3.225.11 and 4.46.0 in `dual` mode.
- [x] Research docs:
  - `docs/research/go-tls-facts.md` §7 `[verified]`;
  - `docs/research/langfuse.md` §1.7–1.8;
  - `docs/research/langfuse-api-versions.md`.
- [x] ADR-0006 wording corrected. ADR-0012 rewritten as **version-aware catalog**. ADR-0004 reduced to exclusions only.
- [x] Specs and tickets:
  - #7 (TLS) → #9–#13;
  - #8 (REST limits) → #14–#15;
  - #17 (M1) → #18–#22.
  All tickets carry `ready-for-agent` and native `blocked_by` edges.
- [x] Issue #16 closed (version-aware catalog decided).

### Key Decisions

| Decision | Rationale | Alternatives |
|---|---|---|
| Capture `SSL_CERT_FILE`/`SSL_CERT_DIR`, then `os.Unsetenv` them, first thing in `main` (ADR-0006) | Go treats them as overrides. On Windows/macOS (Go 1.27) either one disables the platform verifier. On Linux, OS trust is lost only when **both** are set. Unsetting lets Go build the normal system pool; the server then appends the CAs. Works on every Go version. | `//go:debug x509sslcertoverrideplatform=0`: rejected, since the key is temporary (removed in Go 1.31) |
| No skip-verify option; TLS 1.2 minimum | Security requirement (OWASP); lint-enforced | — |
| Explicit source failure → startup error. Ambient source failure → warning and skip. | A stale, unrelated system variable must not block startup | — |
| Config file (ADR-0011) holds non-secret settings only; keys found in it → startup error | Credentials never live on disk in our file | — |
| Go 1.27 + `toolchain go1.27.1` | Latest release; Go 1.27 changed `SSL_CERT_*` handling on macOS/Windows | — |

---

## Files to read before coding (in this order)

- `CLAUDE.md`: definition of done, the "Always" list, Applied Learning.
- `CONTEXT.md`: use these terms verbatim (Trust pool, Explicit/Ambient CA source, Config file, Secret…).
- `.claude/rules/project-structure.md`: **binding** package tree and placement table.
  - `trust` holds the certificate logic.
  - `config` only carries paths.
  - `cmd` holds the capture step and the wiring.
  - `config` is imported only by `cmd`.
- `.claude/rules/go.md`, `testing.md`, `security.md`, `errors.md`, `engineering-process.md`, `docs-sync.md`.
- ADRs: `docs/adr/0006-tls-trust-in-code.md`, `0009-domain-oriented-package-layout.md`, `0011-non-secret-config-file.md`, `0001` (Go + SDK).
- `docs/research/go-tls-facts.md` §3 (Linux default paths) and §7 (executed matrix).
- Prototype code, useful as a reference (**never** merge it): `git show origin/prototype/tls-trust-pool:internal/trust/prototype_tlsproof/main.go`.
- Issues: `gh issue view 7`, then `9`, `10`, `11`, `12`, `13`.

---

## Ticket map for spec #7 (native GitHub dependencies)

| # | Title | Blocked by | Core of the work |
|---|---|---|---|
| 9 | Bootstrap Go module, lint rules and 3-OS CI | — | see below |
| 10 | Trust pool from explicit CA sources (file + directory) | 9 | see below |
| 11 | Ambient CA sources with `SSL_CERT_*` capture-and-unset | 10 | see below |
| 12 | Non-secret config file as a source of CA paths and settings | 10 | see below |
| 13 | Process-level proof on 3 OSes | 11 | see below |

**#9 — Bootstrap.**
- `go.mod` with `go 1.27` and `toolchain go1.27.1`. The go-sdk is **not** required yet.
- `.golangci.yml`: gosec, bodyclose, contextcheck, a ban on `InsecureSkipVerify`, and **depguard** rules for the dependency direction.
- CI on linux/macOS/windows: build, vet, lint, `go test -race`, `govulncheck`.
- `cmd/langfuse-mcp` starts and exits 0, proved by a test.
- README section "For contributors".
- Dependabot for Go modules, the toolchain and Actions.

**#10 — Explicit CA sources.**
- `config` reads `LANGFUSE_CA_CERT` and `LANGFUSE_CA_CERTS_PATH` as plain values.
- `trust` takes a sources value and returns pool + report, or an error.
- Directories are read non-recursively.
- Errors name the variable for missing, empty or non-PEM files and for an empty directory.
- The report contains no certificate bytes.

**#11 — Ambient sources.**
- One capture step reads the 5 variables (`SSL_CERT_FILE`, `SSL_CERT_DIR`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`).
- It splits `SSL_CERT_DIR` with `filepath.SplitList` and unsets only `SSL_CERT_FILE`/`SSL_CERT_DIR`.
- The report distinguishes explicit from ambient origin.
- `LANGFUSE_MCP_IGNORE_AMBIENT_CA=true` turns the ambient sources off.

**#12 — Config file.**
- Location: `os.UserConfigDir()/langfuse-mcp/config.env`, `KEY=VALUE`.
- Precedence: env > file > defaults.
- A malformed line → error with the line number. Keys in the file → error. An absent file is fine.
- A CA path from the file counts as an explicit source.

**#13 — Process-level proof.**
- A CI-only step installs an "OS" test CA into each runner's store.
- The test re-executes itself as a child with `SSL_CERT_FILE` set, and the child must reach both TLS servers.
- The child's environment must no longer contain the variables.
- A guard test covers the ordering.
- Closing #13 closes #4, with a link to the CI run.

**Parallel track:** once #9 is done, M1 **#18** (`execute_read` tracer, spec #17) can run in parallel with #10. #19–#22 depend on #18.

---

## Technical Context

### Seams agreed with the user (spec #7)

- **S1 — `trust` module.** A unit test on every OS, with no network:
  - generate throwaway CAs in the test;
  - serve TLS through `httptest.NewUnstartedServer` + `StartTLS` with a leaf signed by that CA;
  - assert that the handshake succeeds or fails with the returned pool.
- **S2 — process startup.** The test binary re-executes itself as a child (the `os.Args[0]` + env-flag pattern, as in the prototype) and asserts the connection and the environment.
- **Config precedence** is tested at the `config` seam with plain input maps. `config.Load(env, args)` is pure.

### Pool composition (the prototype proves it works)

```text
capture SSL_CERT_* → unset them → config.Load → trust.Build(sources)
  pool = x509.SystemCertPool()   // Linux: distro bundle; Win/macOS: platform verifier
         (fallback roots only if no system pool — distroless)
  pool.AppendCertsFromPEM(each explicit + ambient source)
tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
```

### Dependencies

- No third-party dependency is needed for #9–#13 beyond the standard library. `golang.org/x/crypto/x509roots/fallback` counts only if distroless needs it; justify it in the PR.
- The go-sdk (`github.com/modelcontextprotocol/go-sdk` v1.8.0) arrives in #18.

---

## Things to Know (gotchas verified this session)

- **Linux negative control.** Export **both** `SSL_CERT_FILE` and `SSL_CERT_DIR`. With only one, Go still loads the distro directories and the OS CA stays trusted, so the negative test would never fail. On Windows/macOS one variable is enough.
- **Ordering bug.** A `x509.SystemCertPool()` call before the unset caches the overridden roots, and a later unset does not help. That is the red case for the #13 guard test.
- **Init safety.** No package `init` in `net/http`, `crypto/tls` or `go-sdk/mcp` loads roots before `main` (verified).
- **Local toolchain.** Local Go is 1.26.0. Use `GOTOOLCHAIN=go1.27.1` (it auto-downloads) or rely on the `toolchain` line.
- **Testing Windows from WSL.** Build with `GOOS=windows` and run the `.exe` directly. WSL environment variables do not reach it (they need `WSLENV`), so set the variables inside the child process.
- **Never install test CAs on the developer's machine.** Only in CI or inside containers.
- **The Stop hook `.claude/hooks/docs-sync-check.sh`** blocks ending a turn when code changed and no doc did. Update README/CONTEXT/ROADMAP in the same commit.
- **The README marks unbuilt features "Planned".** Remove the marker only for what now works.
- **`git push` hung once this session.** Wrap it in `timeout 60`.
- **Matt Pocock flow.**
  - Red before green, one seam at a time.
  - Refactoring happens at `/code-review`.
  - `/implement` is user-started. Ask the user to run it, or use `/orchestrate` (all blocking edges are native, so no ticket gets picked up early).

---

## Current State

- **Working:** nothing executable yet. Only docs, rules and scripts: `scripts/setup-ci-langfuse-cloud.sh` (Cloud CI project wizard, not yet run by the user).
- **Tests:** none yet. #9 creates the first one.
- **Running processes:** none. All prototype Docker stacks were torn down.

---

## Next Steps

### Immediate (start here)

1. `gh issue view 9`, then read the rule files listed above.
2. Implement **#9**: module, lint (depguard encoding the dependency direction from `project-structure.md`), 3-OS CI with `govulncheck`, and a trivial `cmd/langfuse-mcp` plus its test. Update the README "For contributors" section.
3. Verify with fresh output: `go build ./...`, `go test -race ./...`, `golangci-lint run`, and the CI run green on all 3 OSes. Commit with docs in the same commit (definition of done).
4. **#10**: `trust` module with S1 tests, test-first. Then **#11** and **#12** (parallel is fine), then **#13**.

### Subsequent

- **M1:** #18, then #19–#22.
- **#14:** integration harness. Pin image digests; old v3 needs `postgres:17` and `clickhouse-server:24.3`; seed with OTLP/protobuf.
- **#15:** the Cloud io/metadata probe.
- Close **#4** after #13 is green, and **#2** after #15.

### Blocked on

- **#2 / #15 Cloud half:** the user must run `./scripts/setup-ci-langfuse-cloud.sh` (it creates the `LANGFUSE_TEST_*` GitHub secrets).
- **macOS proof:** only the #13 CI job can provide it (no local macOS).

---

## Commands

```bash
gh issue list --state open
gh issue view 7 && gh issue view 9
git show origin/prototype/tls-trust-pool:internal/trust/prototype_tlsproof/RESULTS.md
GOTOOLCHAIN=go1.27.1 go version
go build ./... && go test -race ./... && golangci-lint run
```

## Open Questions

- [ ] Distroless image (M5): whether `x509roots/fallback` is needed depends on the base image having CA certs (static-debian13 does). This can wait for M5.

---

_Start a new session, read this file, then `gh issue view 9`._
