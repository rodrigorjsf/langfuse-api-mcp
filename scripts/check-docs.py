#!/usr/bin/env python3
#
# WHAT: the offline docs check (spec #150, ticket #155). It reads the risk-to-control mapping
#       table of docs/research/security.md (the first table under "## Risk-to-control mapping")
#       and fails when:
#       - the table, its |---| separator row or its Status column is missing;
#       - a row has no status, or a status other than `tested`, `planned` or `accepted-risk`
#         (the cell reads `<status>: <detail>`);
#       - a `tested` row names no test, or names one (in backticks) that is not a
#         `func Test…`/`func Fuzz…` of the repository's Go test files or a `def test_…` of the
#         scripts' Python tests (scripts/test_*.py);
#       - a `planned` row links no issue (`#N` or a GitHub `/issues/N` URL);
#       - an `accepted-risk` row gives no reason;
#       - a README.md line holding the word `Planned` links no issue by its GitHub URL
#         (`/issues/N`; GitHub does not turn a bare #N into a link in a repository file) (#157).
#       It prints one line per problem, or "OK <file>" per file, and exits 1 on any problem.
# WHY:  the M7 exit criterion "security mapping all green" is measurable only while each row's
#       status is true: a `tested` row must name tests that exist, and a `planned` row must link
#       the issue that tracks the gap. Likewise a README promise is told from a feature only while
#       every `Planned` mark links the issue that delivers it. The check is offline: it never asks GitHub whether an
#       issue is open (the whole-codebase security review, #156, checks that once by hand).
# WHEN: on every push and pull request (.github/workflows/ci.yml, job "generator"), and by hand
#       after editing the mapping. Offline tests: scripts/test_check_docs.py.
# HOW:  python3 scripts/check-docs.py [MAPPING_FILE [README_FILE]]
#       (Python 3.11+, standard library only; defaults: docs/research/security.md, README.md)

import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DEFAULT_MAPPING = "docs/research/security.md"
DEFAULT_README = "README.md"
MAPPING_HEADING = "## Risk-to-control mapping"
STATUSES = ("tested", "planned", "accepted-risk")
SKIP_DIRS = {".git", ".codegraph", "node_modules"}

GO_TEST_FUNC = re.compile(r"^func ((?:Test|Fuzz)\w+)\(", re.M)
PY_TEST_FUNC = re.compile(r"^\s*def (test_\w+)\(", re.M)
CITED_TEST = re.compile(r"`((?:Test|Fuzz)\w+|test_\w+)`")
ISSUE_LINK = re.compile(r"(?:/issues/|#)\d+\b")
PLANNED_MARK = re.compile(r"\bPlanned\b")
ISSUE_URL = re.compile(r"/issues/\d+\b")
STATUS_CELL = re.compile(r"^`?([\w-]+)`?:(.*)$", re.S)


def known_test_names(root):
    """Return the test names of the repository under root: every Test/Fuzz function of a Go
    *_test.go file and every test_ method or function of a scripts/test_*.py file."""
    names = set()
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for name in filenames:
            if name.endswith("_test.go"):
                pattern = GO_TEST_FUNC
            elif (name.startswith("test_") and name.endswith(".py")
                  and os.path.relpath(dirpath, root) == "scripts"):
                pattern = PY_TEST_FUNC
            else:
                continue
            with open(os.path.join(dirpath, name), encoding="utf-8") as f:
                names.update(pattern.findall(f.read()))
    return names


def split_row(line):
    """Return the cells of a Markdown table row, honouring an escaped pipe (\\|)."""
    body = line.strip()
    body = body[1:] if body.startswith("|") else body
    body = body[:-1] if body.endswith("|") and not body.endswith("\\|") else body
    return [cell.strip() for cell in re.split(r"(?<!\\)\|", body)]


def mapping_rows(text):
    """Return (header cells, separator cells, [(line number, cells)]) of the first table under
    the mapping heading, or None when there is no such table."""
    lines = text.splitlines()
    try:
        start = next(i for i, line in enumerate(lines) if line.strip() == MAPPING_HEADING)
    except StopIteration:
        return None
    table = []
    for i in range(start + 1, len(lines)):
        line = lines[i]
        if line.startswith("## "):
            break
        if line.lstrip().startswith("|"):
            table.append((i + 1, line))
        elif table:
            break
    if len(table) < 2:
        return None
    header, separator = split_row(table[0][1]), split_row(table[1][1])
    rows = [(n, split_row(line)) for n, line in table[2:]]
    return header, separator, rows


def status_problem(cell, known):
    """Return what is wrong with one Status cell, or None."""
    if not cell:
        return "has no status"
    m = STATUS_CELL.match(cell)
    if not m or m.group(1) not in STATUSES:
        word = m.group(1) if m else cell.split()[0].strip("`")
        return f"has an unknown status '{word}'; use `tested: …`, `planned: …` or `accepted-risk: …`"
    status, detail = m.group(1), m.group(2).strip()
    if status == "tested":
        cited = CITED_TEST.findall(detail)
        if not cited:
            return "is `tested` but names no test (a Go Test/Fuzz function or a Python test_, in backticks)"
        missing = [name for name in dict.fromkeys(cited) if name not in known]
        if missing:
            return ("is `tested` but names " + ", ".join(missing)
                    + ", which is not a test in the Go or Python tests")
    elif status == "planned":
        if not ISSUE_LINK.search(detail):
            return "is `planned` but links no issue (#N or a GitHub /issues/N link)"
    elif not detail:
        return "is `accepted-risk` but gives no reason"
    return None


def check_mapping(text, known):
    """Return the problems of the mapping in a document, as printable lines."""
    table = mapping_rows(text)
    if table is None:
        return [f"no risk-to-control mapping table under '{MAPPING_HEADING}'"]
    header, separator, rows = table
    if not all(re.fullmatch(r":?-{3,}:?", cell) for cell in separator):
        return ["the risk-to-control mapping has no |---| separator row under its header"]
    if "Status" not in header:
        return ["the risk-to-control mapping has no Status column"]
    col = header.index("Status")
    problems = []
    for line, cells in rows:
        cell = cells[col] if col < len(cells) else ""
        problem = status_problem(cell, known)
        if problem:
            problems.append(f"line {line}: row '{cells[0]}' {problem}")
    return problems


def check_readme(text):
    """Return the README lines holding a `Planned` mark but no issue URL, as printable lines."""
    return [f"line {n}: a Planned mark links no issue (a GitHub /issues/N link)"
            for n, line in enumerate(text.splitlines(), start=1)
            if PLANNED_MARK.search(line) and not ISSUE_URL.search(line)]


def main(argv):
    """Check the mapping and README files in argv (defaults: docs/research/security.md,
    README.md); return the exit status."""
    known = known_test_names(ROOT)
    checks = ((DEFAULT_MAPPING, lambda text: check_mapping(text, known)), (DEFAULT_README, check_readme))
    failed = False
    for i, (default, check) in enumerate(checks):
        path, label = (argv[i], argv[i]) if i < len(argv) else (os.path.join(ROOT, default), default)
        with open(path, encoding="utf-8") as f:
            problems = check(f.read())
        for problem in problems:
            print(f"{label}: {problem}")
        if problems:
            failed = True
        else:
            print(f"OK {label}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
