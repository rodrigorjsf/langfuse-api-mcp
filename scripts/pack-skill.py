#!/usr/bin/env python3
#
# WHAT: packs the user skill into the ZIP Claude Desktop uploads (spec #132
#       seam S4, ticket #139): <OUT_DIR>/<skill>-skill_<VERSION>.zip, whose
#       root is the skill's folder (<skill>/SKILL.md, <skill>/references/...),
#       holding every file of the skill directory and nothing else. Entries
#       are sorted, dated 1980-01-01 and mode 0644, so the same skill and
#       version always give the same bytes. It prints the ZIP's path.
# WHY:  Claude Desktop installs skills only by upload, and its upload wants
#       the skill folder as the ZIP's root (support.claude.com "How to create
#       custom Skills"). Every other host installs from the repository with
#       `npx skills add` (ADR-0005 amendment, M6). The ZIP is attached to the
#       GitHub release only on a `v*` tag (.github/workflows/release.yml).
# WHEN: in the release workflow's snapshot job, after GoReleaser (the version
#       is the build's), and by hand to try an upload.
#       Offline tests: scripts/test_pack_skill.py.
# HOW:  python3 scripts/pack-skill.py VERSION OUT_DIR [SKILL_DIR]
#       (Python 3.11+, standard library only; SKILL_DIR defaults to
#       skills/langfuse-api-mcp). Exit 1 when SKILL_DIR has no SKILL.md,
#       2 on a usage error.

import os
import sys
import zipfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DEFAULT_SKILL = os.path.join(ROOT, "skills", "langfuse-api-mcp")
ENTRY = "SKILL.md"
EPOCH = (1980, 1, 1, 0, 0, 0)  # the earliest ZIP timestamp


def skill_files(skill_dir):
    """Return the skill-relative paths of every file under skill_dir, sorted,
    with forward slashes."""
    files = []
    for dirpath, _, filenames in os.walk(skill_dir):
        for name in filenames:
            files.append(os.path.relpath(os.path.join(dirpath, name), skill_dir).replace(os.sep, "/"))
    return sorted(files)


def pack(version, out_dir, skill_dir):
    """Write the skill ZIP and return its path."""
    name = os.path.basename(os.path.normpath(skill_dir))
    path = os.path.join(out_dir, f"{name}-skill_{version}.zip")
    os.makedirs(out_dir, exist_ok=True)
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        for rel in skill_files(skill_dir):
            info = zipfile.ZipInfo(f"{name}/{rel}", date_time=EPOCH)
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            with open(os.path.join(skill_dir, rel), "rb") as f:
                z.writestr(info, f.read())
    return path


def main(argv):
    """Pack the skill named by argv; return the exit status."""
    if len(argv) not in (2, 3):
        print("usage: pack-skill.py VERSION OUT_DIR [SKILL_DIR]", file=sys.stderr)
        return 2
    version, out_dir = argv[0], argv[1]
    skill_dir = argv[2] if len(argv) == 3 else DEFAULT_SKILL
    if not os.path.isfile(os.path.join(skill_dir, ENTRY)):
        print(f"pack-skill: {skill_dir} has no {ENTRY}", file=sys.stderr)
        return 1
    print(pack(version, out_dir, skill_dir))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
