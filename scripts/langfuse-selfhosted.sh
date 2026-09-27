#!/usr/bin/env bash
#
# WHAT: starts (up) or removes (down) a throwaway self-hosted Langfuse from the
#       official docker compose setup, with a fresh organization, project and
#       key pair created by Langfuse's headless init, and writes the
#       LANGFUSE_TEST_* variables the integration tests read into an env file:
#       the base URL, the key pair and the name of the pinned deployment.
# WHY:  .claude/rules/testing.md — the integration suite (build tag
#       `integration`) runs against a real self-hosted Langfuse on every pull
#       request (.github/workflows/integration.yml) and locally on demand; the
#       weekly run starts each pinned deployment in turn (ADR-0012).
# WHEN: in CI before `go test -tags integration`; locally whenever you want to
#       run the suite without touching Langfuse Cloud. `down` afterwards.
# HOW:  [LANGFUSE_DEPLOYMENT=<name>] scripts/langfuse-selfhosted.sh up [env-file]
#                                                  (default .env.integration.selfhosted)
#       set -a; . ./.env.integration.selfhosted; set +a
#       go test -tags integration -count=1 ./internal/server/
#       scripts/langfuse-selfhosted.sh down
#
# LANGFUSE_DEPLOYMENT picks the pinned deployment; each is one deployment
# profile of ADR-0012 (version + operation families), and the integration
# suite's pinnedDeployments table (internal/server) holds the profile each
# must be detected as:
#   4.46.0-events_only  (default) v4 read + experiments; a fresh v4 install
#   4.46.0-dual         every family: v4 in LANGFUSE_MIGRATION_V4_WRITE_MODE=dual
#   3.225.11            the latest 3.x: legacy only (its experiments routes want a v4 write mode; see #84)
#   3.80.0              legacy only; the v4 read routes do not exist
# One deployment at a time: they all bind the same ports.
#
# Needs docker with the compose plugin, curl and ~3 GiB of free RAM; the stack
# binds port 3000 (Langfuse) and 9090 (MinIO). Cold start is about a minute
# once the images are pulled.
#
# The whole stack lives in the repository (internal/server/testdata/langfuse-selfhosted/),
# nothing is downloaded at run time: compose-<version>.yml is the upstream
# langfuse/langfuse docker-compose.yml, byte for byte, at the commit named
# below, and pinned-<deployment>.yml pins every service image by digest. `up`
# refuses to start if any image of the rendered stack is not pinned by digest.
# A floating tag silently changes the Langfuse under test (a cached `:4` was
# once 4.16.0 and lacked whole API families; docs/research/langfuse.md).
# Bump them together, on purpose: re-download the compose file from the new
# commit (curl -fsSL https://raw.githubusercontent.com/langfuse/langfuse/<commit>/docker-compose.yml),
# update the override and COMPOSE_COMMIT, update pinnedDeployments and the
# catalog fixtures if the profile changes, and record the version in the
# research notes.

set -euo pipefail

DEPLOYMENT=${LANGFUSE_DEPLOYMENT:-4.46.0-events_only}

# The committed stack. Every override pins postgres to 17: the v3 composes leave
# its version floating, and `latest` (18) rejects their data mount
# (docs/research/langfuse.md §1.8). The 4.46.0-dual override also sets the v4
# migration modes (docs/research/langfuse-api-versions.md §1: every family answers).
STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/internal/server/testdata/langfuse-selfhosted"

case "$DEPLOYMENT" in
  4.46.0-events_only | 4.46.0-dual)
    # Upstream langfuse/langfuse docker-compose.yml, 2026-09-25.
    COMPOSE_COMMIT=fd5c9ee18e0759b6aea6247351e84d7ee02ed1b1
    COMPOSE_VERSION=4.46.0
    ;;
  3.225.11)
    # The compose file of tag v3.225.11 (2026-09-24), the latest 3.x release.
    COMPOSE_COMMIT=ef21c7cbb3e8f4e5ee90afdaa88678e8a51ce622
    COMPOSE_VERSION=3.225.11
    ;;
  3.80.0)
    # The compose file of tag v3.80.0 (2025-07-09). Its ClickHouse is
    # untagged, and 26.x stores every timestamp as 9999-12-31: the override
    # pins 24.3 (docs/research/langfuse.md §1.8).
    COMPOSE_COMMIT=bd0b204d189504ba2422dd1858dc74c2fbfa4509
    COMPOSE_VERSION=3.80.0
    ;;
  *)
    echo "unknown LANGFUSE_DEPLOYMENT '$DEPLOYMENT': one of 4.46.0-events_only, 4.46.0-dual, 3.225.11, 3.80.0" >&2
    exit 2
    ;;
esac

PROJECT=langfuse-mcp-it
BASE_URL=http://localhost:3000
READY_TIMEOUT_SECONDS=${READY_TIMEOUT_SECONDS:-300}
# A user-owned directory, not a predictable path in a shared /tmp: `down` removes it.
WORK_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/$PROJECT"

compose() {
  docker compose --project-name "$PROJECT" \
    -f "$WORK_DIR/docker-compose.yml" -f "$WORK_DIR/pinned.yml" "$@"
}

random_hex() { od -An -N"$1" -tx1 /dev/urandom | tr -d ' \n'; }

up() {
  local env_file="${1:-.env.integration.selfhosted}"
  mkdir -p "$WORK_DIR"
  # A copy, so `down` and `logs` stop the stack that was started whatever
  # LANGFUSE_DEPLOYMENT says then. $COMPOSE_COMMIT names the upstream source.
  cp "$STACK_DIR/compose-$COMPOSE_VERSION.yml" "$WORK_DIR/docker-compose.yml"
  cp "$STACK_DIR/pinned-$DEPLOYMENT.yml" "$WORK_DIR/pinned.yml"
  # Fail closed: a compose that does not render, or any image without a digest, stops here.
  local images image unpinned=0
  images=$(compose config --images)
  [[ -n "$images" ]] || { echo "the stack for $DEPLOYMENT renders no images" >&2; exit 1; }
  while read -r image; do
    if [[ "$image" != *@sha256:* ]]; then
      echo "image not pinned by digest: $image" >&2
      unpinned=1
    fi
  done <<<"$images"
  ((unpinned == 0)) || exit 1
  echo "stack $DEPLOYMENT: upstream compose $COMPOSE_COMMIT, every image pinned by digest"

  # A fresh project and key pair on every start: nothing is reused between runs.
  local public_key secret_key
  public_key="pk-lf-$(random_hex 16)"
  secret_key="sk-lf-$(random_hex 16)"
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "::add-mask::$secret_key"
  fi
  LANGFUSE_INIT_ORG_ID=it-org LANGFUSE_INIT_ORG_NAME=integration-tests \
  LANGFUSE_INIT_PROJECT_ID=it-project LANGFUSE_INIT_PROJECT_NAME=integration-tests \
  LANGFUSE_INIT_PROJECT_PUBLIC_KEY="$public_key" LANGFUSE_INIT_PROJECT_SECRET_KEY="$secret_key" \
  NEXTAUTH_SECRET="$(random_hex 32)" SALT="$(random_hex 32)" ENCRYPTION_KEY="$(random_hex 32)" \
  TELEMETRY_ENABLED=false \
    compose up --detach --quiet-pull

  echo "waiting for $BASE_URL/api/public/ready (up to ${READY_TIMEOUT_SECONDS}s)…"
  local waited=0
  until curl -fsS -o /dev/null "$BASE_URL/api/public/ready"; do
    if ((waited >= READY_TIMEOUT_SECONDS)); then
      echo "Langfuse did not become ready in ${READY_TIMEOUT_SECONDS}s" >&2
      compose logs --tail 100 langfuse-web >&2 || true
      exit 1
    fi
    sleep 5
    waited=$((waited + 5))
  done
  echo "ready after ~${waited}s: $(curl -fsS "$BASE_URL/api/public/health")"

  (
    umask 077
    printf 'LANGFUSE_TEST_BASE_URL=%s\nLANGFUSE_TEST_PUBLIC_KEY=%s\nLANGFUSE_TEST_SECRET_KEY=%s\nLANGFUSE_TEST_DEPLOYMENT=%s\n' \
      "$BASE_URL" "$public_key" "$secret_key" "$DEPLOYMENT" >"$env_file"
  )
  echo "wrote LANGFUSE_TEST_* for deployment $DEPLOYMENT to $env_file"
}

down() {
  if [[ -f "$WORK_DIR/docker-compose.yml" ]]; then
    compose down --volumes --remove-orphans
  fi
  rm -rf "$WORK_DIR"
}

case "${1:-}" in
  up) up "${2:-}" ;;
  down) down ;;
  logs)
    [[ -f "$WORK_DIR/docker-compose.yml" ]] || { echo "no stack started: run $0 up first" >&2; exit 1; }
    compose logs --tail "${2:-200}"
    ;;
  *)
    echo "usage: $0 up [env-file] | down | logs [lines]" >&2
    exit 2
    ;;
esac
