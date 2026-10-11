#!/usr/bin/env bash
# WHAT: runs the tests of one package under the race detector in ONE process
#       and reports the peak resident set of that process, under a guard that
#       ends the run before the machine swaps: the guard reads the resident
#       set (VmRSS) of the test process by its PID every 0.2 s and kills it
#       when it reaches the limit (default 5000 MiB) or when MemAvailable
#       falls under 1024 MiB. Exit code: that of the tests; 3 when the guard
#       fired. Linux only (/proc).
# WHY:  one `go test -race` process of internal/server once grew past 6.5 GB,
#       swapped an 8 GB machine and ended as a timeout after 15 minutes (#201).
#       The cause was memory the tests kept after they ended, not the race
#       detector: the peak is now about 1.7 GB. This is how the next growth is
#       seen, safely: `ulimit -v` and `prlimit --as` are no guard, because the
#       race detector reserves a very large address space and would not start.
# WHEN: after adding many tests or a large fixture to a package, or when
#       `go test -race ./...` starts to swap. Not part of CI: the number
#       depends on the machine and on the Go release, so nothing asserts it.
#       Run it alone: not beside a container stack or another test run.
# HOW:  scripts/race-memory.sh [package [limit MiB [test flags...]]]
#         scripts/race-memory.sh                          ./internal/server, 5000 MiB
#         scripts/race-memory.sh ./internal/server 3000   a lower limit
#         scripts/race-memory.sh ./internal/server 5000 -test.run GetTraceTree
#       MIN_MEM_AVAILABLE_MIB=<n> replaces the 1024 MiB MemAvailable floor.
#       The last line is `peak resident set <n> MiB (<limit> MiB limit)`;
#       the figure is the largest VmHWM seen, so it can miss a spike of the
#       last 0.2 s. `go` and `gcc` (the race detector needs cgo) must be on PATH.
set -euo pipefail

pkg=${1:-./internal/server}
limit_mib=${2:-5000}
floor_mib=${MIN_MEM_AVAILABLE_MIB:-1024}
[[ $# -gt 2 ]] && flags=("${@:3}") || flags=()

if [[ ! -r /proc/meminfo ]]; then
  echo "race-memory.sh needs /proc (Linux)" >&2
  exit 2
fi

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
go test -race -c -o "$dir/race.test" "$pkg"

# The test binary runs in the package directory, as `go test` does.
cd "$pkg"
"$dir/race.test" "${flags[@]}" &
pid=$!

peak_mib=0
fired=
while kill -0 "$pid" 2>/dev/null; do
  rss_kib=$(awk '/^VmRSS:/ {print $2}' "/proc/$pid/status" 2>/dev/null || true)
  hwm_kib=$(awk '/^VmHWM:/ {print $2}' "/proc/$pid/status" 2>/dev/null || true)
  avail_mib=$(($(awk '/^MemAvailable:/ {print $2}' /proc/meminfo) / 1024))
  ((${hwm_kib:-0} / 1024 > peak_mib)) && peak_mib=$((hwm_kib / 1024))
  if ((${rss_kib:-0} / 1024 >= limit_mib)); then
    fired="resident set ${rss_kib} KiB reached the ${limit_mib} MiB limit"
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
if [[ -n "$fired" ]]; then
  echo "memory guard fired: $fired; the test process was killed" >&2
  echo "peak resident set ${peak_mib} MiB (${limit_mib} MiB limit), killed" >&2
  exit 3
fi
echo "peak resident set ${peak_mib} MiB (${limit_mib} MiB limit)"
exit "$status"
