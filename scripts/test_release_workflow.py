#!/usr/bin/env python3
#
# WHAT: offline tests of the tag-only jobs of .github/workflows/release.yml (#126, #154): every job
#       that signs or publishes runs only on a `v*` tag push, and publish-registry (the MCP
#       Registry entry) runs after github-release, holds only `id-token: write` and
#       `contents: read`, cannot fail the release run, and installs mcp-publisher checked against
#       a pinned SHA-256. The pinned installs (#92, #124, #155): every workflow installs the MCPB
#       CLI with `npm ci --ignore-scripts` from its integrity-hashed lockfile, and Python packages
#       only with `pip install --require-hashes` from the hash-pinned scripts/requirements.txt,
#       and every setup-node step takes the one exact NODE_VERSION. Signing (#158): sign-blobs signs
#       every archive, checksums.txt, the .mcpb and the skill ZIP with a pinned cosign and attests
#       the same files; publish-image signs the image and its platform manifests by digest and
#       attests it by digest. npm trusted publishing (#186): publish-npm meets what npm's
#       documentation (https://docs.npmjs.com/trusted-publishers) asks of a GitHub Actions
#       publisher, and no packaging input holds a hand-written release version.
# WHY:  a publish job reachable from a pull request or a branch would publish from an unreviewed
#       ref (security gate, dangerous parameters); a Registry failure (preview) must never fail a
#       release; an unpinned mcp-publisher would be an unchecked supply-chain dependency.
# WHEN: on every push and pull request (.github/workflows/ci.yml, job "generator"), and by hand
#       after changing release.yml.
# HOW:  python3 scripts/test_release_workflow.py   (Python 3.11+ with PyYAML from
#       scripts/requirements.txt)

import json
import os
import re
import sys
import unittest

import yaml

sys.dont_write_bytecode = True

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
WORKFLOWS = os.path.join(ROOT, ".github", "workflows")
WORKFLOW = os.path.join(WORKFLOWS, "release.yml")
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


def step_using(job, action):
    """Return the one step of job that uses action (`owner/name`, pinned by commit)."""
    steps = [s for s in job["steps"] if s.get("uses", "").startswith(action + "@")]
    if len(steps) != 1:
        raise AssertionError(f"want one {action} step, got {len(steps)}")
    return steps[0]


class Signing(unittest.TestCase):
    """What a user verifies (docs/reference/security-model.md "Verify what you run"): cosign keyless signatures and build
    provenance for every archive, checksums.txt, the .mcpb and the skill ZIP, and for the image by
    digest. The live proof against v0.1.0 is docs/research/raw/2026-09-30-v0.1.0-release-proofs.md."""

    SIGNED_BLOBS = ["*.tar.gz", "*.zip", "checksums.txt", "*.mcpb"]

    def setUp(self):
        self.wf = load()
        self.jobs = self.wf["jobs"]

    def test_cosign_is_pinned_and_installed_by_sigstores_installer_in_every_signing_job(self):
        self.assertRegex(self.wf["env"]["COSIGN_VERSION"], r"^v\d+\.\d+\.\d+$")
        for name in ["sign-blobs", "publish-image"]:
            with self.subTest(job=name):
                step = step_using(self.jobs[name], "sigstore/cosign-installer")
                self.assertEqual(step["with"]["cosign-release"], "${{ env.COSIGN_VERSION }}")

    def test_sign_blobs_signs_every_archive_the_checksums_the_bundle_and_the_skill_zip(self):
        run = "\n".join(s.get("run", "") for s in self.jobs["sign-blobs"]["steps"])
        loop = re.search(r"^\s*for f in (.+); do$", run, re.M)
        self.assertIsNotNone(loop)
        self.assertEqual(loop.group(1).split(), self.SIGNED_BLOBS)
        self.assertIn('cosign sign-blob --yes --bundle "$f.sigstore.json" "$f"', run)

    def test_sign_blobs_attests_build_provenance_for_the_same_files(self):
        attest = step_using(self.jobs["sign-blobs"], "actions/attest")
        paths = attest["with"]["subject-path"].split()
        self.assertEqual(paths, ["release/" + g for g in self.SIGNED_BLOBS])

    def test_publish_image_signs_the_image_and_every_platform_manifest_by_digest(self):
        runs = [s.get("run", "") for s in self.jobs["publish-image"]["steps"]]
        self.assertIn('cosign sign --yes --recursive "$IMAGE@$DIGEST"', runs)
        sign = next(s for s in self.jobs["publish-image"]["steps"] if "cosign sign" in s.get("run", ""))
        self.assertEqual(sign["env"]["DIGEST"], "${{ steps.push.outputs.digest }}")

    def test_publish_image_attests_build_provenance_for_the_image_by_digest(self):
        attest = step_using(self.jobs["publish-image"], "actions/attest")
        self.assertEqual(attest["with"]["subject-name"], "${{ env.IMAGE }}")
        self.assertEqual(attest["with"]["subject-digest"], "${{ steps.push.outputs.digest }}")
        self.assertIs(attest["with"]["push-to-registry"], True)


def version_tuple(text):
    return tuple(int(part) for part in text.split("."))


class NpmTrustedPublishing(unittest.TestCase):
    """publish-npm authenticates through npm trusted publishing (OIDC), the first release to do so
    being v0.2.0. What npm asks of a GitHub Actions publisher (docs.npmjs.com/trusted-publishers):
    npm CLI 11.5.1 or later and Node 22.14.0 or later, the permission `id-token: write`, a
    GitHub-hosted runner, a workflow file named exactly as the publisher names it, and the
    package.json `repository.url` matching the repository. The settings on npmjs.com are not
    visible here: the wizard (scripts/release-wizard.sh, stage 5) has the maintainer look at them."""

    def setUp(self):
        self.wf = load()
        self.job = self.wf["jobs"]["publish-npm"]

    def test_the_job_asks_for_the_oidc_token_and_nothing_else(self):
        self.assertEqual(self.job["permissions"], {"id-token": "write"})

    def test_it_runs_on_a_github_hosted_runner_in_the_release_environment(self):
        self.assertEqual(self.job["runs-on"], "ubuntu-latest")
        self.assertEqual(self.job["environment"], "release")

    def test_the_workflow_file_is_named_the_way_the_npm_publishers_name_it(self):
        # The publisher names the file, with its extension, case-sensitively.
        self.assertEqual(os.path.basename(WORKFLOW), "release.yml")

    def test_the_pinned_node_is_new_enough_and_ships_an_npm_with_trusted_publishing(self):
        # Node 22.14.0 is npm's floor for the feature; the npm that Node ships must be 11.5.1 or
        # later, which Node 24.6 and later do (v24.21.0 ships npm 11.19.0, nodejs.org/dist/index.json).
        node = version_tuple(self.wf["env"]["NODE_VERSION"])
        self.assertGreaterEqual(node, (22, 14, 0))
        self.assertGreaterEqual(node, (24, 6, 0))
        step = step_using(self.job, "actions/setup-node")
        self.assertEqual(step["with"]["node-version"], "${{ env.NODE_VERSION }}")
        self.assertEqual(step["with"]["registry-url"], "https://registry.npmjs.org")

    def test_the_publish_step_reads_the_fallback_token_from_the_release_environment_secret_only(self):
        # Empty while the secret does not exist, and then npm 11.19.0 exchanges the OIDC token
        # before it looks at any configured one. The line is what makes the wizard's token
        # fallback (stage 8) work after the tag: a re-run of the job uses the workflow as tagged.
        publish = next(s for s in self.job["steps"] if "npm publish" in s.get("run", ""))
        self.assertEqual(publish["env"]["NODE_AUTH_TOKEN"], "${{ secrets.NPM_TOKEN }}")
        for wf, job, step in workflow_steps():
            if "NPM_TOKEN" in json.dumps(step):
                self.assertEqual((wf, job), ("release.yml", "publish-npm"))

    def test_it_publishes_publicly_with_provenance_and_skips_a_version_already_there(self):
        run = "\n".join(s.get("run", "") for s in self.job["steps"])
        self.assertIn("npm publish --access public --provenance", run)
        self.assertIn('npm view "$name@$VERSION" version', run)

    def test_every_package_names_the_repository_npm_checks(self):
        with open(os.path.join(ROOT, "packaging", "npm", "pack.mjs"), encoding="utf-8") as f:
            self.assertIn("git+https://github.com/rodrigorjsf/langfuse-api-mcp.git", f.read())


class PackagingHoldsNoReleaseVersion(unittest.TestCase):
    """Every channel takes its version from the tag (RELEASE_VERSION in .goreleaser.yaml, then the
    MCPB manifest, the Registry entry and the npm packages are rendered with it), so no packaging
    input carries a hand-written release version: only the placeholder."""

    PLACEHOLDER = "0.0.0-dev"

    def test_the_registry_entry_template_carries_the_placeholder_everywhere(self):
        with open(os.path.join(ROOT, "packaging", "server.json"), encoding="utf-8") as f:
            entry = json.load(f)
        self.assertEqual(entry["version"], self.PLACEHOLDER)
        self.assertGreater(len(entry["packages"]), 0)
        # The npm package names its version, the image its tag; every dotted number in the file
        # is the placeholder's 0.0.0, so no release version was typed in.
        self.assertEqual(entry["packages"][0]["version"], self.PLACEHOLDER)
        self.assertTrue(entry["packages"][1]["identifier"].endswith(":" + self.PLACEHOLDER))
        self.assertEqual(set(re.findall(r"\d+\.\d+\.\d+", json.dumps(entry))), {"0.0.0"})

    def test_the_mcpb_manifest_carries_the_placeholder(self):
        with open(os.path.join(ROOT, "packaging", "mcpb", "manifest.json"), encoding="utf-8") as f:
            self.assertEqual(json.load(f)["version"], self.PLACEHOLDER)

    def test_goreleaser_takes_the_release_version_from_the_environment(self):
        with open(os.path.join(ROOT, "packaging", ".goreleaser.yaml"), encoding="utf-8") as f:
            config = yaml.safe_load(f)
        self.assertIn('envOrDefault "RELEASE_VERSION"', config["snapshot"]["version_template"])
        self.assertEqual(config["version"], 2)  # GoReleaser's schema version, not the release's


def workflow_steps():
    """Yield (workflow file, job, step) for every step of every workflow."""
    for name in sorted(os.listdir(WORKFLOWS)):
        if name.endswith((".yml", ".yaml")):
            with open(os.path.join(WORKFLOWS, name), encoding="utf-8") as f:
                wf = yaml.safe_load(f)
            for job_name, job in wf["jobs"].items():
                for step in job.get("steps", []):
                    yield name, job_name, step


class PinnedInstalls(unittest.TestCase):
    """Supply chain (security.md): no workflow installs a dependency its lockfile does not pin."""

    def test_every_npm_install_is_npm_ci_without_install_scripts(self):
        seen = 0
        for wf, job, step in workflow_steps():
            for line in step.get("run", "").splitlines():
                if re.search(r"\bnpm (?:ci|install|i)\b", line):
                    seen += 1
                    with self.subTest(workflow=wf, job=job, line=line.strip()):
                        self.assertRegex(line, r"\bnpm ci --ignore-scripts\b")
        self.assertGreater(seen, 0)

    def test_the_mcpb_cli_lockfile_pins_every_package_with_an_integrity_hash(self):
        with open(os.path.join(ROOT, "packaging", "mcpb", "package-lock.json"), encoding="utf-8") as f:
            packages = json.load(f)["packages"]
        deps = {path: meta for path, meta in packages.items() if path}
        self.assertGreater(len(deps), 0)
        for path, meta in deps.items():
            with self.subTest(package=path):
                self.assertRegex(meta.get("integrity", ""), r"^sha512-")

    def test_node_is_pinned_to_an_exact_version_everywhere_it_is_set_up(self):
        # The npx channel depends on Node (#123): every setup-node step takes the one pinned version.
        self.assertRegex(load()["env"]["NODE_VERSION"], r"^\d+\.\d+\.\d+$")
        seen = 0
        for wf, job, step in workflow_steps():
            if step.get("uses", "").startswith("actions/setup-node@"):
                seen += 1
                with self.subTest(workflow=wf, job=job):
                    self.assertEqual(step["with"]["node-version"], "${{ env.NODE_VERSION }}")
        self.assertGreater(seen, 0)

    def test_every_pip_install_requires_hashes_from_the_pinned_requirements(self):
        seen = 0
        for wf, job, step in workflow_steps():
            for line in step.get("run", "").splitlines():
                if re.search(r"\bpip3?\"? install\b", line):
                    seen += 1
                    with self.subTest(workflow=wf, job=job, line=line.strip()):
                        self.assertIn("--require-hashes -r scripts/requirements.txt", line)
        self.assertGreater(seen, 0)
        with open(os.path.join(ROOT, "scripts", "requirements.txt"), encoding="utf-8") as f:
            reqs = [r for r in re.split(r"(?<!\\)\n", f.read()) if r.strip() and not r.startswith("#")]
        for req in reqs:
            with self.subTest(requirement=req.split()[0]):
                self.assertRegex(req, r"==\S+")
                self.assertIn("--hash=sha256:", req)


if __name__ == "__main__":
    unittest.main(verbosity=2)
