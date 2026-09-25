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

## Why the server removes `SSL_CERT_FILE`/`SSL_CERT_DIR` from its own environment

Go treats both variables as an **override** of its default locations on every OS (they are independent: each replaces only its own list). On Linux an exported `SSL_CERT_FILE` replaces the distro bundle; since Go 1.27 on macOS/Windows it also disables the platform verifier. So the first thing `main` does — before any code touches `crypto/x509` — is read both variables as ambient CA sources and then `os.Unsetenv` them for this process only. Go then builds its normal system pool (distro bundle / platform verifier) and the server appends the ambient and explicit CAs. This works on every Go version and does not depend on the temporary `x509sslcertoverrideplatform` GODEBUG key (added in 1.27, planned removal in 1.31). Facts: `docs/research/go-tls-facts.md`.

## Consequences

- A test must prove no code path touches `crypto/x509` before the unset, and CI proves on linux/macOS/windows that OS roots **and** appended CAs are both trusted when `SSL_CERT_FILE` was exported.
- Whether a host passes the user's environment to a stdio server differs per harness (e.g. GUI apps on macOS do not see shell-profile exports). README documents per-harness behavior; the JSON `env` block remains the portable fallback.
- Docker: ambient variables must be passed with `-e`, and the file mounted (`-v`).
- The startup log lists which CA sources were loaded (paths and counts, never contents).
