#!/usr/bin/env bash
#
# WHAT: starts the MCP server over stdio against the local self-hosted
#       Langfuse of the integration stack, in read-only (`ro`) or write (`rw`)
#       mode, and appends the server's stderr (startup log and the audit line
#       of every tool call) to dist/e2e/audit-<mode>.log.
# WHY:  an agent host registers this script as the MCP server command for the
#       end-to-end campaign (docs/development/integration-tests.md). The keys
#       stay in the gitignored .env.integration.selfhosted and never enter the
#       host's MCP registration, and the audit file is the record of what an
#       agent really called, independent of what the agent reports.
# WHEN: by hand, after `scripts/langfuse-selfhosted.sh up` and
#       `go build -o dist/e2e/langfuse-mcp ./cmd/langfuse-mcp`.
# HOW:  claude mcp add --scope local langfuse-ro -- "$PWD/scripts/e2e-mcp-server.sh" ro
#       claude mcp add --scope local langfuse-rw -- "$PWD/scripts/e2e-mcp-server.sh" rw
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
mode=${1:-}
case "$mode" in
  ro) writes=false ;;
  rw) writes=true ;;
  *) echo "usage: $0 ro|rw" >&2; exit 2 ;;
esac

env_file="$ROOT/.env.integration.selfhosted"
server="$ROOT/dist/e2e/langfuse-mcp"
[[ -r "$env_file" ]] || { echo "missing $env_file: run scripts/langfuse-selfhosted.sh up" >&2; exit 1; }
[[ -x "$server" ]] || { echo "missing $server: run go build -o dist/e2e/langfuse-mcp ./cmd/langfuse-mcp" >&2; exit 1; }

set -a
# shellcheck disable=SC1090
. "$env_file"
set +a

export LANGFUSE_BASE_URL=$LANGFUSE_TEST_BASE_URL
export LANGFUSE_PUBLIC_KEY=$LANGFUSE_TEST_PUBLIC_KEY
export LANGFUSE_SECRET_KEY=$LANGFUSE_TEST_SECRET_KEY
export LANGFUSE_MCP_ALLOW_WRITES=$writes

exec "$server" 2>>"$ROOT/dist/e2e/audit-$mode.log"
