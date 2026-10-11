#!/usr/bin/env bash
# WHAT: runs the tests of one package under the race detector in ONE process
#       and reports the peak resident set of that process, under a guard that
#       ends the run before the machine swaps: the guard reads the resident
#       set (VmRSS) of the test process by its PID every 0.2 s and kills it
#       when it reaches the limit (default 5000 MiB) or when MemAvailable
#       falls under 1024 MiB. Interrupting the script (Ctrl-C, TERM, HUP)
#       kills the test process too. Linux only (/proc).
# WHY:  one `go test -race` process of internal/server once passed 5000 MiB
#       (reported past 6.5 GB), swapped an 8 GB machine and ended as a timeout
#       after 15 minutes (#201). The cause was memory the tests kept after
#       they ended, not the race detector: the peak is now about 1.7 GiB.
#       This is how the next growth is seen, safely: `ulimit -v` and
#       `prlimit --as` are no guard, because the race detector reserves a very
#       large address space and would not start.
# WHEN: after adding many tests or a large fixture to a package, or when
#       `go test -race ./...` starts to swap. A local check, not part of CI:
#       the number depends on the machine and on the Go release, so nothing
#       asserts it. Run it alone: not beside a container stack or another
#       test run.
# HOW:  scripts/race-memory.sh [package [limit MiB [test flags...]]]
#         scripts/race-memory.sh                          ./internal/server, 5000 MiB
#         scripts/race-memory.sh ./internal/server 3000   a lower limit
#         scripts/race-memory.sh ./internal/server 5000 -test.run GetTraceTree
#       MIN_MEM_AVAILABLE_MIB=<n> replaces the 1024 MiB MemAvailable floor.
#       Prints `peak resident set <n> MiB (<limit> MiB limit)` and exits with
#       the tests' exit code. When the guard fires it kills the test process,
#       prints `... killed` and the reason on stderr, and exits 3. The figure
#       is the largest VmHWM seen, sampled every 0.2 s, so it can miss a spike
#       of the last 0.2 s; `/usr/bin/time -v` on the same binary is exact.
#       `go` and `gcc` (the race detector needs cgo) must be on PATH.
set -euo pipefail

pkg=${1:-./internal/server}
limit_mib=${2:-5000}
floor_mib=${MIN_MEM_AVAILABLE_MIB:-1024}
[[ $# -gt 2 ]] && flags=("${@:3}") || flags=()

if [[ ! -r /proc/meminfo ]]; then
  echo "race-memory.sh needs /proc (Linux)" >&2
  exit 2
fi
if [[ ! $limit_mib =~ ^[0-9]+$ || ! $floor_mib =~ ^[0-9]+$ ]]; then
  echo "usage: scripts/race-memory.sh [package [limit MiB [test flags...]]]; the limit and MIN_MEM_AVAILABLE_MIB are whole numbers" >&2
  exit 2
fi

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
dir=$(mktemp -d)
pid=
cleanup() {
  [[ -n "$pid" ]] && kill -KILL "$pid" 2>/dev/null || true
  rm -rf "$dir"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP
go test -race -c -o "$dir/race.test" "$pkg"

# The test binary runs in the package directory, as `go test` does.
cd "$(go list -f '{{.Dir}}' "$pkg")"
"$dir/race.test" "${flags[@]}" &
pid=$!

peak_mib=0
fired=
while kill -0 "$pid" 2>/dev/null; do
  rss_kib=$(awk '/^VmRSS:/ {print $2}' "/proc/$pid/status" 2>/dev/null || true)
  hwm_kib=$(awk '/^VmHWM:/ {print $2}' "/proc/$pid/status" 2>/dev/null || true)
  avail_mib=$(($(awk '/^MemAvailable:/ {print $2}' /proc/meminfo) / 1024))
  ((${hwm_kib:-0} / 1024 > peak_mib)) && peak_mib=$((hwm_kib / 1024))
  rss_mib=$((${rss_kib:-0} / 1024))
  if ((rss_mib >= limit_mib)); then
    fired="the resident set reached ${rss_mib} MiB, the ${limit_mib} MiB limit"
  elif ((avail_mib < floor_mib)); then
    fired="MemAvailable ${avail_mib} MiB is under ${floor_mib} MiB"
  fi
  if [[ -n "$fired" ]]; then
    kill -KILL "$pid" 2>/dev/null || true
    break
  fi
  sleep 0.2
done

status=0
wait "$pid" 2>/dev/null || status=$?
pid=
if [[ -n "$fired" ]]; then
  echo "memory guard fired: $fired; the test process was killed" >&2
  echo "peak resident set ${peak_mib} MiB (${limit_mib} MiB limit), killed" >&2
  exit 3
fi
if ((peak_mib == 0)); then
  echo "peak resident set not sampled: the run ended within 0.2 s (${limit_mib} MiB limit)"
else
  echo "peak resident set ${peak_mib} MiB (${limit_mib} MiB limit)"
fi
exit "$status"
