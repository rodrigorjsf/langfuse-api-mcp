#!/usr/bin/env python3
#
# WHAT: offline tests of the tag-only jobs of .github/workflows/release.yml (#126, #154): every job
#       that signs or publishes runs only on a `v*` tag push, and publish-registry (the MCP
#       Registry entry) runs after github-release, holds only `id-token: write` and
#       `contents: read`, cannot fail the release run, and installs mcp-publisher checked against
#       a pinned SHA-256.
# WHY:  a publish job reachable from a pull request or a branch would publish from an unreviewed
#       ref (security gate, dangerous parameters); a Registry failure (preview) must never fail a
#       release; an unpinned mcp-publisher would be an unchecked supply-chain dependency.
# WHEN: on every push and pull request (.github/workflows/ci.yml, job "generator"), and by hand
#       after changing release.yml.
# HOW:  python3 scripts/test_release_workflow.py   (Python 3.11+ with PyYAML from
#       scripts/requirements.txt)

import os
import re
import sys
import unittest

import yaml

sys.dont_write_bytecode = True

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
WORKFLOW = os.path.join(ROOT, ".github", "workflows", "release.yml")
TAG_ONLY = "github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')"
TAG_JOBS = ["sign-blobs", "publish-image", "publish-npm", "github-release", "publish-registry"]


def load():
    with open(WORKFLOW, encoding="utf-8") as f:
        return yaml.safe_load(f)


class TagOnlyJobs(unittest.TestCase):
    def setUp(self):
        self.wf = load()
        self.jobs = self.wf["jobs"]

    def test_every_signing_or_publishing_job_runs_only_on_a_v_tag_push(self):
        for name in TAG_JOBS:
            with self.subTest(job=name):
                self.assertEqual(self.jobs[name].get("if"), TAG_ONLY)

    def test_publish_registry_runs_after_the_github_release(self):
        self.assertIn("github-release", self.jobs["publish-registry"]["needs"])

    def test_publish_registry_holds_only_id_token_write_and_contents_read(self):
        self.assertEqual(self.jobs["publish-registry"]["permissions"],
                         {"id-token": "write", "contents": "read"})

    def test_every_publish_job_runs_in_the_release_environment(self):
        for name in ["publish-image", "publish-npm", "publish-registry"]:
            with self.subTest(job=name):
                self.assertEqual(self.jobs[name].get("environment"), "release")

    def test_a_registry_failure_does_not_fail_the_release_run(self):
        self.assertIs(self.jobs["publish-registry"].get("continue-on-error"), True)

    def test_publish_registry_logs_in_with_github_oidc_and_publishes(self):
        run = "\n".join(s.get("run", "") for s in self.jobs["publish-registry"]["steps"])
        self.assertRegex(run, r'mcp-publisher" login github-oidc\n')
        self.assertRegex(run, r'mcp-publisher" publish ')

    def test_mcp_publisher_is_pinned_with_a_checksum(self):
        env = self.wf["env"]
        self.assertRegex(env["MCP_PUBLISHER_VERSION"], r"^v\d+\.\d+\.\d+$")
        self.assertRegex(env["MCP_PUBLISHER_SHA256"], r"^[0-9a-f]{64}$")
        run = "\n".join(s.get("run", "") for s in self.jobs["publish-registry"]["steps"])
        self.assertIn('echo "$MCP_PUBLISHER_SHA256  $archive" | sha256sum -c -', run)
        # Checked before it is extracted, and extracted before it runs.
        self.assertLess(run.index("sha256sum -c"), run.index("tar -xzf"))
        self.assertLess(run.index("tar -xzf"), run.index("login github-oidc"))
        self.assertIsNone(re.search(r"releases/latest", run))


if __name__ == "__main__":
    unittest.main(verbosity=2)
