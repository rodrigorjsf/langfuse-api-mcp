# PROTOTYPE — TLS trust pool with SSL_CERT_* capture-and-unset (throwaway)

Question (issues #4, #11, #13): with Go 1.27.1, does "capture `SSL_CERT_FILE`/`SSL_CERT_DIR`, then `os.Unsetenv` them at the top of `main`, then `x509.SystemCertPool()` + `AppendCertsFromPEM`" keep OS-store trust **and** trust the ambient CA? Run 2026-09-25.

## How it runs

`tlsproof gen <dir>` creates two throwaway CAs ("os" and "ambient") plus a 127.0.0.1 leaf per CA.
`tlsproof run <dir>` starts two in-process `httptest` TLS servers (one per CA) and re-executes itself as a child per case; each child dials `os-store-ca`, `ambient-ca` and `public` (`https://cloud.langfuse.com/api/public/health`) and prints JSON. The binary blank-imports `github.com/modelcontextprotocol/go-sdk/mcp` to show no package `init` loads system roots.

- Linux: `run-<distro>.sh` inside a container installs the "os" CA into the **container** trust store (never the host).
- Windows 11 (build 26200): the `GOOS=windows` binary runs natively via WSL interop. The "os" CA is **not** installed (that would need a GUI prompt and a host change); the `public` target is the OS-store proof (Windows platform verifier).
- macOS: not run (no host available) — covered by the #13 CI job.

Modes: `child-nounset` = no capture (Go's own behavior); `child-unset` = capture + unset first thing in `main`, then append; `child-late` = `SystemCertPool()` called once **before** the unset (the ordering bug the guard test must catch).

## Results (verbatim in `out/`)

| Case | Linux (debian trixie, alpine 3.22, fedora 43 — identical) | Windows 11 |
|---|---|---|
| no env | os ✅ ambient ❌ public ✅ | os ❌(not installed) ambient ❌ public ✅ |
| `SSL_CERT_FILE` only, no unset | os ✅ ambient ✅ public ✅ | ambient ✅ **public ❌** |
| `SSL_CERT_FILE` only, capture+unset | os ✅ ambient ✅ public ✅ | ambient ✅ public ✅ |
| `SSL_CERT_DIR` only, no unset | os ✅ ambient ✅ public ✅ | ambient ✅ **public ❌** |
| `SSL_CERT_DIR` only, capture+unset | os ✅ ambient ✅ public ✅ | ambient ✅ public ✅ |
| `SSL_CERT_FILE` only, late unset | os ✅ ambient ✅ public ✅ | ambient ✅ **public ❌** |
| both, no unset | ambient ✅ **os ❌ public ❌** | ambient ✅ **public ❌** |
| both, capture+unset | os ✅ ambient ✅ public ✅ | ambient ✅ public ✅ |
| both, late unset | ambient ✅ **os ❌ public ❌** | ambient ✅ **public ❌** |

(One alpine run had a transient network timeout on `public` in the "both, no unset" case; the re-run in `out/` shows the TLS failure.)

## Verdict

1. **Design confirmed** on Linux (3 distros) and Windows 11: capture + unset + append trusts the OS store and the ambient CA; the child's environment no longer contains either variable.
2. **Linux nuance (corrects ADR-0006 wording):** each variable replaces only its own list — `SSL_CERT_FILE` replaces the bundle-file list, `SSL_CERT_DIR` the directory list. Distros keep individual certs (or the bundle) in `/etc/ssl/certs` / `/etc/pki/tls/certs`, so OS trust is lost only when **both** are exported. On Windows (Go 1.27) **either** variable alone disables the platform verifier.
3. **Ordering matters:** once `SystemCertPool()` has run with the variables set, a later unset does not help (roots are cached). The #13 guard test's negative case must export **both** variables on Linux to be meaningful; on Windows/macOS one is enough.
4. No package `init` (net/http, crypto/tls, go-sdk mcp) loaded roots before `main`.
