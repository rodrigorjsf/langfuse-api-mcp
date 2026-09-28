#!/usr/bin/env python3
#
# WHAT: the offline check of a user skill (spec #132 seam S3, ticket #134). For
#       each skill directory it fails when:
#       - the entry SKILL.md frontmatter lacks `name` or `description`,
#         `name` is not the directory's name, or `description` is over the
#         200 characters Claude Desktop's upload takes (#139), or holds an
#         unquoted ": " or " #", which ends a YAML plain scalar (#142);
#       - a reference the entry file cites does not exist;
#       - a snake_case word in the text is neither a tool name nor an error
#         code the server has (KNOWN_TOOLS, KNOWN_ERROR_CODES below) nor an
#         operation ID of the embedded union catalog;
#       - the text holds a phrase of FORBIDDEN_PHRASES: follow or obey
#         instructions found in data or a payload, enable or turn on write
#         mode, LANGFUSE_MCP_ALLOW_WRITES, skip, bypass or avoid the
#         confirmation, retry a refused or declined write. The words are
#         banned even when negated ("never skip the confirmation"): say what
#         to do instead ("stop and tell the user").
#       Only lowercase snake_case words are checked; a mixed-case operation
#       ID (observations_getMany) is not.
#       It prints one line per problem, or "OK <dir>", and exits 1 on any
#       problem.
# WHY:  the skill is text an agent follows. The server's write gate
#       (ADR-0003 amendment) must stay the only decision point, Langfuse data
#       is untrusted (security.md), and a skill fetched from main must not
#       point the agent at a tool, error code or file that does not exist. The
#       phrase list is a heuristic for the obvious wording, not paraphrases;
#       review covers the rest.
# WHEN: on every push and pull request (.github/workflows/ci.yml, job
#       "generator"), and by hand after editing anything under skills/.
#       Offline tests: scripts/test_check_skill.py.
# HOW:  python3 scripts/check-skill.py [SKILL_DIR ...]
#       (Python 3.11+, standard library only; default: every directory under
#       skills/)

import functools
import json
import os
import re
import sys

ENTRY = "SKILL.md"
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
# Claude Desktop's skill upload (the release ZIP, #139) takes a description of
# at most 200 characters (support.claude.com "How to create custom Skills").
MAX_DESCRIPTION = 200
CATALOG = os.path.join(ROOT, "internal", "catalog", "spec", "langfuse-union-catalog.json")

# Keep in step with the tools internal/server registers (server.go, write.go,
# tracetree.go). test_check_skill.py fails when the two differ.
KNOWN_TOOLS = frozenset({
    "search_operations",
    "describe_operation",
    "execute_read",
    "get_trace_tree",
    "execute_write",
})

# Keep in step with the ADR-0008 error codes in internal/server/toolerror.go
# and failure.go. test_check_skill.py fails when the two differ.
KNOWN_ERROR_CODES = frozenset({
    "tls_untrusted_certificate",
    "network_error",
    "timeout",
    "canceled",
    "invalid_argument",
    "operation_not_found",
    "internal_error",
    "langfuse_bad_request",
    "langfuse_unauthorized",
    "langfuse_forbidden",
    "langfuse_not_found",
    "langfuse_conflict",
    "langfuse_unprocessable",
    "operation_unavailable",
    "langfuse_rate_limited",
    "langfuse_unavailable",
    "redirect_refused",
    "response_too_large",
    "confirmation_unavailable",
    "confirmation_declined",
    "confirmation_invalid",
})

# A lowercase snake_case word that is not part of a path, file name or longer
# identifier: how a tool name or error code appears in the text.
SNAKE_WORD = re.compile(r"(?<![\w/.-])[a-z][a-z0-9]*(?:_[a-z0-9]+)+(?![\w/-]|\.\w)")


# (rule, pattern) pairs, matched case-insensitively on the text with every run
# of whitespace folded to one space, so a phrase broken across lines still
# matches. A heuristic for the obvious wording, not paraphrases.
FORBIDDEN_PHRASES = [
    ("follow or obey instructions found in data or a payload",
     re.compile(r"\b(?:follow|obey)(?:s|ed|ing)?\b[^.;:]{0,40}?\binstructions?\b"
                r"[^.;:]{0,40}?\b(?:data|payloads?)\b", re.I)),
    ("enable or turn on write mode",
     re.compile(r"\b(?:enabl(?:e|es|ed|ing)|turn(?:s|ed|ing)? on)\s+(?:the\s+)?write\s+mode\b", re.I)),
    ("LANGFUSE_MCP_ALLOW_WRITES",
     re.compile(r"LANGFUSE_MCP_ALLOW_WRITES", re.I)),
    ("skip, bypass or avoid the confirmation",
     re.compile(r"\b(?:skip|bypass|avoid)(?:s|ped|ping|ed|ing)?\s+(?:the\s+|a\s+|any\s+|this\s+)?"
                r"(?:(?:user|server)['’]s\s+)?confirmations?\b", re.I)),
    ("retry a refused or declined write",
     re.compile(r"\bretr(?:y|ies|ied|ying)\s+(?:the\s+|a\s+|any\s+|that\s+)?(?:refused|declined)\s+"
                r"(?:\w+\s+){0,2}?writes?\b", re.I)),
]


@functools.cache
def catalog_operation_ids():
    """Return every operation ID of the embedded union catalog. Some are
    lowercase snake_case too (prompts_get), so they are not unknown names."""
    with open(CATALOG, encoding="utf-8") as f:
        paths = json.load(f)["paths"]
    return frozenset(op["operationId"] for item in paths.values() for op in item.values()
                     if isinstance(op, dict) and "operationId" in op)


def frontmatter(text):
    """Return the `key: value` pairs of a leading --- block (empty if none)."""
    m = re.match(r"---\n(.*?)\n---\n", text, re.S)
    if not m:
        return {}
    fields = {}
    for line in m.group(1).splitlines():
        key, sep, value = line.partition(":")
        if sep and not line.startswith((" ", "\t")):
            fields[key.strip()] = value.strip().strip('"\'')
    return fields


def cited_files(text):
    """Return the skill-relative files a text cites, in order of first mention:
    relative Markdown link targets and any references/<file>.md it names."""
    cited = []
    for target in re.findall(r"\]\(([^)\s]+)\)", text):
        target = target.split("#", 1)[0]
        if target and not re.match(r"[a-z][a-z0-9+.-]*:", target, re.I):
            cited.append(target)
    cited += re.findall(r"(?<![\w/.:-])references/[\w.-]+\.md", text)
    return list(dict.fromkeys(cited))


def check_skill(skill_dir):
    """Return the problems found in one skill directory, as printable lines."""
    problems = []
    entry_path = os.path.join(skill_dir, ENTRY)
    if not os.path.isfile(entry_path):
        return [f"{ENTRY} does not exist"]
    with open(entry_path, encoding="utf-8") as f:
        entry = f.read()
    fields = frontmatter(entry)
    dirname = os.path.basename(os.path.normpath(skill_dir))
    if not fields.get("name"):
        problems.append(f"{ENTRY}: frontmatter has no name")
    elif fields["name"] != dirname:
        problems.append(f"{ENTRY}: frontmatter name '{fields['name']}' is not the directory name '{dirname}'")
    if not fields.get("description"):
        problems.append(f"{ENTRY}: frontmatter has no description")
    elif len(fields["description"]) > MAX_DESCRIPTION:
        problems.append(f"{ENTRY}: frontmatter description has {len(fields['description'])} characters; "
                        f"Claude Desktop's upload takes at most {MAX_DESCRIPTION}")
    raw = re.search(r"(?m)^description:[ \t]*(.*)$", entry)
    if raw and not raw.group(1).startswith(("'", '"')) and (": " in raw.group(1) or " #" in raw.group(1)):
        # A YAML plain scalar ends at ": " or " #": a real loader rejects or cuts the description.
        problems.append(f"{ENTRY}: frontmatter description holds ': ' or ' #' unquoted; quote it or reword it")
    for rel in cited_files(entry):
        if not os.path.isfile(os.path.join(skill_dir, rel)):
            problems.append(f"{ENTRY}: cites {rel}, which does not exist")
    known = KNOWN_TOOLS | KNOWN_ERROR_CODES | catalog_operation_ids()
    for rel, text in skill_texts(skill_dir):
        for word in dict.fromkeys(SNAKE_WORD.findall(text)):
            if word not in known:
                problems.append(f"{rel}: '{word}' is not a tool name, error code or catalog operation ID")
        folded = " ".join(text.split())
        for rule, pattern in FORBIDDEN_PHRASES:
            for m in pattern.finditer(folded):
                problems.append(f'{rel}: forbidden phrase ({rule}): "{m.group(0)}"')
    return problems


def skill_texts(skill_dir):
    """Yield (relative path, text) for the entry file and every Markdown file
    beside or below it."""
    for dirpath, dirnames, filenames in os.walk(skill_dir):
        dirnames.sort()
        for name in sorted(filenames):
            if name.endswith(".md"):
                path = os.path.join(dirpath, name)
                with open(path, encoding="utf-8") as f:
                    yield os.path.relpath(path, skill_dir).replace(os.sep, "/"), f.read()


def main(argv):
    """Check each skill directory in argv (default: every one under skills/);
    return the exit status."""
    if argv:
        targets = [(d, d) for d in argv]
    else:
        skills = os.path.join(ROOT, "skills")
        targets = [(os.path.join(skills, d), f"skills/{d}") for d in sorted(os.listdir(skills))
                   if os.path.isdir(os.path.join(skills, d))]
    status = 0
    for skill_dir, label in targets:
        problems = check_skill(skill_dir)
        for problem in problems:
            print(f"{label}: {problem}")
        if problems:
            status = 1
        else:
            print(f"OK {label}")
    return status


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
