#!/usr/bin/env bash
#
# WHAT: starts (up) or removes (down) a throwaway self-hosted Langfuse from the
#       official docker compose setup, with a fresh organization, project and
#       key pair created by Langfuse's headless init, and writes the three
#       LANGFUSE_TEST_* variables the integration tests read into an env file.
# WHY:  .claude/rules/testing.md — the integration suite (build tag
#       `integration`) runs against a real self-hosted Langfuse on every pull
#       request (.github/workflows/integration.yml) and locally on demand.
# WHEN: in CI before `go test -tags integration`; locally whenever you want to
#       run the suite without touching Langfuse Cloud. `down` afterwards.
# HOW:  scripts/langfuse-selfhosted.sh up [env-file]   (default .env.integration.selfhosted)
#       set -a; . ./.env.integration.selfhosted; set +a
#       go test -tags integration -count=1 ./...
#       scripts/langfuse-selfhosted.sh down
#
# Needs docker with the compose plugin, curl and ~3 GiB of free RAM; the stack
# binds port 3000 (Langfuse) and 9090 (MinIO). Cold start is about a minute
# once the images are pulled.
#
# Everything is pinned: the upstream compose file by commit, every image by
# digest. A floating tag silently changes the Langfuse under test (a cached
# `:4` was once 4.16.0 and lacked whole API families; docs/research/langfuse.md).
# Bump them together, on purpose, and record the version in the research notes.

set -euo pipefail

# Upstream langfuse/langfuse docker-compose.yml, 2026-09-25.
COMPOSE_COMMIT=fd5c9ee18e0759b6aea6247351e84d7ee02ed1b1
# Langfuse 4.46.0 and the dependency versions that compose file names.
IMAGE_WEB=docker.langfuse.com/langfuse/langfuse:4.46.0@sha256:755b821ba8f73a20d43d90e68f2b75d2f597dd164c55890f67a05f5a8d0f0e24
IMAGE_WORKER=docker.langfuse.com/langfuse/langfuse-worker:4.46.0@sha256:3568d2d1eb5dd570f4087b37972ddffa3f6ae4f872553e0b0e2eedf4896a29e9
IMAGE_CLICKHOUSE=docker.io/clickhouse/clickhouse-server:25.12@sha256:8a790dd3468db22b1d4e7b18a176f378ff5ff6053b9c48dd4ea1fa71a24c5ba6
IMAGE_MINIO=cgr.dev/chainguard/minio:latest@sha256:bd014394a80898e68c149f2311fdf8d5a2c2f3bb2c33b9327ae6d02b4b065ae1
IMAGE_REDIS=docker.io/redis:7@sha256:c6eabf748fc7a61dbb5a705c78bcf3d6377b1127a97d0ce965c11c44ba46896f
IMAGE_POSTGRES=docker.io/postgres:17@sha256:d74eeac9a635390a49bc21bd49fccd973de707e2a53a76ac49b552b8712ec46f

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
  curl -fsSL -o "$WORK_DIR/docker-compose.yml" \
    "https://raw.githubusercontent.com/langfuse/langfuse/$COMPOSE_COMMIT/docker-compose.yml"
  cat >"$WORK_DIR/pinned.yml" <<EOF
services:
  langfuse-web: { image: "$IMAGE_WEB" }
  langfuse-worker: { image: "$IMAGE_WORKER" }
  clickhouse: { image: "$IMAGE_CLICKHOUSE" }
  minio: { image: "$IMAGE_MINIO" }
  redis: { image: "$IMAGE_REDIS" }
  postgres: { image: "$IMAGE_POSTGRES" }
EOF

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
    printf 'LANGFUSE_TEST_BASE_URL=%s\nLANGFUSE_TEST_PUBLIC_KEY=%s\nLANGFUSE_TEST_SECRET_KEY=%s\n' \
      "$BASE_URL" "$public_key" "$secret_key" >"$env_file"
  )
  echo "wrote LANGFUSE_TEST_* to $env_file"
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
