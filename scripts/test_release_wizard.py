#!/usr/bin/env python3
#
# WHAT: offline tests of scripts/release-wizard.sh (#186), the interactive wizard that walks the
#       maintainer through cutting a release on every channel. The wizard is never run end to end
#       here: it opens browsers and waits for a person. These tests run it as a black box with
#       stand-ins for git, gh, curl, cosign, node and npx first on PATH, and read its text:
#       - called without a version, or with one that is not X.Y.Z, it prints its usage and runs
#         no command at all;
#       - it keeps the wizard library of the /wizard skill byte for byte (the same as
#         scripts/setup-ci-langfuse-cloud.sh) and has the eleven stages of the ticket, in order;
#       - the preflight changes nothing and stops on an existing tag, a missing tool or a `gh`
#         without `gh attestation`;
#       - with no person at the terminal (standard input closed) it stops before any outward
#         action: no tag, no push, no secret, no job re-run, no milestone closed;
#       - every outward or irreversible action sits behind a confirmation, no tag is ever moved
#         or deleted, a secret is read hidden and goes only to the `release` environment secret
#         that .github/workflows/release.yml reads.
# WHY:  a wizard that tags and pushes cannot be tried against the real services, and a
#       publication cannot be undone, so its safety rules are proven on its text and its first
#       stage is proven by running it against stand-ins.
# WHEN: on every push and pull request (.github/workflows/ci.yml, job "generator"), and by hand
#       after changing scripts/release-wizard.sh.
# HOW:  python3 scripts/test_release_wizard.py   (Python 3.11+, standard library only)

import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
WIZARD = os.path.join(ROOT, "scripts", "release-wizard.sh")
OTHER_WIZARD = os.path.join(ROOT, "scripts", "setup-ci-langfuse-cloud.sh")
RELEASE_WORKFLOW = os.path.join(ROOT, ".github", "workflows", "release.yml")
MARKER = "# STAGES "  # the first line of the stages marker block of the /wizard template

REAL_GIT = shutil.which("git") or "/usr/bin/git"
REAL_JQ = shutil.which("jq") or "/usr/bin/jq"
SYSTEM_PATH = "/usr/bin:/bin"

# One stand-in for every tool the wizard may call. It logs each call, answers the few read-only
# calls the first stages make, and refuses (exit 99, logged as MUTATION) everything that changes
# something, so a test can say "nothing was changed" by reading the log.
STUB = r"""#!/bin/bash
tool=$(basename "$0")
printf '%s %s\n' "$tool" "$*" >> "$STUB_LOG"
mutation() { printf 'MUTATION: %s %s\n' "$tool" "$*" >> "$STUB_LOG"; exit 99; }
case "$tool" in
  git)
    case "$1" in
      ls-remote)
        if [ -n "${STUB_REMOTE_TAG:-}" ]; then printf 'abc123\trefs/tags/%s\n' "$STUB_REMOTE_TAG"; exit 0; fi
        exit 2 ;;
      fetch) exit 0 ;;
      rev-parse|config|remote|log|show|cat-file|for-each-ref|merge-base|status|branch|diff|describe)
        exec "$STUB_REAL_GIT" "$@" ;;
      tag)
        case "$2" in -l|--list|--points-at|-n) exec "$STUB_REAL_GIT" "$@" ;; esac
        mutation "$@" ;;
      *) mutation "$@" ;;
    esac ;;
  gh)
    case "$1 $2" in
      "auth status") exit "${STUB_GH_AUTH:-0}" ;;
      "attestation --help") exit "${STUB_GH_ATTESTATION:-0}" ;;
      "secret set"|"secret delete"|"run rerun"|"release create"|"pr merge"|"issue close"|"issue edit") mutation "$@" ;;
    esac
    for a in "$@"; do
      case "$a" in -X|--method|-f|-F|--field|--raw-field|--input) mutation "$@" ;; esac
    done
    if [ "$1" = api ]; then
      case "$*" in *permissions.admin*) echo "${STUB_GH_ADMIN:-true}"; exit 0 ;; esac
    fi
    echo '[]'; exit 0 ;;
  jq) exec "$STUB_REAL_JQ" "$@" ;;
  curl|cosign|npx|npm) exit 0 ;;
  node) echo v24.21.0; exit 0 ;;
  *) exit 0 ;;
esac
"""

TOOLS = ["git", "gh", "jq", "cosign", "node", "npx", "npm", "curl"]
# What the wizard library calls to open a page: stood in for, so no test opens a real browser.
OPENERS = ["wslview", "explorer.exe", "xdg-open", "open"]


class Sandbox:
    """A scratch directory: a git repository whose origin names the real repository, a bin
    directory of stand-ins, and the log they write."""

    def __init__(self, tools=TOOLS):
        self.dir = tempfile.mkdtemp(prefix="release-wizard-test-")
        self.bin = os.path.join(self.dir, "bin")
        self.repo = os.path.join(self.dir, "repo")
        self.log_path = os.path.join(self.dir, "calls.log")
        os.makedirs(self.bin)
        os.makedirs(self.repo)
        open(self.log_path, "w").close()
        stub = os.path.join(self.bin, "stub")
        with open(stub, "w", encoding="utf-8") as f:
            f.write(STUB)
        os.chmod(stub, 0o755)
        for tool in [*tools, *OPENERS]:
            os.symlink("stub", os.path.join(self.bin, tool))
        git = lambda *a: subprocess.run([REAL_GIT, "-C", self.repo, *a], check=True, capture_output=True)
        git("init", "-q", "-b", "main")
        git("config", "user.name", "Test Maintainer")
        git("config", "user.email", "maintainer@example.invalid")
        git("remote", "add", "origin", "https://github.com/rodrigorjsf/langfuse-api-mcp.git")
        git("commit", "-q", "--allow-empty", "-m", "initial")

    def close(self):
        shutil.rmtree(self.dir, ignore_errors=True)

    def calls(self):
        with open(self.log_path, encoding="utf-8") as f:
            return [line.rstrip("\n") for line in f]

    def run(self, args, env=None, stdin="", timeout=60):
        environment = {
            "PATH": self.bin + os.pathsep + SYSTEM_PATH,
            "HOME": self.dir,
            "STUB_LOG": self.log_path,
            "STUB_REAL_GIT": REAL_GIT,
            "STUB_REAL_JQ": REAL_JQ,
            "TERM": "dumb",
            "TMPDIR": self.dir,
        }
        environment.update(env or {})
        return subprocess.run(
            [WIZARD, *args], cwd=self.repo, env=environment, input=stdin,
            capture_output=True, text=True, timeout=timeout,
        )


def read(path):
    with open(path, encoding="utf-8", newline="") as f:
        return f.read()


def library_of(path):
    """Everything above the stages marker block: the dashed line just before the marker line."""
    lines = read(path).split("\n")
    index = next(i for i, line in enumerate(lines) if line.startswith(MARKER))
    return "\n".join(lines[: index - 1])


def stages_of(path):
    """The text of the stages: from the marker on."""
    lines = read(path).split("\n")
    index = next(i for i, line in enumerate(lines) if line.startswith(MARKER))
    return "\n".join(lines[index:])


class Usage(unittest.TestCase):
    """Called without a version, or with one that is not X.Y.Z, the wizard prints its usage and
    changes nothing: it runs no command, not even git."""

    def setUp(self):
        self.box = Sandbox()
        self.addCleanup(self.box.close)

    def test_no_argument_prints_the_usage_and_runs_nothing(self):
        done = self.box.run([])
        self.assertEqual(done.returncode, 2)
        self.assertIn("Usage:", done.stderr)
        self.assertIn("release-wizard.sh X.Y.Z", done.stderr)
        self.assertEqual(done.stdout, "")
        self.assertEqual(self.box.calls(), [])

    def test_a_version_that_is_not_x_y_z_prints_the_usage_and_runs_nothing(self):
        bad = ["v0.2.0", "0.2", "0.2.0.1", "0.2.0-rc.1", "0.2.0\n", " 0.2.0", "0.2.0 ", "latest", "",
               "0.2.x", "0.2.0;touch pwned", "../0.2.0", "-1.2.3", "0.2.0/../x"]
        for version in bad:
            with self.subTest(version=version):
                done = self.box.run([version])
                self.assertEqual(done.returncode, 2)
                self.assertIn("Usage:", done.stderr)
                self.assertEqual(done.stdout, "")
                self.assertEqual(self.box.calls(), [])
        self.assertFalse(os.path.exists(os.path.join(self.box.repo, "pwned")))

    def test_two_arguments_print_the_usage_and_run_nothing(self):
        done = self.box.run(["0.2.0", "0.3.0"])
        self.assertEqual(done.returncode, 2)
        self.assertIn("Usage:", done.stderr)
        self.assertEqual(self.box.calls(), [])


STAGE_TITLES = [
    "Preflight",
    "Exit checks",
    "Merge the pull request",
    "main is green",
    "Release settings",
    "Tag and push",
    "Watch the Release run",
    "If needed: npm",
    "If needed: MCP Registry",
    "Check the release",
    "Close out",
]


def stage_blocks():
    """(title, text) of each stage: the text from its `stage "…"` line to the next one."""
    text = stages_of(WIZARD)
    starts = [m for m in re.finditer(r'^stage "([^"]*)"', text, re.M)]
    blocks = []
    for i, m in enumerate(starts):
        end = starts[i + 1].start() if i + 1 < len(starts) else len(text)
        blocks.append((m.group(1), text[m.start():end]))
    return blocks


class Form(unittest.TestCase):
    """The file itself: the /wizard skill's library untouched, the header, LF, executable."""

    def test_it_is_executable_with_lf_line_endings(self):
        self.assertTrue(os.stat(WIZARD).st_mode & stat.S_IXUSR)
        with open(WIZARD, "rb") as f:
            self.assertNotIn(b"\r", f.read())

    def test_everything_above_the_stages_marker_is_the_skills_library_unchanged(self):
        # The existing wizard was generated from the same template; its library is the reference.
        self.assertEqual(library_of(WIZARD), library_of(OTHER_WIZARD))
        self.assertTrue(library_of(WIZARD).startswith("#!/usr/bin/env bash\n"))

    def test_the_header_below_the_marker_says_what_why_when_and_how(self):
        header = stages_of(WIZARD)
        for key in ["WHAT:", "WHY:", "WHEN:", "HOW:"]:
            with self.subTest(key=key):
                self.assertRegex(header, r"(?m)^# " + key)

    def test_bash_accepts_the_syntax(self):
        done = subprocess.run(["bash", "-n", WIZARD], capture_output=True, text=True)
        self.assertEqual(done.returncode, 0, done.stderr)

    @unittest.skipUnless(shutil.which("shellcheck"), "shellcheck is not installed")
    def test_shellcheck_finds_nothing_to_report(self):
        # The library is the skill's and is not ours to change: shellcheck reads the whole file, and
        # SC2034 is the library's own RED colour, which no wizard uses.
        done = subprocess.run(["shellcheck", "--exclude=SC2034", WIZARD], capture_output=True, text=True)
        self.assertEqual(done.returncode, 0, done.stdout)


class Stages(unittest.TestCase):
    def test_there_are_eleven_stages_in_the_order_of_the_ticket(self):
        titles = [title for title, _ in stage_blocks()]
        self.assertEqual(len(titles), len(STAGE_TITLES), titles)
        for got, want in zip(titles, STAGE_TITLES):
            with self.subTest(stage=want):
                self.assertTrue(got.startswith(want), f"{got!r} should start with {want!r}")

    def test_the_stage_count_the_wizard_announces_matches(self):
        self.assertRegex(stages_of(WIZARD), r"(?m)^TOTAL_STAGES=11$")


def output_of(done):
    return done.stdout + done.stderr


def mutations(box):
    return [line for line in box.calls() if line.startswith("MUTATION:")]


class Preflight(unittest.TestCase):
    """Stage 1 changes nothing and stops, naming the cause, when the release cannot start."""

    def setUp(self):
        self.box = Sandbox()
        self.addCleanup(self.box.close)

    def test_a_tag_that_exists_locally_stops_the_wizard(self):
        subprocess.run([REAL_GIT, "-C", self.box.repo, "tag", "-a", "v0.2.0", "-m", "v0.2.0"], check=True)
        done = self.box.run(["0.2.0"])
        self.assertEqual(done.returncode, 1)
        self.assertRegex(output_of(done), r"v0\.2\.0 already exists locally")
        self.assertEqual(mutations(self.box), [])
        # The tag is still the one the maintainer made: nothing moved or deleted it.
        tags = subprocess.run([REAL_GIT, "-C", self.box.repo, "tag", "-l"], capture_output=True, text=True)
        self.assertEqual(tags.stdout.split(), ["v0.2.0"])

    def test_a_tag_that_exists_on_origin_stops_the_wizard(self):
        done = self.box.run(["0.2.0"], env={"STUB_REMOTE_TAG": "v0.2.0"})
        self.assertEqual(done.returncode, 1)
        self.assertRegex(output_of(done), r"v0\.2\.0 already exists on origin")
        self.assertEqual(mutations(self.box), [])

    def test_a_missing_tool_is_named(self):
        if shutil.which("cosign", path=SYSTEM_PATH):
            self.skipTest("cosign is on the system path of this machine")
        box = Sandbox(tools=[t for t in TOOLS if t != "cosign"])
        self.addCleanup(box.close)
        done = box.run(["0.2.0"])
        self.assertEqual(done.returncode, 1)
        self.assertRegex(output_of(done), r"cosign is missing")
        self.assertEqual(mutations(box), [])

    def test_a_gh_without_the_attestation_command_is_named_with_the_way_to_get_one(self):
        done = self.box.run(["0.2.0"], env={"STUB_GH_ATTESTATION": "1"})
        self.assertEqual(done.returncode, 1)
        out = output_of(done)
        self.assertIn("gh attestation", out)
        self.assertIn("go install github.com/cli/cli/v2/cmd/gh@latest", out)
        self.assertEqual(mutations(self.box), [])

    def test_a_gh_that_is_not_signed_in_is_named(self):
        done = self.box.run(["0.2.0"], env={"STUB_GH_AUTH": "1"})
        self.assertEqual(done.returncode, 1)
        self.assertIn("gh auth login", output_of(done))

    def test_a_gh_account_that_is_not_an_administrator_is_named(self):
        done = self.box.run(["0.2.0"], env={"STUB_GH_ADMIN": "false"})
        self.assertEqual(done.returncode, 1)
        self.assertRegex(output_of(done), r"administrator")

    def test_an_origin_that_is_not_the_repository_is_named(self):
        subprocess.run([REAL_GIT, "-C", self.box.repo, "remote", "set-url", "origin",
                        "https://github.com/someone/else.git"], check=True)
        done = self.box.run(["0.2.0"])
        self.assertEqual(done.returncode, 1)
        self.assertIn("rodrigorjsf/langfuse-api-mcp", output_of(done))

    def test_with_everything_in_place_and_nobody_at_the_terminal_nothing_is_changed(self):
        done = self.box.run(["0.2.0"], stdin="")
        out = output_of(done)
        self.assertIn("Stage 1/11", out)
        self.assertIn("ready to start", out.lower())
        # Standard input is closed: the first confirmation answers "no" and the wizard stops there.
        self.assertEqual(done.returncode, 1)
        self.assertIn("stopped", out.lower())
        self.assertEqual(mutations(self.box), [])
        pushes = [c for c in self.box.calls() if re.match(r"git (push|tag -a|tag [^-l])", c)]
        self.assertEqual(pushes, [])


DOC = os.path.join(ROOT, "docs", "development", "release-pipeline.md")
SECURITY_MODEL = os.path.join(ROOT, "docs", "reference", "security-model.md")

# A line that runs something, as opposed to one that only prints or records a sentence.
TEXT_ONLY = re.compile(r"^\s*(warn|say|note|step|die|SKIPPED\+=|printf '\\n|#)")

# Every outward or irreversible action: the pattern of its command, and the word its confirmation
# must carry so that the question names it.
OUTWARD = [
    (re.compile(r"\bgit tag -a\b"), "tag"),
    (re.compile(r"\bgit push\b"), "push"),
    (re.compile(r"\bgh secret (set|delete)\b"), "secret"),
    # rerun_job wraps the only `gh run rerun`; its callers are the outward actions.
    (re.compile(r"\brerun_job\b"), "re-run"),
    (re.compile(r"\bgh api -X (PATCH|POST|PUT|DELETE)\b"), "milestone"),
    (re.compile(r"\bgh (pr merge|release (create|delete|edit)|issue (close|edit))\b"), "never"),
]
CONFIRMATION = re.compile(r'(?:stop_unless|confirm) "([^"]*)"')


def code_lines(text):
    return [line for line in text.split("\n") if not TEXT_ONLY.match(line)]


class Safety(unittest.TestCase):
    """Every outward or irreversible action happens only after a confirmation that names it."""

    def test_no_tag_is_ever_moved_or_deleted(self):
        text = "\n".join(code_lines(stages_of(WIZARD)))
        for forbidden in [r"git tag -d", r"git tag -f", r"git tag --delete", r"git tag --force",
                          r"push\b[^\n]*(--force|--delete|-f\b)", r"push origin :", r"push origin \+",
                          r"update-ref", r"git branch -[dD]"]:
            with self.subTest(forbidden=forbidden):
                self.assertIsNone(re.search(forbidden, text), forbidden)
        # The one tag command creates an annotated tag, and the one push names that tag only.
        self.assertEqual(re.findall(r"git tag [^\n]*", text), ['git tag -a "$TAG" -m "$TAG" "$TAG_SHA"'])
        self.assertEqual(re.findall(r"git push [^\n]*", text), ['git push origin "refs/tags/$TAG"; then'])

    def test_every_outward_action_follows_a_confirmation_that_names_it(self):
        seen_actions = 0
        for title, block in stage_blocks():
            confirmations = []
            for line in code_lines(block):
                confirmations += [q.lower() for q in CONFIRMATION.findall(line)]
                for pattern, word in OUTWARD:
                    if pattern.search(line):
                        seen_actions += 1
                        with self.subTest(stage=title, action=line.strip()):
                            self.assertNotEqual(word, "never", "this action is not allowed in the wizard")
                            self.assertTrue(any(word in q for q in confirmations),
                                            f"no confirmation naming {word!r} before: {line.strip()}")
        self.assertGreaterEqual(seen_actions, 7)  # tag, push, secret set, re-run x2, secret delete, milestone

    def test_the_preflight_and_the_checks_run_nothing_outward(self):
        blocks = dict(stage_blocks())
        for title, block in blocks.items():
            if title.startswith(("Preflight", "Exit checks", "main is green", "Check the release")):
                for line in code_lines(block):
                    for pattern, _ in OUTWARD:
                        with self.subTest(stage=title, line=line.strip()):
                            self.assertIsNone(pattern.search(line))

    def test_the_two_if_needed_stages_act_only_after_reading_that_their_job_failed(self):
        blocks = dict(stage_blocks())
        # Each reads the conclusion of its job and acts only in the branch where that is `failure`:
        # npm in the else of "not a failure", the Registry in the `failure)` arm of a case.
        guards = [
            ("If needed: npm", "publish-npm", r'if \[\[ "\$npm_job" != failure \]\]; then\n.*?\n\s*else\n'),
            ("If needed: MCP Registry", "publish-registry", r'case "\$reg_job" in\n\s*failure\)\n'),
        ]
        for prefix, job, guard in guards:
            title = next(t for t in blocks if t.startswith(prefix))
            code = "\n".join(code_lines(blocks[title]))
            first_action = min(m.start() for pattern, _ in OUTWARD for m in pattern.finditer(code))
            before = code[:first_action]
            with self.subTest(stage=title):
                self.assertRegex(before, r"job_conclusion " + job)
                self.assertIsNotNone(re.search(guard, before, re.S), "the action is not behind the failure check")


    def test_a_job_re_run_names_one_job_and_is_never_the_whole_failed_set(self):
        # `--failed` would also re-run a publish-image that stage 7 let pass; one job is named instead.
        code = "\n".join(code_lines(read(WIZARD)))
        self.assertEqual(len(re.findall(r"gh run rerun", code)), 1)
        self.assertIn('gh run rerun "$RUN_ID" -R "$REPO" --job "$id"', code)
        self.assertNotIn("--failed", code)
        calls = re.findall(r"rerun_job ([a-z-]+)", "\n".join(code_lines(stages_of(WIZARD))))
        self.assertEqual(calls, ["publish-npm", "publish-registry"])

    def test_a_stored_token_and_a_pushed_tag_are_never_left_without_a_word(self):
        code = "\n".join(code_lines(read(WIZARD)))
        self.assertRegex(code, r"(?m)^trap on_exit EXIT$")
        self.assertRegex(code, r"(?m)^trap 'exit 130' INT TERM$")
        # The flag is up before the secret is written and down only after it is deleted.
        up = code.index("TOKEN_STORED=1")
        set_ = code.index("gh secret set NPM_TOKEN")
        delete = code.index("gh secret delete NPM_TOKEN")
        down = code.index("TOKEN_STORED=0", delete)  # the first one is the declaration at the top
        self.assertLess(up, set_)
        self.assertLess(delete, down)
        # What on_exit says is a warning line (not code): read it from the file's text.
        self.assertRegex(read(WIZARD), r"(?s)on_exit\(\) \{.*TOKEN_STORED.*gh secret delete NPM_TOKEN --env release.*AFTER_TAG")


class Secrets(unittest.TestCase):
    """A secret is read hidden, goes only to the release environment secret or to the one process
    that needs it, and is never printed, logged or written to a file."""

    def stage_code(self):
        return "\n".join(code_lines(stages_of(WIZARD)))

    def test_no_value_is_saved_to_a_file_by_the_library(self):
        code = self.stage_code()
        self.assertRegex(code, r"(?m)^ENV_FILE=/dev/null$")
        self.assertNotRegex(code, r"\bwrite_env\b")
        self.assertNotRegex(code, r"\bset_secret\b")
        self.assertNotRegex(code, r"\bset_var\b")
        self.assertNotRegex(code, r"set -[a-z]*x|xtrace")

    def test_the_npm_token_is_hidden_and_goes_only_to_the_release_environment_secret(self):
        code = self.stage_code()
        self.assertEqual(len(re.findall(r'ask_secret NPM_TOKEN "', code)), 1)
        allowed = [
            r'^\s*\[\[ -n "\$NPM_TOKEN" \]\] \|\| die "no token entered\."$',
            r'^\s*printf \'%s\' "\$NPM_TOKEN" \| gh secret set NPM_TOKEN --env release -R "\$REPO"$',
        ]
        for line in code.split("\n"):
            if "$NPM_TOKEN" in line:
                with self.subTest(line=line.strip()):
                    self.assertTrue(any(re.match(a, line) for a in allowed), line)
        self.assertNotRegex(code, r"gh secret set[^\n]*(--body|-b )")
        self.assertRegex(code, r"(?m)^\s*unset NPM_TOKEN$")

    def test_the_langfuse_key_pair_is_read_for_one_process_and_written_nowhere(self):
        code = self.stage_code()
        self.assertEqual(len(re.findall(r'ask_secret LF_SK "', code)), 1)
        self.assertEqual(len(re.findall(r'ask_secret LF_PK "', code)), 1)  # the project redacts pk-lf- too
        for line in code.split("\n"):
            if re.search(r"\$\{?LF_(SK|PK|URL)", line):
                with self.subTest(line=line.strip()):
                    ok = (re.match(r'^\s*if \[\[ -n "\$LF_URL" && -n "\$LF_PK" && -n "\$LF_SK" \]\]; then$', line)
                          # the environment of the one process that needs the keys, never an argument
                          or re.search(r'\| LANGFUSE_BASE_URL="\$LF_URL" LANGFUSE_PUBLIC_KEY="\$LF_PK" '
                                       r'LANGFUSE_SECRET_KEY="\$LF_SK" "\$\{limit\[@\]\}" npx -y ', line))
                    self.assertTrue(ok, line)
        self.assertRegex(code, r"(?m)^unset LF_URL LF_PK LF_SK reply$")
        # The transcript gets the version npx reported and nothing of the reply or the keys.
        self.assertNotRegex(code, r"\$reply[^\n]*>>\s*\"?\$TRANSCRIPT")


class Agreement(unittest.TestCase):
    """What the wizard names exists where it points: the workflow, its jobs, its secret, the docs."""

    def workflow(self):
        return read(RELEASE_WORKFLOW)

    def job(self, name):
        m = re.search(r"(?ms)^  " + re.escape(name) + r":\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", self.workflow())
        self.assertIsNotNone(m, f"no job {name} in release.yml")
        return m.group(1)

    def test_the_secret_the_wizard_sets_is_the_one_publish_npm_reads_in_the_environment_it_runs_in(self):
        wizard = read(WIZARD)
        self.assertEqual(sorted(set(re.findall(r"gh secret (?:set|delete) (\w+) --env (\w+)", wizard))),
                         [("NPM_TOKEN", "release")])
        job = self.job("publish-npm")
        self.assertRegex(job, r"environment: release\b")
        self.assertRegex(job, r"NODE_AUTH_TOKEN: \$\{\{ secrets\.NPM_TOKEN \}\}")

    def test_the_jobs_and_workflow_files_the_wizard_reads_exist(self):
        for job in ["publish-npm", "publish-registry"]:
            with self.subTest(job=job):
                self.job(job)
        for wf in ["ci.yml", "release.yml"]:
            with self.subTest(workflow=wf):
                self.assertTrue(os.path.exists(os.path.join(ROOT, ".github", "workflows", wf)))
        self.assertRegex(self.workflow(), r"\^v\[0-9\]\+\\\.\[0-9\]\+\\\.\[0-9\]\+\$")  # the tag pattern the wizard accepts

    def test_the_checks_of_stage_10_are_the_ones_the_security_model_gives(self):
        wizard, doc = read(WIZARD), read(SECURITY_MODEL)
        for command in ["cosign verify-blob --bundle", "cosign verify ", "gh attestation verify",
                        "--certificate-oidc-issuer https://token.actions.githubusercontent.com"]:
            with self.subTest(command=command):
                self.assertIn(command, doc)
        self.assertIn('IDENTITY="https://github.com/$REPO/.github/workflows/release.yml@refs/tags/$TAG"', wizard)
        self.assertIn('identity="https://github.com/rodrigorjsf/langfuse-api-mcp/.github/workflows/release.yml@refs/tags/v$version"', doc)
        self.assertIn("cosign verify-blob --bundle", wizard)
        self.assertIn("gh attestation verify", wizard)
        self.assertIn("npm audit signatures", wizard)

    def test_the_procedure_doc_points_to_the_wizard_and_keeps_the_facts_for_when_it_stops(self):
        doc = read(DOC)
        self.assertIn("scripts/release-wizard.sh", doc)
        # The wizard's own message points to this heading.
        self.assertIn("If the wizard stops after the tag", doc)
        for fact in ["release tags", "`release` environment", "NPM_TOKEN", "publish-registry"]:
            with self.subTest(fact=fact):
                self.assertIn(fact, doc)


if __name__ == "__main__":
    unittest.main(verbosity=2)
