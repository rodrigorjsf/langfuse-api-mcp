# Minimal container image (spec #119): stdio only, non-root, no shell. GoReleaser builds it from
# packaging/.goreleaser.yaml (dockers_v2) with the release binaries it already built; nothing is
# compiled here. The build context holds <os>/<arch>/langfuse-mcp for each platform.
# Base: distroless static-debian13 (#155): CA bundle, /etc/passwd with the nonroot user, no shell,
# no package manager; pinned by the digest of its multi-arch index; Dependabot (docker, /packaging)
# bumps it.
# The image adds no trust of its own: the trust pool is the base's CA bundle plus the CA file a user
# mounts read-only and names with LANGFUSE_CA_CERT (README "Install and run", #28).
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/langfuse-mcp /usr/local/bin/langfuse-mcp
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/langfuse-mcp"]
