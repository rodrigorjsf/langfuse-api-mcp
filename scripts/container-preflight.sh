# shellcheck shell=bash
#
# WHAT: the preflight guard every container stack of this repository runs
#       before it starts (sourced, not executed): it refuses to start when any
#       other container is running, or when the kernel reports less free
#       memory (MemAvailable in /proc/meminfo) than the stack needs. It never
#       stops or removes a container itself.
# WHY:  the development box is small: WSL had 10 GB of RAM, 2 GB of swap and
#       4 CPUs when it froze, and now has 7.8 GiB, 4 GiB and 12 CPUs
#       (.wslconfig). On 2026-09-27 WSL froze when three full Langfuse stacks
#       (one of them this repository's integration stack, left running) and
#       Ollama loading qwen3:8b ran at once. One stack at a time, and only with
#       room to spare (docs/development/integration-tests.md "Container stacks").
# WHEN: sourced by scripts/langfuse-selfhosted.sh (`up`) and
#       scripts/small-model-eval-local.sh, before anything is started.
# HOW:  . "$ROOT/scripts/container-preflight.sh"
#       container_preflight <stack name> <minimum MemAvailable in MiB>
#       ALLOW_OTHER_CONTAINERS=1  skips the running-container check
#       MIN_MEM_AVAILABLE_MIB=<n> replaces the stack's memory minimum
#                                 (0 skips the memory check)
#       Without /proc/meminfo (macOS) the memory check is skipped with a notice.

container_preflight() {
  local stack=$1 min_mib=${MIN_MEM_AVAILABLE_MIB:-$2}
  local running
  running=$(docker ps --format '{{.Names}}')
  if [[ -n "$running" && "${ALLOW_OTHER_CONTAINERS:-}" != 1 ]]; then
    {
      echo "refusing to start $stack: other containers are running:"
      sed 's/^/  /' <<<"$running"
      echo "Stop them first (this script never stops them for you): run one stack at a time."
      echo "ALLOW_OTHER_CONTAINERS=1 overrides this check."
    } >&2
    exit 1
  fi

  if [[ ! -r /proc/meminfo ]]; then
    echo "no /proc/meminfo: skipping the free-memory check for $stack" >&2
    return 0
  fi
  local available_kib available_mib
  available_kib=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)
  available_mib=$((${available_kib:-0} / 1024))
  echo "MemAvailable ${available_mib} MiB; $stack needs at least ${min_mib} MiB"
  if ((available_mib < min_mib)); then
    {
      echo "refusing to start $stack: only ${available_mib} MiB of memory available, it needs ${min_mib} MiB."
      echo "Free memory first; MIN_MEM_AVAILABLE_MIB=<n> overrides the minimum."
    } >&2
    exit 1
  fi
}
