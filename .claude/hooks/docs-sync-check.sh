#!/usr/bin/env bash
# WHAT: Stop hook — blocks the agent from finishing a turn when code changed but no doc did.
# WHY:  .claude/rules/docs-sync.md is advisory; this makes "no done without docs" enforced.
# HOW:  union of unpushed commits (@{u}...HEAD, or HEAD~1..HEAD without upstream), working tree and untracked; code = *.go, go.mod, go.sum, Dockerfile, .goreleaser*;
#       docs = README.md, CONTEXT.md, ROADMAP.md, CLAUDE.md, docs/**, .claude/rules/**, skills/**.
#       Blocks once per stop (respects stop_hook_active to avoid loops).
set -euo pipefail
input=$(cat)
case "$input" in *'"stop_hook_active":true'*|*'"stop_hook_active": true'*) exit 0;; esac
cd "${CLAUDE_PROJECT_DIR:-.}"
if git rev-parse -q --verify '@{u}' >/dev/null 2>&1; then range='@{u}...HEAD'; else range='HEAD~1..HEAD'; fi
changed=$( { git diff --name-only "$range" 2>/dev/null; git diff --name-only HEAD 2>/dev/null; git ls-files --others --exclude-standard; } | sort -u )
[ -z "$changed" ] && exit 0
code=$(printf '%s\n' "$changed" | grep -E '(\.go$|^go\.(mod|sum)$|^Dockerfile|^\.goreleaser)' || true)
docs=$(printf '%s\n' "$changed" | grep -E '^(README\.md|CONTEXT\.md|ROADMAP\.md|CLAUDE\.md|docs/|\.claude/rules/|skills/)' || true)
if [ -n "$code" ] && [ -z "$docs" ]; then
  printf '{"decision":"block","reason":"Code changed but no doc did (.claude/rules/docs-sync.md). Update every affected doc (README config/tool tables, CONTEXT.md, ADRs, ROADMAP.md) or state explicitly why none is affected before finishing."}\n'
fi
exit 0
