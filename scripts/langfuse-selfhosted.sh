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
# Everything is pinned: the upstream compose file by commit, every image by
# digest. A floating tag silently changes the Langfuse under test (a cached
# `:4` was once 4.16.0 and lacked whole API families; docs/research/langfuse.md).
# Bump them together, on purpose, update pinnedDeployments and the catalog
# fixtures if the profile changes, and record the version in the research notes.

set -euo pipefail

DEPLOYMENT=${LANGFUSE_DEPLOYMENT:-4.46.0-events_only}

# Shared by every deployment.
IMAGE_REDIS=docker.io/redis:7@sha256:c6eabf748fc7a61dbb5a705c78bcf3d6377b1127a97d0ce965c11c44ba46896f
# The v3 composes leave the PostgreSQL version floating, and `latest` (18)
# rejects their data mount: pin 17 everywhere (docs/research/langfuse.md §1.8).
IMAGE_POSTGRES=docker.io/postgres:17@sha256:d74eeac9a635390a49bc21bd49fccd973de707e2a53a76ac49b552b8712ec46f
IMAGE_CLICKHOUSE_25=docker.io/clickhouse/clickhouse-server:25.12@sha256:8a790dd3468db22b1d4e7b18a176f378ff5ff6053b9c48dd4ea1fa71a24c5ba6
IMAGE_MINIO_CHAINGUARD=cgr.dev/chainguard/minio:latest@sha256:bd014394a80898e68c149f2311fdf8d5a2c2f3bb2c33b9327ae6d02b4b065ae1
# Extra environment of langfuse-web and langfuse-worker, as YAML flow mapping entries.
MIGRATION_ENV=""

case "$DEPLOYMENT" in
  4.46.0-events_only | 4.46.0-dual)
    # Upstream langfuse/langfuse docker-compose.yml, 2026-09-25.
    COMPOSE_COMMIT=fd5c9ee18e0759b6aea6247351e84d7ee02ed1b1
    IMAGE_WEB=docker.langfuse.com/langfuse/langfuse:4.46.0@sha256:755b821ba8f73a20d43d90e68f2b75d2f597dd164c55890f67a05f5a8d0f0e24
    IMAGE_WORKER=docker.langfuse.com/langfuse/langfuse-worker:4.46.0@sha256:3568d2d1eb5dd570f4087b37972ddffa3f6ae4f872553e0b0e2eedf4896a29e9
    IMAGE_CLICKHOUSE=$IMAGE_CLICKHOUSE_25
    IMAGE_MINIO=$IMAGE_MINIO_CHAINGUARD
    if [[ "$DEPLOYMENT" == 4.46.0-dual ]]; then
      # docs/research/langfuse-api-versions.md §1: every family answers.
      MIGRATION_ENV='LANGFUSE_MIGRATION_V4_WRITE_MODE: dual, LANGFUSE_MIGRATION_V4_NATIVE_OTEL_BEHAVIOUR: dual_write'
    fi
    ;;
  3.225.11)
    # The compose file of tag v3.225.11 (2026-09-24), the latest 3.x release.
    COMPOSE_COMMIT=ef21c7cbb3e8f4e5ee90afdaa88678e8a51ce622
    IMAGE_WEB=docker.io/langfuse/langfuse:3.225.11@sha256:a343f64e035eb01aeea358703a0428945d909d01e19452509a5a830862dda878
    IMAGE_WORKER=docker.io/langfuse/langfuse-worker:3.225.11@sha256:8a28c946bb5401eef488153fa294db5a79bd99dd5c90db8e4d39559374c9ebd3
    IMAGE_CLICKHOUSE=$IMAGE_CLICKHOUSE_25
    IMAGE_MINIO=$IMAGE_MINIO_CHAINGUARD
    ;;
  3.80.0)
    # The compose file of tag v3.80.0 (2025-07-09). Its ClickHouse is
    # untagged, and 26.x stores every timestamp as 9999-12-31: pin 24.3
    # (docs/research/langfuse.md §1.8).
    COMPOSE_COMMIT=bd0b204d189504ba2422dd1858dc74c2fbfa4509
    IMAGE_WEB=docker.io/langfuse/langfuse:3.80.0@sha256:74f7c6877bce160bbecd4921297ca750f1ef0cd7fc6331e37fd942eb61360873
    IMAGE_WORKER=docker.io/langfuse/langfuse-worker:3.80.0@sha256:de25fb4cd7d61bdd60574d9002f2d995982fcc893b80277ac54f78b48c4c88ef
    IMAGE_CLICKHOUSE=docker.io/clickhouse/clickhouse-server:24.3@sha256:85b97f63dcfff47790d26bb5d5801637aaddb2b93e5e9aee27a686c2fb2b9916
    IMAGE_MINIO=docker.io/minio/minio:RELEASE.2025-09-07T16-13-09Z@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e
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
  curl -fsSL -o "$WORK_DIR/docker-compose.yml" \
    "https://raw.githubusercontent.com/langfuse/langfuse/$COMPOSE_COMMIT/docker-compose.yml"
  cat >"$WORK_DIR/pinned.yml" <<EOF
services:
  langfuse-web: { image: "$IMAGE_WEB", environment: { $MIGRATION_ENV } }
  langfuse-worker: { image: "$IMAGE_WORKER", environment: { $MIGRATION_ENV } }
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
