#!/usr/bin/env bash
#
# WHAT: builds the one MCPB bundle (Claude Desktop, macOS and Windows) from a
#       GoReleaser snapshot or release in dist/: the universal macOS binary as
#       server/langfuse-mcp, the windows/amd64 binary as server/langfuse-mcp.exe,
#       and packaging/mcpb/manifest.json with its version set to the build's.
#       It checks the manifest with `mcpb validate`, packs it with `mcpb pack`
#       into dist/langfuse-mcp_<version>.mcpb and prints that path.
# WHY:  GoReleaser has no MCPB support; spec #119 wants one bundle whose
#       manifest version equals the release version, validated and packed by
#       the pinned MCPB CLI (packaging/mcpb/package.json + package-lock.json).
# WHEN: after `goreleaser release --snapshot --clean --config packaging/.goreleaser.yaml`
#       (the release workflow's snapshot job), or after a tagged release build.
# HOW:  (cd packaging/mcpb && npm ci --ignore-scripts)   # once: the pinned CLI
#       scripts/pack-mcpb.sh                              # from the repository root
#       Needs jq and Node. DIST=<dir> reads another GoReleaser output directory.

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
dist=${DIST:-$root/dist}
mcpb=$root/packaging/mcpb/node_modules/.bin/mcpb
[[ -x $mcpb ]] || { echo "pack-mcpb: the pinned MCPB CLI is missing; run: (cd packaging/mcpb && npm ci --ignore-scripts)" >&2; exit 1; }

version=$(jq -er .version "$dist/metadata.json")

# One binary per selector, or stop: GoReleaser's own record of what it built.
binary() {
  local path
  path=$(jq -er --arg id "$1" --arg os "$2" --arg arch "$3" '
    [.[] | select(.type == "Binary" and .extra.ID == $id and .goos == $os and .goarch == $arch) | .path]
    | if length == 1 then .[0] else error("want exactly one binary, got \(length)") end' "$dist/artifacts.json")
  printf '%s/%s\n' "$root" "$path"
}
darwin=$(binary langfuse-mcp-macos-universal darwin all)
windows=$(binary langfuse-mcp windows amd64)

stage=$dist/mcpb
rm -rf "$stage"
mkdir -p "$stage/server"
install -m 0755 "$darwin" "$stage/server/langfuse-mcp"
install -m 0755 "$windows" "$stage/server/langfuse-mcp.exe"
install -m 0644 "$root/LICENSE" "$stage/LICENSE"
jq --arg version "$version" '.version = $version' "$root/packaging/mcpb/manifest.json" >"$stage/manifest.json"

out=$dist/langfuse-mcp_${version}.mcpb
"$mcpb" validate "$stage/manifest.json" >&2
"$mcpb" pack "$stage" "$out" >&2
printf '%s\n' "$out"
