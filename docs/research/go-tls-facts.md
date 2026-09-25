# Go TLS / X.509 root-store facts (fact-finding, no decisions)

Access date for every source below: **2026-09-25**. Primary sources only (go.dev,
pkg.go.dev, proxy.golang.org, Go source on github.com/golang/go at tag `go1.27.1`,
golang.org/x/crypto source, modelcontextprotocol/go-sdk). Labels: `[sourced]` = quoted
from the cited primary source; `[sourced — unverified]` = inference or a point the
source does not state explicitly. Nothing in this document was executed as a Go
program; no claim is `[verified]`.

## 1. Go 1.27 release status and SSL_CERT_* release-note text

**Go 1.27 is released.** `[sourced]`

- https://go.dev/doc/devel/release — "go1.27.0 (released 2026-08-19) — Go 1.27.0 is a
  major release of Go." and "go1.27.1 (released 2026-09-01) includes fixes to cgo, the
  compiler, the runtime, the go fix command, and the database/sql, debug/elf,
  encoding/json, net/http, os, simd, and simd/archsimd packages."
- https://go.dev/dl/?mode=json — lists exactly two stable versions: `"version":
  "go1.27.1", "stable": true` and `"go1.26.8"`.

**Latest stable: go1.27.1** (previous line still supported: go1.26.8). `[sourced]`

Release notes, https://go.dev/doc/go1.27, section `crypto/x509` (quoted verbatim,
including the source's own "loaded from disk and instead of" wording): `[sourced]`

> SystemCertPool now respects SSL_CERT_FILE and SSL_CERT_DIR on Windows and Darwin.
> When these environment variables are set, roots are loaded from disk and instead of
> using the platform certificate verification APIs, the native Go verifier is used.
> This behavior can be disabled with GODEBUG=x509sslcertoverrideplatform=0.

## 2. GODEBUG `x509sslcertoverrideplatform`

### 2.1 Existence and version `[sourced]`

https://go.dev/doc/godebug, "Go 1.27" history section:

> Go 1.27 added a new x509sslcertoverrideplatform setting that controls whether
> crypto/x509 will load roots from disk on Windows and Darwin when SSL_CERT_FILE or
> SSL_CERT_DIR are set. The default value x509sslcertoverrideplatform=1 will cause roots
> to be loaded from disk when these environment variables are set. Setting
> x509sslcertoverrideplatform=0 disables this behavior in favor of using the platform
> certificate store instead of honoring the environment variables. We plan to remove
> this setting in Go 1.31.

Go source table, https://github.com/golang/go/blob/go1.27.1/src/internal/godebugs/table.go#L79:

```go
{Name: "x509sslcertoverrideplatform", Package: "crypto/x509", Changed: 27, Old: "0"},
```

with the field meanings (same file, L14-15):

```go
Changed   int    // minor version when default changed, if any; 21 means Go 1.21
Old       string // value that restores behavior prior to Changed
```

### 2.2 Settable via `//go:debug` in package main `[sourced]`

It is a registered setting (it is in `internal/godebugs.All`), so the general
mechanism applies. https://go.dev/doc/godebug, "Default GODEBUG Values":

> starting in Go 1.21, a main package's source files can include one or more //go:debug
> directives at the top of the file (preceding the package statement).

> Starting in Go 1.21, the Go toolchain treats a //go:debug directive with an
> unrecognized GODEBUG setting as an invalid program. Programs with more than one
> //go:debug line for a given setting are also treated as invalid. (Older toolchains
> ignore //go:debug directives entirely.)

> When testing a package, //go:debug lines in the *_test.go files are treated as
> directives for the test's main package. In any other context, //go:debug lines are
> ignored by the toolchain; go vet reports such lines as misplaced.

Consequence `[sourced — unverified]` (inference from the two quotes above, not stated
for this specific setting): `//go:debug x509sslcertoverrideplatform=0` is accepted by a
Go 1.27+ toolchain; a Go 1.26 or older toolchain (1.21-1.26) would reject it as an
unrecognized setting, making the program invalid. The same holds for a `godebug` block
in go.mod (Go 1.23+): "It is an error to list a godebug with an unrecognized setting."

Check what is compiled in: `go list -f '{{.DefaultGODEBUG}}' my/main/package` — "Only
differences from the base Go toolchain defaults are reported." `[sourced]`

### 2.3 How the go.mod `go` line changes the default `[sourced]` + inference

https://go.dev/doc/godebug:

> When a GODEBUG setting is not listed in the environment variable, its value is
> derived from three sources: the defaults for the Go toolchain used to build the
> program, amended to match the Go version listed in go.mod, and then overridden by
> explicit //go:debug lines in the program.

> When compiling a work module or workspace that declares an older Go version, the Go
> toolchain amends its defaults to match that older Go version as closely as possible.
> [...] As an exception, GODEBUGs introduced for security releases will have the new
> behavior apply to all versions.

> Only the work module's go.mod is consulted for godebug directives. Any directives in
> required dependency modules are ignored.

Applied to this setting (inference from `Changed: 27, Old: "0"`; `[sourced — unverified]`
as a stated rule for this setting, but it is the documented mechanism):

| Work module's go.mod `go` line | Toolchain | Effective default | Effect on macOS/Windows when SSL_CERT_FILE/DIR set |
|---|---|---|---|
| `go 1.27` or newer | 1.27.x | `x509sslcertoverrideplatform=1` | env vars honored; roots loaded from disk; pure-Go verifier; platform verifier NOT used |
| `go 1.26.x` or older | 1.27.x | `x509sslcertoverrideplatform=0` (the `Old` value) | env vars ignored; platform verifier used (pre-1.27 behavior) |
| any | 1.26.x or older | setting does not exist | env vars ignored on macOS/Windows (pre-1.27 behavior) |

The setting was introduced in a major release (1.27), not a security point release, so
the "security release" exception does not apply `[sourced — unverified]`. Only the
**main (work) module's** `go` line matters; a dependency declaring `go 1.25.0` (e.g.
go-sdk, §6) has no effect. The setting has no effect on Linux at all — see the
`runtime.GOOS` gate in §3.

## 3. Linux default cert paths and SSL_CERT_FILE / SSL_CERT_DIR override `[sourced]`

https://github.com/golang/go/blob/go1.27.1/src/crypto/x509/root_linux.go#L9-L32:

```go
// Possible certificate files; stop after finding one.
var certFiles = []string{
	"/etc/ssl/certs/ca-certificates.crt",                // Debian/Ubuntu/Gentoo etc.
	"/etc/pki/tls/certs/ca-bundle.crt",                  // Fedora/RHEL 6
	"/etc/ssl/ca-bundle.pem",                            // OpenSUSE
	"/etc/pki/tls/cacert.pem",                           // OpenELEC
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // CentOS/RHEL 7
	"/etc/ssl/cert.pem",                                 // Alpine Linux
}

// Possible directories with certificate files; all will be read.
var certDirectories = []string{
	"/etc/ssl/certs",     // SLES10/SLES11, https://golang.org/issue/12139
	"/etc/pki/tls/certs", // Fedora/RHEL
}
```

(An `init()` appends `/system/etc/security/cacerts` and `/data/misc/keychain/certs-added`
only when `goos.IsAndroid == 1`.)

**Location change in Go 1.27:** the env-var logic is no longer in `root_unix.go`. In
go1.27.1, `root_unix.go` only contains a no-op `systemVerify`; the loading moved to
`root.go` (shared by all platforms). In go1.26.8 it was still in `root_unix.go`
(`loadSystemRoots`, L32-58). `[sourced]`

https://github.com/golang/go/blob/go1.27.1/src/crypto/x509/root.go#L124-L204:

```go
certFileEnv = "SSL_CERT_FILE"
...
certDirEnv = "SSL_CERT_DIR"
...
func loadSystemRoots() (*CertPool, error) {
	certFilePath, certDirPath := os.Getenv(certFileEnv), os.Getenv(certDirEnv)

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		if certFilePath == "" && certDirPath == "" {
			return &CertPool{systemPool: true}, nil
		}
		if x509sslcertoverrideplatform.Value() == "0" {
			x509sslcertoverrideplatform.IncNonDefault()
			return &CertPool{systemPool: true}, nil
		}
	}

	return loadOnDiskRoots(certFilePath, certDirPath)
}

func loadOnDiskRoots(certFilePath, certDirPath string) (*CertPool, error) {
	roots := NewCertPool()

	files := certFiles
	if certFilePath != "" {
		files = []string{certFilePath}
	}
	...
	dirs := certDirectories
	if certDirPath != "" {
		// OpenSSL and BoringSSL both use ":" as the SSL_CERT_DIR separator on
		// Unix-like systems, and ";" on Windows.
		dirs = filepath.SplitList(certDirPath)
	}
	...
	if roots.len() > 0 || firstErr == nil {
		return roots, nil
	}

	return nil, firstErr
}
```

Facts read directly from this code `[sourced]`:

- `SSL_CERT_FILE` **replaces** the whole `certFiles` list (not appended).
- `SSL_CERT_DIR` **replaces** the whole `certDirectories` list; it is a list split by
  `filepath.SplitList` (`:` on Unix, `;` on Windows).
- The two are **independent**: setting only `SSL_CERT_FILE` still reads every file in
  the default `certDirectories` (`/etc/ssl/certs`, `/etc/pki/tls/certs`); setting only
  `SSL_CERT_DIR` still reads the first existing default `certFiles` entry. To fully
  isolate from the distro store, both must point at controlled locations
  `[sourced — unverified]` as a recommendation; the mechanics are sourced.
- Missing files/dirs (`os.IsNotExist`) are skipped silently. If nothing loads and no
  other error occurred, an **empty** pool is returned with nil error.

`SystemCertPool` doc (https://github.com/golang/go/blob/go1.27.1/src/crypto/x509/cert_pool.go#L106-L120,
also rendered at https://pkg.go.dev/crypto/x509#SystemCertPool):

> The environment variables SSL_CERT_FILE and SSL_CERT_DIR can be used to override the
> system default locations for the SSL certificate file and SSL certificate files
> directory, respectively. The latter can be a colon-separated list, or a
> semicolon-separated list on Windows. On platforms which have system APIs for
> certificate verification (macOS and Windows), setting SSL_CERT_FILE or SSL_CERT_DIR
> will prevent those APIs from being used, unless the x509sslcertoverrideplatform=0
> GODEBUG setting is used. (This changed in Go 1.27.)

## 4. `golang.org/x/crypto/x509roots/fallback` and `x509.SetFallbackRoots` `[sourced]`

Package doc, https://github.com/golang/crypto/blob/master/x509roots/fallback/fallback.go
(pkg.go.dev: https://pkg.go.dev/golang.org/x/crypto/x509roots/fallback):

> Package fallback embeds a set of fallback X.509 trusted roots in the application by
> automatically invoking [x509.SetFallbackRoots]. This allows the application to work
> correctly even if the operating system does not provide a verifier or system roots
> pool.
>
> To use it, import the package like
>
>     import _ "golang.org/x/crypto/x509roots/fallback"
>
> It's recommended that only binaries, and not libraries, import this package.
>
> This package must be kept up to date for security and compatibility reasons. Use
> govulncheck to be notified of when new versions of the package are available.

Its `init()` calls `x509.SetFallbackRoots(newFallbackCertPool())`.

Module facts (https://proxy.golang.org/golang.org/x/crypto/x509roots/fallback/@latest and
its `.mod`): it is a **separate module** from `golang.org/x/crypto` (Subdir
`x509roots/fallback`), latest `v0.0.0-20260921070245-7a4a4d6beae2` (2026-09-21), and its
go.mod declares `go 1.26.0`. (`golang.org/x/crypto` itself is at `v0.57.0`, 2026-09-08.)

`SetFallbackRoots` doc, https://github.com/golang/go/blob/go1.27.1/src/crypto/x509/root.go#L72-L84
(pkg.go.dev: https://pkg.go.dev/crypto/x509#SetFallbackRoots):

> SetFallbackRoots sets the roots to use during certificate verification, if no custom
> roots are specified and a platform verifier or a system certificate pool is not
> available (for instance in a container which does not have a root certificate
> bundle). SetFallbackRoots will panic if roots is nil.
>
> SetFallbackRoots may only be called once, if called multiple times it will panic.
>
> The fallback behavior can be forced on all platforms, even when there is a system
> certificate pool, by setting GODEBUG=x509usefallbackroots=1 (note that on Windows and
> macOS this will disable usage of the platform verification APIs and cause the pure Go
> verifier to be used). Setting x509usefallbackroots=1 without calling SetFallbackRoots
> has no effect.

When fallback applies, from `initSystemRoots` (root.go L43-68):

```go
systemCertsAvail := systemRoots != nil && (systemRoots.len() > 0 || systemRoots.systemPool)

if !useFallbackRoots && systemCertsAvail {
	return
}
```

So fallbacks are used only when the loaded system pool is nil (load error) or **empty**
and not a platform (`systemPool`) pool — or when forced with `x509usefallbackroots=1`.
Interaction with §3 `[sourced — unverified]` (inference from code): on Linux, if
`SSL_CERT_FILE`/`SSL_CERT_DIR` point at paths that do not exist and the defaults are
therefore not read, `loadOnDiskRoots` returns an empty pool with nil error, so fallback
roots would be used; on macOS/Windows with no env vars set, the platform pool counts as
available and fallback is never used unless forced.

## 5. macOS/Windows: `SystemCertPool()` + `AppendCertsFromPEM` as `tls.Config.RootCAs` `[sourced]`

Go 1.18 release notes, https://go.dev/doc/go1.18 (crypto/x509):

> SystemCertPool is now available on Windows. On Windows, macOS, and iOS, when a
> CertPool returned by SystemCertPool has additional certificates added to it,
> Certificate.Verify will do two verifications: one using the platform verifier APIs
> and the system roots, and one using the Go verifier and the additional roots. Chains
> returned by the platform verifier APIs will be prioritized.

Current source confirms it, https://github.com/golang/go/blob/go1.27.1/src/crypto/x509/cert_pool.go#L31-L35:

```go
// systemPool indicates whether this is a special pool derived from the
// system roots. If it includes additional roots, it requires doing two
// verifications, one using the roots provided by the caller, and one using
// the system platform verifier.
systemPool bool
```

and https://github.com/golang/go/blob/go1.27.1/src/crypto/x509/verify.go#L565-L581:

```go
// Use platform verifiers, where available, if Roots is from SystemCertPool.
if runtime.GOOS == "windows" || runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
	// Don't use the system verifier if the system pool was replaced with a non-system pool,
	// i.e. if SetFallbackRoots was called with x509usefallbackroots=1.
	systemPool := systemRootsPool()
	if opts.Roots == nil && (systemPool == nil || systemPool.systemPool) {
		return c.systemVerify(&opts)
	}
	if opts.Roots != nil && opts.Roots.systemPool {
		platformChains, err := c.systemVerify(&opts)
		// If the platform verifier succeeded, or there are no additional
		// roots, return the platform verifier result. Otherwise, continue
		// with the Go verifier.
		if err == nil || opts.Roots.len() == 0 {
			return platformChains, err
		}
	}
}
```

Answer: **yes** — both are consulted: platform verifier first; if it fails and the pool
has appended roots, the Go verifier runs against the appended roots. `[sourced]`

Caveat introduced by Go 1.27 `[sourced — unverified]` (inference combining §3 and this
section): the `systemPool: true` flag is only set by `loadSystemRoots` when no
`SSL_CERT_*` env var is set (or `x509sslcertoverrideplatform=0`). If `SSL_CERT_FILE` or
`SSL_CERT_DIR` is set on macOS/Windows under the 1.27 default, `SystemCertPool()`
returns an on-disk pool with `systemPool == false`, so the dual verification above does
**not** happen — only the pure-Go verifier against disk roots + appended certs. Also,
`tls.Config.RootCAs` being passed through as `VerifyOptions.Roots` is standard
crypto/tls behavior but was not re-read from crypto/tls source here.

## 6. modelcontextprotocol/go-sdk latest release and minimum Go `[sourced]`

- GitHub releases API (https://api.github.com/repos/modelcontextprotocol/go-sdk/releases/latest):
  latest non-prerelease **`v1.8.0`**, published 2026-09-14T08:05:46Z,
  https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0. Previous stable:
  `v1.7.0` (2026-07-28). Prereleases `v1.8.0-pre.1` / `-pre.2` on 2026-09-04.
- Go module proxy (https://proxy.golang.org/github.com/modelcontextprotocol/go-sdk/@latest):
  `"Version":"v1.8.0","Time":"2026-09-04T08:08:52Z"` (tag time differs from GitHub
  release publish time).
- go.mod at v1.8.0 (https://proxy.golang.org/github.com/modelcontextprotocol/go-sdk/@v/v1.8.0.mod,
  identical at https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.8.0/go.mod):
  `module github.com/modelcontextprotocol/go-sdk` / **`go 1.25.0`**. Direct requires:
  `github.com/golang-jwt/jwt/v5 v5.3.1`, `github.com/google/go-cmp v0.7.0`,
  `github.com/google/jsonschema-go v0.4.3`, `github.com/segmentio/encoding v0.5.4`,
  `github.com/yosida95/uritemplate/v3 v3.0.2`, `golang.org/x/oauth2 v0.35.0`,
  `golang.org/x/time v0.15.0`, `golang.org/x/tools v0.42.0`.

As a dependency, its `go 1.25.0` line only sets a minimum toolchain; it does not affect
GODEBUG defaults of the consuming binary (only the work module's go.mod does, §2.3).
