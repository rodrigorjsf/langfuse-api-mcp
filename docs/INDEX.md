# Documentation index

The [README](../README.md) covers what the server does, how to install and configure it, and the security essentials. Start there. This index lists everything else, from the deepest user reference to how the server was built.

## Using the server in depth

| Document | Read it when you want… |
|---|---|
| [Tools reference](reference/tools.md) | every tool in full, how a call is validated, every error code, result limits, the envelope and the audit line |
| [Configuration reference](reference/configuration.md) | every setting's defaults and startup errors, the trust pool and proxy, the deployment profile, each MCP client's environment handling, the config file rules and the startup log lines |
| [Installation reference](reference/installation.md) | what each channel ships and how it is built and tested, the reported version, the npm launcher, the image, the MCPB bundle and the user skill install |
| [Security model](reference/security-model.md) | every security guarantee and how it is enforced, write mode and its confirmation in full, and how to verify a release's signatures and provenance |
| [SECURITY.md](../SECURITY.md) | how to report a vulnerability |

## How it was built

| Document | What it holds |
|---|---|
| [CONTRIBUTING.md](../CONTRIBUTING.md) | local checks, prerequisites, the cross-OS trust proof, the stack and the reading order for contributors |
| [CI checks in depth](development/checks.md) | the union catalog generator, the user skill check and the security mapping check |
| [Release pipeline](development/release-pipeline.md) | snapshot and tag runs, smoke tests per channel, signing and publishing, cutting a release |
| [Integration tests and container stacks](development/integration-tests.md) | the live Langfuse suite, the pinned deployments and the container stack guard |
| [Small-model discovery eval](development/small-model-eval.md) | the eval of tool discovery with a small model, and its recorded runs |
| [ROADMAP.md](../ROADMAP.md) | milestones and what each one delivered |
| [CONTEXT.md](../CONTEXT.md) | the domain glossary |
| [Architecture decisions](adr/) | the ADRs: why things are the way they are |
| [Architecture diagram](architecture/target-architecture.html) | the module layout and data flow (rendered from [its JSON source](architecture/target-architecture.json)) |
| [Research index](research/INDEX.md) | the evidence behind decisions: Langfuse API facts, TLS, MCP hosts, security mapping, dated records |
| [Agent docs](agents/) | the issue tracker, triage labels and domain docs conventions for coding agents |
