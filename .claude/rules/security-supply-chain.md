---
paths:
  - ".github/**"
  - "packaging/**"
  - "scripts/**"
  - "go.mod"
  - "go.sum"
---
# Security: supply chain

Part of the non-negotiable security rules (`security.md`).

**Supply chain**: `govulncheck` in CI; pinned `go.sum`; the generator's PyYAML pinned with hashes in `scripts/requirements.txt`, installed with `--require-hashes` by both workflows and updated by Dependabot (#92); the MCPB CLI pinned with integrity hashes in `packaging/mcpb/package-lock.json`, installed with `npm ci --ignore-scripts`, updated by Dependabot (#124); base image pinned by digest; the npm packages carry no install script and no dependency beyond their own platform packages, and Node (pinned in `release.yml`) and the npm registry are dependencies of the npx channel only (#123); SBOM for archives *and* image (the archive SBOMs are covered by the signed `checksums.txt`; the image SBOM is published but not yet signed or attested, #165); cosign keyless signing and `actions/attest` provenance of the archives, checksums file, `.mcpb`, user skill ZIP (#139) and image (by digest), the GHCR push, `npm publish` and the MCP Registry publish (`publish-registry`, #154: after `github-release`, only `id-token: write` and `contents: read`, `continue-on-error` so the preview Registry never fails a release, `mcp-publisher` pinned by version and SHA-256, `packaging/server.json` static text rendered and validated offline against the published schema by `scripts/registry-server-json`, never built from Langfuse data; tests prove it) run **only on a `v*` release tag** (#126): each is a tag-gated job of `release.yml` with only the permissions it needs (`id-token`/`attestations`/`packages: write` nowhere else), the publish jobs run in the `release` environment, restricted to `v*` tags and backed by a `v*` tag ruleset, and `publish-npm` authenticates through npm trusted publishing (OIDC); no npm token is stored anywhere (the `v0.1.0` token was deleted and revoked; `docs/development/release-pipeline.md` "Cutting a release"); a snapshot run (pull request, `main`, weekly) skips them; no new dependency without justification.
