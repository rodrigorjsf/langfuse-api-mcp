#!/usr/bin/env bash
#
# WHAT: runs the small-model discovery eval (scripts/small-model-eval.py)
#       against a local model instead of the Anthropic API: it starts the
#       Ollama stack of scripts/small-model-eval-ollama.yml, pulls the model if
#       the volume lacks it, runs the eval through Ollama's Anthropic-compatible
#       Messages API, and stops the stack again, also on failure or Ctrl-C.
# WHY:  the eval needs a Messages API; without a paid key, a local 8B model on
#       a consumer GPU gives the same PASS/FAIL evidence for free (#88). The
#       container runs only for the eval, never in the background.
# WHEN: by hand, like the eval itself: at the end of a milestone and after any
#       change to tool descriptions, hints or the operation index.
# HOW:  scripts/small-model-eval-local.sh [extra small-model-eval.py flags]
#       SMALL_MODEL=<ollama model> overrides the model (default qwen3:8b).
#       OLLAMA_KV_CACHE_TYPE=q8_0 halves the KV cache (default f16); only
#                   qwen3:14b needs it to fit 12 GB, and it costs qwen3:8b
#                   an intent (docs/research/raw/
#                   2026-09-27-small-model-eval-local-model-vram.md):
#                   SMALL_MODEL=qwen3:14b OLLAMA_KV_CACHE_TYPE=q8_0 passes
#                   9/11 against 8/11, but fails intent 11, peaks at 11337 MiB
#                   of VRAM and takes 3.4 times as long, so it is not the
#                   default.
#       docker compose -f scripts/small-model-eval-ollama.yml down -v
#                   also deletes the model cache (about 5 GB for qwen3:8b).
#
# Needs docker with the compose plugin and the NVIDIA container toolkit, an
# NVIDIA GPU with about 8 GB of free VRAM for qwen3:8b (Q4_K_M, context
# 16384; measured 7.5 GB, 100% on the GPU of an RTX 3060 12 GB), python3 and
# the Go toolchain. Port 127.0.0.1:11434 must be free.
#
# Safe on a small box (README "Container stacks"): the preflight guard of
# scripts/container-preflight.sh refuses to start while any other container is
# running (ALLOW_OTHER_CONTAINERS=1 overrides) or with less than
# MIN_MEM_MIB MemAvailable (MIN_MEM_AVAILABLE_MIB overrides); the server is
# built before the model loads, so `go build` never runs beside it; the
# container is capped in memory and CPUs (scripts/small-model-eval-ollama.yml)
# and is OOM-killed instead of freezing the host; every model layer goes to
# the GPU, and the run stops before the eval unless `ollama ps` says 100% GPU.
#
# qwen3:8b is a thinking model: it spends output tokens on thinking before a
# tool call, and one turn can take minutes, so the eval runs with
# --max-tokens 4096 --timeout 600 unless you pass other values.
#
# No secret is involved: Ollama ignores the API key, and the eval's literal
# placeholder "ollama" is all that is sent.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT/scripts/small-model-eval-ollama.yml"
MODEL=${SMALL_MODEL:-qwen3:8b}
BASE_URL=http://127.0.0.1:11434
READY_TIMEOUT_SECONDS=${READY_TIMEOUT_SECONDS:-120}
# MemAvailable required before the stack starts: the container's mem_limit
# (4 GiB) plus 2 GiB for the server, the eval, the Go build cache and WSL itself.
MIN_MEM_MIB=6144

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

# shellcheck source=scripts/container-preflight.sh
. "$ROOT/scripts/container-preflight.sh"
container_preflight "the Ollama eval stack" "$MIN_MEM_MIB"

# Build first: a Go build beside a loaded model is the kind of peak to avoid.
WORK_DIR=$(mktemp -d)
echo "building the server"
(cd "$ROOT" && go build -o "$WORK_DIR/langfuse-mcp" ./cmd/langfuse-mcp)

# Down without -v: the containers go, the pulled model stays in the volume.
trap 'compose down; rm -rf "$WORK_DIR"' EXIT

compose up --detach --quiet-pull

waited=0
until curl -fsS -o /dev/null "$BASE_URL/api/version"; do
  if ((waited >= READY_TIMEOUT_SECONDS)); then
    echo "Ollama did not answer in ${READY_TIMEOUT_SECONDS}s" >&2
    compose logs --tail 50 >&2 || true
    exit 1
  fi
  sleep 2
  waited=$((waited + 2))
done
echo "ollama $(curl -fsS "$BASE_URL/api/version"); KV cache ${OLLAMA_KV_CACHE_TYPE:-f16}"

compose exec -T ollama ollama pull "$MODEL"

# Load the model now and refuse to run unless every layer is on the GPU: a
# partial offload would put the rest of the model in the capped host memory.
curl -fsS -o /dev/null "$BASE_URL/api/generate" \
  -d "{\"model\": \"$MODEL\", \"prompt\": \"\", \"keep_alive\": \"30m\"}"
loaded=$(compose exec -T ollama ollama ps)
echo "$loaded"
if ! grep -q "100% GPU" <<<"$loaded"; then
  echo "$MODEL is not fully on the GPU; refusing to run the eval" >&2
  compose logs --tail 50 | grep -E "offloaded|layers" >&2 || true
  exit 1
fi
compose logs | grep -E "offloaded [0-9]+/[0-9]+ layers" | tail -1

ANTHROPIC_BASE_URL=$BASE_URL ANTHROPIC_API_KEY=ollama \
  python3 "$ROOT/scripts/small-model-eval.py" --model "$MODEL" \
  --server "$WORK_DIR/langfuse-mcp" --max-tokens 4096 --timeout 600 "$@"
