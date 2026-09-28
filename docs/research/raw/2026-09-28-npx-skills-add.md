# `npx skills add` installs the user skill (2026-09-28, #139)

Ticket #139 (spec #132): `npx skills add` against the repository installs the skill into a scratch project.

## Setup

- `skills` CLI 1.5.18 (`npx -y skills@1.5.18`), Node from the maintainer's WSL2 machine.
- Telemetry off: `DISABLE_TELEMETRY=1 DO_NOT_TRACK=1`. `NO_COLOR=1`; spinner frames and cursor escapes are removed below.
- The scratch project is an empty `git init` directory in a temporary scratchpad.
- The skill is at its #139 state: `skills/langfuse-api-mcp/` holds `SKILL.md` and the `cost-latency`, `experiments`, `prompts`, `scores` and `traces` references (the `errors` reference is its own ticket). Part 1 was run again after the review changed the description; the output below is from that final run.

## Which source form was run, and why

The README form is `npx skills add rodrigorjsf/langfuse-api-mcp`. It clones the repository's default branch (`main`). At the time of this run, the skill existed only on the unmerged spec branch `spec/132-spec-m6-user-skill-langfuse-api-mcp-skil`, which is not on `origin` (`git ls-remote origin` lists no such branch). So the run has two parts:

1. **The skill itself** was installed from the local checkout of the ticket's branch: `npx skills add <checkout path>`. The CLI reads that source the same way it reads a clone.
2. **The repository form** was run against `origin`. It proves that the CLI clones the private repository with the git credentials already on the machine (no token passed, `GIT_TERMINAL_PROMPT=0`). It also shows that `main` has no skill yet.

The repository form installs the skill only once the spec branch is merged to `main`. Re-run step 2 after that merge.

## 1. Install from the checkout into a scratch project

```console
$ npx -y skills@1.5.18 add <checkout> --list
◇  Local path validated
◇  Found 1 skill
◇  Available Skills
│    langfuse-api-mcp
│      Investigate Langfuse traces, sessions, scores, prompts, datasets and metrics with the langfuse-api-mcp tools (search_operations, describe_operation, execute_read, get_trace_tree, execute_write).
└  Use --skill <name> to install specific skills

$ npx -y skills@1.5.18 add <checkout> --agent claude-code --copy -y
●  Skill: langfuse-api-mcp
│  Investigate Langfuse traces, sessions, scores, prompts, datasets and metrics with the langfuse-api-mcp tools (search_operations, describe_operation, execute_read, get_trace_tree, execute_write).
◇  Installation Summary
│  ./.agents/skills/langfuse-api-mcp
│    copy → Claude Code
◇  Installation complete
◇  Installed 1 skill
│  ✓ langfuse-api-mcp (copied)
│    → ./.claude/skills/langfuse-api-mcp
└  Done!  Review skills before use; they run with full agent permissions.

$ find .claude -type f | sort
.claude/skills/langfuse-api-mcp/SKILL.md
.claude/skills/langfuse-api-mcp/references/cost-latency.md
.claude/skills/langfuse-api-mcp/references/experiments.md
.claude/skills/langfuse-api-mcp/references/prompts.md
.claude/skills/langfuse-api-mcp/references/scores.md
.claude/skills/langfuse-api-mcp/references/traces.md

$ diff -r .claude/skills/langfuse-api-mcp <checkout>/skills/langfuse-api-mcp && echo IDENTICAL
IDENTICAL
```

The CLI finds the one skill under `skills/`, installs it into Claude Code's project directory (`.claude/skills/langfuse-api-mcp/`) and copies every file byte for byte. `--agent claude-code --copy -y` makes the run non-interactive. According to `npx skills --help`, `--agent <agents>` names the hosts to install for (`'*'` for all), and without `--copy` the CLI symlinks the host's directory to the shared `.agents/skills/` copy. Only Claude Code was installed and checked in this run.

## 2. The repository form against `origin`

```console
$ GIT_TERMINAL_PROMPT=0 npx -y skills@1.5.18 add rodrigorjsf/langfuse-api-mcp --list
●  claude-code_2-1-283_agent  Agent detected — installing non-interactively
◇  Source: https://github.com/rodrigorjsf/langfuse-api-mcp.git
◇  Repository cloned
◇  No skills found
└  No valid skills found. Skills require a SKILL.md with name and description.
```

The private repository clones with the machine's existing git credentials. "No skills found" is expected: `main` does not have `skills/` yet.

## Not proven here

- **Loading and workflows.** Loading the skill on its own from a natural request, and completing the trace, cost, prompt and experiment workflows, is spec #132's manual proof 3. It is a separate run.
- **Claude Desktop.** Uploading the release ZIP in Claude Desktop is proven by hand once a ZIP exists. `scripts/pack-skill.py` builds it, and the release workflow's snapshot job checks its layout.
