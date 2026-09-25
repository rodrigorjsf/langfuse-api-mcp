# TLS, custom CAs & corporate proxies

> Decision: [ADR-0006](../adr/0006-tls-trust-in-code.md). Labels as in [stack-and-sdk.md](stack-and-sdk.md).

## Why the official tooling breaks (hypothesis)

Node.js uses Mozilla's bundled roots and **ignores the OS store** unless started with `NODE_USE_SYSTEM_CA=1` / `--use-system-ca` (v22.19+/v24.6+) or `NODE_EXTRA_CA_CERTS=<pem>`; it **ignores `HTTP(S)_PROXY`/`NO_PROXY`** unless `NODE_USE_ENV_PROXY=1` / `--use-env-proxy` (fetch v22.21+/v24.0+, http/https v22.21+/v24.5+). `[verified 3-0]` https://nodejs.org/learn/http/enterprise-network-configuration

An MCP host that launches a Node-based server without those variables fails behind a TLS-intercepting proxy. **Inference, not reproduced.**

## Per-language trust behavior

| Runtime | Default roots | Add a corporate CA | Label |
|---|---|---|---|
| Node | bundled Mozilla | `NODE_EXTRA_CA_CERTS`, `NODE_USE_SYSTEM_CA` (launcher must set) | `[verified 3-0]` |
| Go | OS store (platform verifier on macOS/Windows; files on Linux) | in code: `x509.SystemCertPool()` + `AppendCertsFromPEM` | `[verified 3-0]` |
| Rust | depends on crate | `rustls-platform-verifier` `Verifier::new_with_extra_roots` | `[verified 3-0]` |
| Python | `certifi` bundle | `truststore` (3.10+, app must opt in) | `[verified 2-1]` |

## Go details that shape the design

- `SSL_CERT_FILE` / `SSL_CERT_DIR` override default locations; **since Go 1.27, setting either on macOS/Windows disables platform verification** (OS store no longer consulted) unless `GODEBUG=x509sslcertoverrideplatform=0`. `SSL_CERT_DIR` replaces, not extends. `[verified 3-0]` https://pkg.go.dev/crypto/x509 → never ask users to set them; load CAs in code.
- `SystemCertPool()` returns an in-memory copy; appending does not touch the host. `[sourced]`
- `AppendCertsFromPEM` reports whether any cert parsed → fail loudly on an empty/invalid CA file. `[sourced]`
- `SetFallbackRoots` (e.g. `golang.org/x/crypto/x509roots/fallback`) supplies roots only when no system pool exists (scratch/distroless). Callable once. `[sourced]`
- `SSL_CERT_DIR` is a colon-separated list (semicolon on Windows). Run 1 refuted this 1-2; `security.md` later settled it from pkg.go.dev `[sourced]`. The ambient-CA loader must split it the same way.

## Containers

- Copying `/etc/ssl/certs` from a builder into distroless did not fix a Go controller's `unknown authority` in a reported case (no maintainer answer). `[sourced — unverified]` https://github.com/GoogleContainerTools/distroless/issues/668 → mount the CA and pass it via `LANGFUSE_CA_CERT`/`LANGFUSE_CA_CERTS_PATH` instead of relying on image trust stores.
- `ko` pattern: bundle CAs in `kodata` + `SSL_CERT_DIR`. `[sourced]` https://ko.build/advanced/root-ca-certificates/ — not adopted (see Go 1.27 note).

## Open questions

- Does `SystemCertPool()` + appended roots behave identically under the macOS/Windows platform verifier on Go 1.27? → CI matrix on all three OSes with a test CA.
- Resolved in ADR-0006 (revised 2026-09-25): read then `os.Unsetenv` `SSL_CERT_FILE`/`SSL_CERT_DIR` at the top of `main` and append `SSL_CERT_FILE`/`SSL_CERT_DIR` (plus `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`) as *ambient* CA sources. Facts in [go-tls-facts.md](go-tls-facts.md).
- Which MCP hosts pass the user's environment to stdio servers (Claude Code, Claude Desktop, Cursor, VS Code, Codex; macOS GUI launch vs terminal launch)? Needed for the README per-harness table. `[open]`
