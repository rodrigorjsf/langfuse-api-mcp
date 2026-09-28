#!/usr/bin/env bash
#
# WHAT: runs `go test` with the given arguments and, on a Windows runner only,
#       retries it once when the failed run's output carries the Go runtime
#       crash signature of golang/go#81238. Any other failure fails at once.
# WHY:  that runtime bug (not fixed in any Go release yet) corrupts a goroutine
#       stack on AMX Intel runners of the windows-latest pool, so a test binary
#       dies with a runtime fatal error for any commit (#102). The retry is kept
#       narrow, in one place, so removing it with #102 is a single edit.
# WHEN: every Go test step of .github/workflows/ci.yml and release.yml.
# HOW:  scripts/go-test-windows-retry.sh -race -count=1 ./...
#       Needs RUNNER_OS and RUNNER_TEMP (set by GitHub Actions). Remove with #102.

set -uo pipefail

log="${RUNNER_TEMP:?}/go-test.log"
if go test "$@" 2>&1 | tee "$log"; then exit 0; fi
if [ "${RUNNER_OS:-}" = "Windows" ] &&
  grep -qE 'fatal error: (unknown caller pc|fault)|unexpected return pc' "$log"; then
  echo "::warning::Go runtime crash on a Windows runner (golang/go#81238, #102); retrying once"
  exec go test "$@"
fi
exit 1
