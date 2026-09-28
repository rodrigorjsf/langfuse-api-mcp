# Security policy

## Reporting a vulnerability

Report a vulnerability through GitHub private vulnerability reporting:
[Report a vulnerability](https://github.com/rodrigorjsf/langfuse-api-mcp/security/advisories/new)
(the repository's **Security** tab, then **Report a vulnerability**).

**Never open a public issue, pull request or discussion for a vulnerability.** A public report
discloses the problem before a fix exists.

Include what you can:

- the version (`initialize` reports it, and so does the startup log line);
- the install channel (release archive, container image, npm, MCPB, `go install`);
- the steps or the input that reproduce the problem, and what an attacker gains.

Never include real Langfuse keys, tokens or trace data. Redact them, or use a throwaway project.

The report stays private between you and the maintainer until a fix is released. You are credited in
the advisory unless you ask not to be.

## Supported versions

The project is on semantic versioning `0.x`: there is no stability promise yet. Only the latest
release of the `0.x` line gets security fixes. A fix ships as a new release; older releases are not
patched.

| Version | Supported |
|---|---|
| latest `0.x` release | yes |
| any older release | no |
| unreleased builds (`0.0.0-dev`) | no; reproduce on the latest release |

## Scope

The server's security model (credentials, the write gate, request and result hardening, transport)
is described in the README "Security model" section. A way around any control listed there is in
scope. A problem in Langfuse itself goes to [Langfuse](https://github.com/langfuse/langfuse/security).
