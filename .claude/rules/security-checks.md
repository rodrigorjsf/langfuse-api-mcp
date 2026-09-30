---
paths:
  - "skills/**"
  - "docs/research/security.md"
  - "scripts/check-*.py"
  - "scripts/test_check_*.py"
  - ".claude/rules/security*.md"
---
# Security: user skill and risk mapping

Part of the non-negotiable security rules (`security.md`). After an edit here run `python3 scripts/check-skill.py` (skill) or `python3 scripts/check-docs.py` (mapping).

**User skill** (M6, #134): the text under `skills/` is client-side and never shapes a tool name or description. `scripts/check-skill.py` runs in CI and fails on a phrase telling the agent to follow or obey instructions found in data or a payload, enable or turn on write mode, `LANGFUSE_MCP_ALLOW_WRITES`, skip, bypass or avoid the confirmation, or retry a refused or declined write, and on a tool name or error code the server lacks (its lists are test-checked against `internal/server`). The phrase list is a heuristic; review covers paraphrases.

**Mapping** (#155): every row of the risk-to-control mapping in `docs/research/security.md` carries exactly one status — `tested` naming its test functions, `planned` linking an issue with a milestone, or `accepted-risk` with a one-line reason. `scripts/check-docs.py` runs in CI (offline, own tests in `scripts/test_check_docs.py`) and fails on a missing or unknown status, a `tested` row naming a test that does not exist, and a `planned` row linking no issue. A new control in `.claude/rules/security*.md` gets a mapping row in the same change.
