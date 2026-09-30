# Installation reference

What each distribution channel ships, how it is built and tested, and the version it reports. How to install is in the [README](../../README.md#install-and-run) · [documentation index](../INDEX.md).

## Versions and channels

Every build reports the version it was built with: `initialize` returns it as the server version, and the startup log line `server started` names it. A build without an injected version reports the module version Go recorded, without the leading `v`, only when it is a release tag (a `go install …@v0.1.0` build reports `0.1.0`); anything else reports `0.0.0-dev`: a pseudo-version, `(devel)` or a `+dirty` version, which is what a `go build` in a checkout gets unless it sits, clean, exactly on a release tag.

| Channel | Status |
|---|---|
| [Release archive](#release-archive) (Linux, macOS, Windows × amd64, arm64) | published with every release, signed; built and smoke-tested on every change to packaging or the executable |
| [`go install`](#go-install) | `@v0.1.0` (or `@latest`) reports its release version; a commit (`@main`) reports `0.0.0-dev` |
| [Docker](#docker) (linux/amd64, linux/arm64; stdio only) | `ghcr.io/rodrigorjsf/langfuse-api-mcp:<version>`, public, signed by digest; built and smoke-tested on every change to packaging or the executable |
| [Claude Desktop (MCPB)](#claude-desktop-mcpb) (macOS, Windows) | attached to every release, signed; built and smoke-tested on every change to packaging or the executable; its install in Claude Desktop itself is not recorded yet |
| [npx](#npx) (Linux, macOS, Windows × x64, arm64) | built on every change to packaging or the executable, and smoke-tested on Linux x64, macOS arm64 and Windows x64 (the other three platform packages are built, not CI-tested); published to npmjs.org with npm provenance at every release |

The server is listed in the [MCP Registry](https://registry.modelcontextprotocol.io) as `io.github.rodrigorjsf/langfuse-api-mcp`, pointing at the npm package, the image and the `.mcpb`.

## Release archive

Every [GitHub release](https://github.com/rodrigorjsf/langfuse-api-mcp/releases) carries these archives; the release pipeline also builds them for every change to packaging or the executable and proves each one on a clean runner.

One archive per target: `langfuse-mcp_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows), with `os` one of `linux`, `darwin`, `windows` and `arch` one of `amd64`, `arm64`. Each holds the `langfuse-mcp` binary (`langfuse-mcp.exe` on Windows), `LICENSE` and this README. Next to them: `checksums.txt` (SHA-256 of every archive and of its SBOM) and one SPDX JSON SBOM per archive (`<archive>.sbom.json`). `windows/arm64` is **built, not CI-tested**: no runner proves it; on Windows on Arm you can also run the `windows/amd64` build under emulation.

Download with `gh` or `curl`, check the archive against `checksums.txt`, then extract it (Linux on amd64 shown; `<version>` is the release without the leading `v`, for example `0.1.0`):

```bash
version=<version>
archive="langfuse-mcp_${version}_linux_amd64.tar.gz"
gh release download "v$version" -R rodrigorjsf/langfuse-api-mcp -p "$archive" -p checksums.txt
# or: curl -fsSLO "https://github.com/rodrigorjsf/langfuse-api-mcp/releases/download/v$version/$archive"
#     curl -fsSLO "https://github.com/rodrigorjsf/langfuse-api-mcp/releases/download/v$version/checksums.txt"
awk -v f="$archive" '$2 == f' checksums.txt | sha256sum -c -   # macOS: … | shasum -a 256 -c -
tar -xzf "$archive"
```

On Windows (PowerShell), compare the hash and extract the zip:

```powershell
$archive = "langfuse-mcp_<version>_windows_amd64.zip"
(Get-FileHash $archive -Algorithm SHA256).Hash.ToLower()   # must equal the line for $archive in checksums.txt
Expand-Archive $archive -DestinationPath langfuse-mcp
```

Then point your MCP client at the extracted binary (see [Client configuration](configuration.md#client-configuration)).

**Downloaded in a browser?** The binaries are not notarized by Apple nor Authenticode-signed, so macOS Gatekeeper blocks a binary downloaded in a browser ("cannot be opened because the developer cannot be verified") and Windows SmartScreen may warn about it. Download with `curl` or `gh` as above (neither marks the file as downloaded from the internet), or use another channel. If you already downloaded it in a browser, check the checksum first, then on macOS remove the mark with `xattr -d com.apple.quarantine langfuse-mcp`, or on Windows choose "More info" → "Run anyway" (or `Unblock-File langfuse-mcp.exe`).

## go install

Install a release (`@v0.1.0`, or `@latest`) or a commit (`@main` or a commit hash). With Go 1.21 or later (the module's `toolchain` directive fetches the Go 1.27 toolchain it needs):

```bash
go install github.com/rodrigorjsf/langfuse-api-mcp/cmd/langfuse-mcp@<version>
```

The binary lands in `$(go env GOPATH)/bin`. A `go install …@<version>` build of a release tag reports that version without the leading `v` (`@v0.1.0` reports `0.1.0`), taken from the module version Go records in the binary. `@latest` resolves to the latest release tag and reports it the same way; a build at a commit that is no release tag (`@main`, a commit hash) reports `0.0.0-dev`. `go install …@v0.1.0` reporting `0.1.0` in `initialize` is recorded ([record](../research/raw/2026-09-30-v0.1.0-release-proofs.md)).

## Docker

Every release pushes the image to GHCR, public, signed and attested by digest; the release pipeline also builds it for every change to packaging or the executable and proves the amd64 image with `docker run -i` on a clean Linux runner.

The image `ghcr.io/rodrigorjsf/langfuse-api-mcp:<version>` is minimal: based on `gcr.io/distroless/static-debian13:nonroot` pinned by digest, the binary is the entrypoint, it runs as the non-root user `nonroot`, and it holds no shell. It is built for `linux/amd64` and `linux/arm64` and published as one multi-arch image, so an arm64 machine runs it natively (a snapshot build tags one local image per architecture, `<version>-amd64` and `<version>-arm64`, since a multi-arch image exists only once pushed). One SPDX JSON SBOM describes it (`langfuse-mcp_<version>_image.sbom.json`, scanned from the amd64 image; the arm64 image holds the same base and the same binary built for arm64).

Pass the keys and the base URL with `-e`, and keep `-i` (the server speaks MCP over stdin and stdout):

```bash
docker run -i --rm \
  -e LANGFUSE_PUBLIC_KEY -e LANGFUSE_SECRET_KEY -e LANGFUSE_BASE_URL \
  ghcr.io/rodrigorjsf/langfuse-api-mcp:<version>
```

`-e NAME` without a value passes the variable from the environment that runs `docker`, so the keys never appear in the command line; set them there, or in your MCP client's `"env"` (a key exported in the shell that starts the client reaches the agent's own commands too: [Keep the keys out of the agent's environment](../../README.md#keep-the-keys-out-of-the-agents-environment)). Behind a corporate CA, mount the CA file read-only and point `LANGFUSE_CA_CERT` at it; the file must be readable by any user (the image does not run as you):

```bash
docker run -i --rm \
  -e LANGFUSE_PUBLIC_KEY -e LANGFUSE_SECRET_KEY -e LANGFUSE_BASE_URL \
  -e LANGFUSE_CA_CERT=/certs/corp.pem -v /path/to/corp.pem:/certs/corp.pem:ro \
  ghcr.io/rodrigorjsf/langfuse-api-mcp:<version>
```

The image trusts its base image's CA bundle plus the file you mount, nothing else (the base image sets `SSL_CERT_FILE` to its bundle, so the startup `CA sources loaded` line lists it as an ambient source next to your file): without the file, a Langfuse host signed by a private CA is refused with `tls_untrusted_certificate`. In an MCP client, the `command` is `docker` and `args` is the list above. To cap the server's memory, give the container a limit and pass the matching Go soft limit, for example `--memory 256m -e GOMEMLIMIT=200MiB`: the Go runtime reads `GOMEMLIMIT` and collects garbage harder as it nears it, instead of being killed at the container limit.

**The image serves stdio only.** The loopback HTTP transport (`LANGFUSE_MCP_TRANSPORT=http`, **Planned**) binds `127.0.0.1` only, which inside a container is the container's own loopback: it cannot be reached from outside, and it is not meant to be exposed.

## npx

Every release publishes the npm packages to npmjs.org with npm provenance; the release pipeline also packs them for every change to packaging or the executable and proves them with `npx` on clean Linux, macOS and Windows runners.

Configure the server in your MCP client as `npx -y langfuse-api-mcp` (pin a version with `langfuse-api-mcp@<version>`); it needs Node.js with npm, nothing else:

```json
{
  "mcpServers": {
    "langfuse": {
      "command": "npx",
      "args": ["-y", "langfuse-api-mcp"],
      "env": {
        "LANGFUSE_PUBLIC_KEY": "pk-lf-...",
        "LANGFUSE_SECRET_KEY": "sk-lf-...",
        "LANGFUSE_BASE_URL": "https://cloud.langfuse.com"
      }
    }
  }
}
```

The package carries the Go binary; nothing is downloaded at install time and no install script runs, so it installs wherever npm can reach its registry, also with `--ignore-scripts`. `langfuse-api-mcp` holds only a small Node launcher, `langfuse-mcp`, and lists one optional dependency per platform: `langfuse-api-mcp-<platform>-<arch>` for `linux`, `darwin` and `win32` × `x64` and `arm64`, each restricted to its OS and CPU and holding only that platform's binary, so npm installs the one for your machine and skips the others. All of them carry the same version as the release. The launcher starts the binary with your arguments and environment unchanged, never through a shell, with stdin, stdout and stderr passed straight through; it forwards `SIGINT`, `SIGTERM` and `SIGHUP` to the binary and exits with the binary's exit code, or of the same signal (proven on Linux and macOS; Windows has no such signals, and there a stop request ends the binary too). On Windows, `npx` itself is a `.cmd` script that npm runs through `cmd.exe`, so arguments you put after the package name pass through npm's own command-line handling before they reach the launcher; the server takes no arguments, and your keys and settings travel in the environment, which no shell rewrites. On any other platform it exits with an error naming your platform and the supported ones: use a [release archive](#release-archive) or [Docker](#docker) there. An install with `--omit=optional` leaves the binary out, and the launcher says so.

The npm registry and Node.js are dependencies of this channel only: the other channels do not touch them.

## Claude Desktop (MCPB)

Every GitHub release carries the bundle, signed; the release pipeline also packs it for every change to packaging or the executable and proves the binary inside it on clean macOS and Windows runners. Its install in Claude Desktop itself is not recorded yet.

One file, `langfuse-mcp_<version>.mcpb`, for Claude Desktop on macOS (Apple silicon and Intel: it holds one universal binary) and Windows (amd64 only; Windows on Arm is not tested). There is no Linux bundle: no Linux host installs `.mcpb` files. Double-click it (or drag it onto Claude Desktop); the install dialog asks for:

| Field | Required | Becomes |
|---|---|---|
| Langfuse public key | yes; stored in the OS keychain, masked | `LANGFUSE_PUBLIC_KEY` |
| Langfuse secret key | yes; stored in the OS keychain, masked | `LANGFUSE_SECRET_KEY` |
| Langfuse base URL | yes | `LANGFUSE_BASE_URL` |
| CA certificate | no; a file picker | `LANGFUSE_CA_CERT` |

Those four variables are all the bundle sets; the server validates them at startup exactly as for any other channel (for example, a base URL that is neither `https` nor `http` on a loopback host stops the server, naming the variable, never its value). The dialog offers **no write-mode option** on purpose: to enable writes, set `LANGFUSE_MCP_ALLOW_WRITES=true` in the [config file](configuration.md#config-file-non-secret-settings), a deliberate step outside the install dialog. Any other setting (proxy, request limits) also goes in the config file. The bundle and its binaries are not signed yet (`mcpb sign`, notarization, Authenticode); if macOS or Windows blocks the binary after a browser download, see the [browser-download note](#release-archive) above. The pipeline validates its manifest with the pinned MCPB CLI (`mcpb validate`) before packing it.

## User skill

The CLI clones the repository (with the git credentials already on your machine, if it needs any) and installs from `main`, while your server may be an older build; the skill therefore names tool names, error codes and v4 operation IDs, plus only the v4 parameter names and filter rules its workflows turn on (such as `sessionId`, `fromStartTime` or the field groups of experiment items, which `describe_operation` does not list), and tells the agent to call `describe_operation` for every other parameter, bound and format. The install from a checkout, and from `main` after the skill was merged, is recorded in [docs/research/raw/2026-09-28-npx-skills-add.md](../research/raw/2026-09-28-npx-skills-add.md). In a recorded Claude Code run against a local Langfuse, the skill loads on its own for trace, cost and experiment requests, and the workflows complete, including the label promotion after the server's confirmation was accepted ([record](../research/raw/2026-09-28-claude-code-skill-run.md)). Its description says to load it before any call to this server's tools; with that wording it also loads for prompt requests ([record](../research/raw/2026-09-28-claude-code-skill-trigger.md)).

**Claude Desktop** installs skills only by upload. Each [GitHub release](https://github.com/rodrigorjsf/langfuse-api-mcp/releases) carries `langfuse-api-mcp-skill_<version>.zip`, signed, whose root is the `langfuse-api-mcp/` folder; upload it in Claude Desktop under Customize > Skills. The release pipeline also builds and checks it on every snapshot run (see [Release pipeline](../development/release-pipeline.md)); from a checkout, build it with `python3 scripts/pack-skill.py <version> dist/skill`.
