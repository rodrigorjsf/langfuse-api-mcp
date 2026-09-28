#!/usr/bin/env python3
#
# WHAT: offline unit tests of scripts/pack-skill.py (spec #132 seam S4, ticket
#       #139): the skill ZIP for Claude Desktop holds the skill folder as its
#       root, with the entry file and every file beside or below it, and
#       nothing else; its name carries the build's version; a directory
#       without an entry file is refused.
# WHY:  Claude Desktop installs a skill only by uploading a ZIP whose root is
#       the skill's folder; a ZIP with the files at its root, or missing a
#       reference, installs a broken skill.
# WHEN: on every push and pull request (.github/workflows/ci.yml, job
#       "generator"), and by hand after changing the pack script.
# HOW:  python3 scripts/test_pack_skill.py   (Python 3.11+, standard library only)

import importlib.util
import io
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
import zipfile
from contextlib import redirect_stdout

sys.dont_write_bytecode = True  # no scripts/__pycache__ left behind

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
REAL_SKILL = os.path.join(ROOT, "skills", "langfuse-api-mcp")

_spec = importlib.util.spec_from_file_location("pack_skill", os.path.join(HERE, "pack-skill.py"))
packmod = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(packmod)


def tempdir(test):
    tmp = tempfile.mkdtemp()
    test.addCleanup(shutil.rmtree, tmp)
    return tmp


def pack(argv):
    """Run main(argv); return (exit status, stdout)."""
    out = io.StringIO()
    with redirect_stdout(out):
        status = packmod.main(argv)
    return status, out.getvalue()


def small_skill(test):
    """A two-file skill in a temporary directory."""
    skill = os.path.join(tempdir(test), "demo-skill")
    os.makedirs(os.path.join(skill, "references"))
    with open(os.path.join(skill, "SKILL.md"), "w", encoding="utf-8") as f:
        f.write("---\nname: demo-skill\ndescription: A demo.\n---\n\n# Demo\n")
    with open(os.path.join(skill, "references", "one.md"), "w", encoding="utf-8") as f:
        f.write("# One\n")
    return skill


class PackTest(unittest.TestCase):
    def test_the_zip_holds_the_skill_folder_as_its_root(self):
        out_dir = tempdir(self)
        status, stdout = pack(["1.2.3", out_dir, small_skill(self)])
        self.assertEqual(0, status)
        path = os.path.join(out_dir, "demo-skill-skill_1.2.3.zip")
        self.assertEqual(path + "\n", stdout)
        with zipfile.ZipFile(path) as z:
            self.assertEqual(["demo-skill/SKILL.md", "demo-skill/references/one.md"], z.namelist())
            self.assertEqual(b"# One\n", z.read("demo-skill/references/one.md"))

    def test_the_real_skill_zip_holds_every_file_of_the_skill(self):
        out_dir = tempdir(self)
        status, _ = pack(["0.0.0-SNAPSHOT-abc1234", out_dir])
        self.assertEqual(0, status)
        with zipfile.ZipFile(os.path.join(out_dir, "langfuse-api-mcp-skill_0.0.0-SNAPSHOT-abc1234.zip")) as z:
            names = z.namelist()
        self.assertIn("langfuse-api-mcp/SKILL.md", names)
        self.assertIn("langfuse-api-mcp/references/traces.md", names)
        # The independent source of truth: the files git tracks under the skill.
        tracked = subprocess.run(["git", "-C", ROOT, "ls-files", "skills/langfuse-api-mcp"],
                                 capture_output=True, text=True, check=True).stdout.split()
        self.assertEqual(sorted(t.removeprefix("skills/") for t in tracked), sorted(names))

    def test_packing_twice_gives_the_same_bytes(self):
        skill = small_skill(self)
        a, b = tempdir(self), tempdir(self)
        pack(["1.0.0", a, skill])
        os.utime(os.path.join(skill, "SKILL.md"), (0, 0))
        pack(["1.0.0", b, skill])
        with open(os.path.join(a, "demo-skill-skill_1.0.0.zip"), "rb") as fa, \
                open(os.path.join(b, "demo-skill-skill_1.0.0.zip"), "rb") as fb:
            self.assertEqual(fa.read(), fb.read())

    def test_a_directory_without_an_entry_file_is_refused(self):
        skill = small_skill(self)
        os.remove(os.path.join(skill, "SKILL.md"))
        out_dir = tempdir(self)
        status, _ = pack(["1.0.0", out_dir, skill])
        self.assertEqual(1, status)
        self.assertEqual([], os.listdir(out_dir))

    def test_a_missing_argument_is_refused(self):
        status, _ = pack(["1.0.0"])
        self.assertEqual(2, status)


if __name__ == "__main__":
    unittest.main()
