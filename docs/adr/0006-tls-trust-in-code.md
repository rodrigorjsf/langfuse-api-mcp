---
status: accepted
---
# TLS trust built in code: system pool + explicit and ambient CA sources; no insecure mode

The HTTP client's root pool is `x509.SystemCertPool()` plus every certificate found in these sources, appended in code with `AppendCertsFromPEM` (union — every source that is set contributes):

| Priority | Variable | Kind | Why |
|---|---|---|---|
| explicit | `LANGFUSE_CA_CERT` | PEM file (bundle) | this server's own knob |
| explicit | `LANGFUSE_CA_CERTS_PATH` | directory of PEM files | this server's own knob |
| ambient | `SSL_CERT_FILE`, `SSL_CERT_DIR` | file / dir | OpenSSL/Go convention |
| ambient | `NODE_EXTRA_CA_CERTS` | file | often already set for Node tooling in corporate setups |
| ambient | `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE` | file | Python / curl conventions |

All variables are read from the process environment, so a value exported system-wide (shell profile, Windows user/system env, container env) is used without being declared in the MCP host's JSON `env` block; the JSON block is only needed when the host does not pass the variable through. An **explicit** source that yields zero certificates or cannot be read is a startup error; an **ambient** source that fails is logged (stderr, path only) and skipped, so a stale unrelated variable never blocks startup. `LANGFUSE_MCP_IGNORE_AMBIENT_CA=true` disables the ambient sources. There is no "skip TLS verification" option — not even an opt-in flag. Proxies come from `HTTPS_PROXY`/`HTTP_PROXY`/`NO_PROXY` via `http.ProxyFromEnvironment`.

## Why not just let Go read `SSL_CERT_FILE`/`SSL_CERT_DIR`

Since Go 1.27, when either is set on macOS/Windows Go stops using the platform verifier and *replaces* the OS store with that file — a corporate user with `SSL_CERT_FILE` exported would silently lose every other OS-trusted root. The main package therefore sets `//go:debug x509sslcertoverrideplatform=0` (keep the platform verifier) and the server appends those files itself.

## Consequences

- Requires Go ≥ 1.27 in `go.mod` (the `//go:debug` key must exist in the toolchain) — `[sourced — unverified]`, confirm in the TLS slice.
- Whether a host passes the user's environment to a stdio server differs per harness (e.g. GUI apps on macOS do not see shell-profile exports). README documents per-harness behavior; the JSON `env` block remains the portable fallback.
- Docker: ambient variables must be passed with `-e`, and the file mounted (`-v`).
- The startup log lists which CA sources were loaded (paths and counts, never contents).
- Behavior of `SystemCertPool()` + appended roots under the macOS/Windows platform verifier is covered by a CI matrix on all three OSes.
