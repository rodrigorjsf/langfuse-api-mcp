# Minimal container image (spec #119): stdio only, non-root, no shell. GoReleaser builds it from
# packaging/.goreleaser.yaml (dockers_v2) with the release binaries it already built; nothing is
# compiled here. The build context holds <os>/<arch>/langfuse-mcp for each platform.
# Base: distroless static-debian13 (#155): CA bundle, /etc/passwd with the nonroot user, no shell,
# no package manager; pinned by the digest of its multi-arch index; Dependabot (docker, /packaging)
# bumps it.
# The image adds no trust of its own: the trust pool is the base's CA bundle plus the CA file a user
# mounts read-only and names with LANGFUSE_CA_CERT (README "Install and run", #28).
FROM gcr.io/distroless/static-debian13:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/langfuse-mcp /usr/local/bin/langfuse-mcp
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/langfuse-mcp"]
