#!/usr/bin/env python3
#
# WHAT: offline unit tests of scripts/check-docs.py (spec #150 seam 3, ticket #155): the check
#       passes on the real risk-to-control mapping of docs/research/security.md and on a passing
#       fixture, and fails on one fixture per rule: a row with no status, an unknown status, a
#       `tested` row naming a test that does not exist or naming none, a `planned` row linking no
#       issue, an `accepted-risk` row giving no reason, and a mapping without its Status column.
#       The README rule (#157): the real README.md passes, and a line holding `Planned` without a
#       GitHub /issues/N link fails.
#       The known test names come from the repository's Go and Python tests.
# WHY:  "security mapping all green" (M7 exit criterion) is measurable only while every row's
#       status is checked: a `tested` row must name a test that exists, a `planned` row must link
#       the issue that tracks the gap.
# WHEN: on every push and pull request (.github/workflows/ci.yml, job "generator"), and by hand
#       after changing the check.
# HOW:  python3 scripts/test_check_docs.py   (Python 3.11+, standard library only)

import importlib.util
import io
import os
import shutil
import sys
import tempfile
import unittest
from contextlib import redirect_stdout

sys.dont_write_bytecode = True  # no scripts/__pycache__ left behind

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
REAL_MAPPING = os.path.join(ROOT, "docs", "research", "security.md")
REAL_README = os.path.join(ROOT, "README.md")

_spec = importlib.util.spec_from_file_location("check_docs", os.path.join(HERE, "check-docs.py"))
checkmod = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(checkmod)

# Test names that exist in the repository: one Go test, one Go fuzz test, one Python test.
GO_TEST = "TestLoadRefusesLangfuseKeysInTheConfigFile"
GO_FUZZ = "FuzzCheckBody"
PY_TEST = "test_the_real_skill_passes"

HEADER = """# Security research

## Risk-to-control mapping

| Risk ID | Risk | Concrete control in this server | Source | Status |
|---|---|---|---|---|
"""

PASSING_ROWS = [
    f"| MCP01:2025 | Secrets | Keys from the environment only. | README | tested: `{GO_TEST}`, `{GO_FUZZ}` |",
    f"| LLM01:2026 | Skill text | The skill check. | README | tested: `{PY_TEST}` |",
    "| MCP07:2025 | HTTP | Loopback only. | spec | planned: [#160](https://github.com/o/r/issues/160) |",
    "| MCP09:2025 | Shadow servers | Signed releases. | README | planned: #150 |",
    "| ASI06:2026 | Memory | Stateless. | PDF | accepted-risk: the client owns agent memory |",
    "| ASI07:2026 | A \\| pipe | Escaped pipe in a cell. | PDF | `accepted-risk`: the status may sit in backticks |",
]

TRAILER = """
## 1. Next section

| Not | The | Mapping |
|---|---|---|
| a | b | c |
"""


def mapping(*rows, header=HEADER):
    return header + "\n".join(rows) + "\n" + TRAILER


def known():
    return checkmod.known_test_names(ROOT)


class RealMappingTest(unittest.TestCase):
    def test_the_real_mapping_passes(self):
        with open(REAL_MAPPING, encoding="utf-8") as f:
            self.assertEqual([], checkmod.check_mapping(f.read(), known()))


class KnownTestNamesTest(unittest.TestCase):
    def test_go_tests_fuzz_tests_and_python_tests_are_known(self):
        names = known()
        for name in (GO_TEST, GO_FUZZ, PY_TEST):
            with self.subTest(name=name):
                self.assertIn(name, names)

    def test_a_function_outside_a_test_file_is_not_a_test(self):
        tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, tmp)
        os.makedirs(os.path.join(tmp, "pkg"))
        os.makedirs(os.path.join(tmp, "scripts"))
        with open(os.path.join(tmp, "pkg", "a_test.go"), "w", encoding="utf-8") as f:
            f.write("package pkg\n\nfunc TestInATestFile(t *testing.T) {}\n")
        with open(os.path.join(tmp, "pkg", "a.go"), "w", encoding="utf-8") as f:
            f.write("package pkg\n\nfunc TestLookAlikeInCode(t *testing.T) {}\n")
        with open(os.path.join(tmp, "scripts", "test_a.py"), "w", encoding="utf-8") as f:
            f.write("class T:\n    def test_in_a_python_test(self):\n        pass\n")
        with open(os.path.join(tmp, "scripts", "a.py"), "w", encoding="utf-8") as f:
            f.write("def test_look_alike_in_code():\n    pass\n")
        self.assertEqual({"TestInATestFile", "test_in_a_python_test"}, checkmod.known_test_names(tmp))


class PassingFixtureTest(unittest.TestCase):
    def test_a_mapping_where_every_row_has_a_valid_status_passes(self):
        self.assertEqual([], checkmod.check_mapping(mapping(*PASSING_ROWS), known()))


class FailureRuleTest(unittest.TestCase):
    """One fixture per rule: the passing fixture with one row broken."""

    def problems(self, row):
        return checkmod.check_mapping(mapping(*PASSING_ROWS, row), known())

    def assertOneProblem(self, row, *fragments):
        problems = self.problems(row)
        self.assertEqual(1, len(problems), problems)
        for fragment in fragments:
            self.assertIn(fragment, problems[0])

    def test_a_row_with_an_empty_status_fails(self):
        self.assertOneProblem("| X1 | Risk | Control. | Source |  |", "X1", "no status")

    def test_a_row_missing_its_status_cell_fails(self):
        self.assertOneProblem("| X2 | Risk | Control. | Source |", "X2", "no status")

    def test_an_unknown_status_fails(self):
        self.assertOneProblem("| X3 | Risk | Control. | Source | done: shipped |", "X3", "unknown status 'done'")

    def test_a_status_without_its_colon_is_unknown(self):
        self.assertOneProblem(f"| X4 | Risk | Control. | Source | tested `{GO_TEST}` |", "X4", "unknown status")

    def test_a_tested_row_naming_a_test_that_does_not_exist_fails(self):
        self.assertOneProblem(f"| X5 | Risk | Control. | Source | tested: `{GO_TEST}`, `TestThatDoesNotExist` |",
                              "X5", "TestThatDoesNotExist")

    def test_a_tested_row_naming_a_python_test_that_does_not_exist_fails(self):
        self.assertOneProblem("| X6 | Risk | Control. | Source | tested: `test_that_does_not_exist` |",
                              "X6", "test_that_does_not_exist")

    def test_a_tested_row_naming_no_test_fails(self):
        self.assertOneProblem("| X7 | Risk | Control. | Source | tested: by hand |", "X7", "names no test")

    def test_a_planned_row_linking_no_issue_fails(self):
        self.assertOneProblem("| X8 | Risk | Control. | Source | planned: later |", "X8", "links no issue")

    def test_an_accepted_risk_row_giving_no_reason_fails(self):
        self.assertOneProblem("| X9 | Risk | Control. | Source | accepted-risk: |", "X9", "gives no reason")

    def test_a_mapping_without_a_status_column_fails(self):
        header = HEADER.replace(" Status |", "").replace("|---|---|---|---|---|", "|---|---|---|---|")
        problems = checkmod.check_mapping(mapping("| X10 | Risk | Control. | Source |", header=header), known())
        self.assertEqual(1, len(problems), problems)
        self.assertIn("no Status column", problems[0])

    def test_a_mapping_without_its_separator_row_fails(self):
        header = HEADER.replace("|---|---|---|---|---|\n", "")
        problems = checkmod.check_mapping(mapping(*PASSING_ROWS, header=header), known())
        self.assertEqual(1, len(problems), problems)
        self.assertIn("separator", problems[0])

    def test_a_document_without_the_mapping_fails(self):
        problems = checkmod.check_mapping("# Security research\n\nNo table here.\n", known())
        self.assertEqual(1, len(problems), problems)
        self.assertIn("no risk-to-control mapping table", problems[0])

    def test_a_problem_names_the_line_of_its_row(self):
        problems = self.problems("| X11 | Risk | Control. | Source | planned: later |")
        line = HEADER.count("\n") + len(PASSING_ROWS) + 1
        self.assertTrue(problems[0].startswith(f"line {line}:"), problems)


ISSUE_URL = "https://github.com/rodrigorjsf/langfuse-api-mcp/issues/160"


class ReadmeRuleTest(unittest.TestCase):
    """The README rule: a line holding the word `Planned` links an issue by its GitHub URL."""

    def test_the_real_readme_passes(self):
        with open(REAL_README, encoding="utf-8") as f:
            self.assertEqual([], checkmod.check_readme(f.read()))

    def test_a_planned_line_linking_an_issue_passes(self):
        text = f"# T\n\n| HTTP | **Planned**, [#160]({ISSUE_URL}) |\nA line without the mark.\n"
        self.assertEqual([], checkmod.check_readme(text))

    def test_a_planned_line_linking_no_issue_fails_and_names_its_line(self):
        problems = checkmod.check_readme("# T\n\nShipped.\n\n## Troubleshooting **(Planned)**\n")
        self.assertEqual(1, len(problems), problems)
        self.assertTrue(problems[0].startswith("line 5:"), problems)
        self.assertIn("links no issue", problems[0])

    def test_a_bare_issue_number_is_not_a_link_in_the_readme(self):
        # GitHub renders a repository file without turning #N into a link.
        problems = checkmod.check_readme("Publishing **Planned** (#150).\n")
        self.assertEqual(1, len(problems), problems)

    def test_a_lower_case_anchor_is_not_a_mark(self):
        self.assertEqual([], checkmod.check_readme("See [Verify](#verify-what-you-run-planned).\n"))

    def test_every_unlinked_planned_line_is_reported(self):
        problems = checkmod.check_readme("A **Planned** one.\nFine.\nPlanned until M7.\n")
        self.assertEqual(["line 1", "line 3"], [p.split(":")[0] for p in problems])


class CommandTest(unittest.TestCase):
    def run_main(self, argv):
        out = io.StringIO()
        with redirect_stdout(out):
            status = checkmod.main(argv)
        return status, out.getvalue()

    def fixture(self, text):
        tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, tmp)
        path = os.path.join(tmp, "security.md")
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
        return path

    def test_without_arguments_it_checks_the_real_mapping_and_readme_and_exits_zero(self):
        status, out = self.run_main([])
        self.assertEqual(0, status, out)
        self.assertIn("OK docs/research/security.md", out)
        self.assertIn("OK README.md", out)

    def test_a_readme_with_an_unlinked_planned_line_exits_one_and_prints_it(self):
        mapping_path = self.fixture(mapping(*PASSING_ROWS))
        readme = os.path.join(os.path.dirname(mapping_path), "README.md")
        with open(readme, "w", encoding="utf-8") as f:
            f.write("# T\n\nPublishing **Planned**.\n")
        status, out = self.run_main([mapping_path, readme])
        self.assertEqual(1, status, out)
        self.assertIn(f"{readme}: line 3:", out)
        self.assertIn("links no issue", out)

    def test_a_mapping_with_a_problem_exits_one_and_prints_it(self):
        path = self.fixture(mapping(*PASSING_ROWS, "| X12 | Risk | Control. | Source | planned: later |"))
        status, out = self.run_main([path])
        self.assertEqual(1, status)
        self.assertIn("X12", out)
        self.assertIn("links no issue", out)


if __name__ == "__main__":
    unittest.main()
