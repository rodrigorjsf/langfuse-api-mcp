# Research index

Evidence behind the ADRs. Labels: `[verified 3-0]` passed 3-vote adversarial check; `[sourced]` read on a primary source; `[sourced — unverified]` secondary or inferred. `[verified]` = executed by a prototype (code + verbatim output kept on a `prototype/*` branch, linked from the section).

| File | Answers | Feeds |
|---|---|---|
| [stack-and-sdk.md](stack-and-sdk.md) | which language/SDK; MCP spec 2026-07-28; what `mcp-server-dev` skills recommend | ADR-0001, 0002, 0005 |
| [tls-and-corporate-networks.md](tls-and-corporate-networks.md) | why corporate TLS breaks; per-language trust stores; Go 1.27 `SSL_CERT_*` trap; containers | ADR-0006 |
| [langfuse.md](langfuse.md) | auth, regions, org vs project keys, rate limits, pagination, deprecations, glossary, workflows, official MCP limits, spec-vs-docs mismatches | ADR-0004, `CONTEXT.md`, M6 skills |
| [security.md](security.md) | OWASP LLM 2025/2026, Agentic 2026, MCP Top 10 2025, MCP spec security, Anthropic review criteria, supply chain → control mapping | ADR-0003, `.claude/rules/security.md` |
| [engineering-process.md](engineering-process.md) | Matt Pocock skill vocabulary, workflow, doc rules | `.claude/rules/engineering-process.md`, `testing.md` |
| [go-tls-facts.md](go-tls-facts.md) | Go 1.27 status, `SSL_CERT_*` override semantics per OS, Linux default cert paths, fallback roots, go-sdk min Go | ADR-0006 |
| [mcp-hosts-env.md](mcp-hosts-env.md) | how each MCP host (Claude Code/Desktop, Cursor, VS Code, Codex, Gemini, Windsurf, Docker) passes env to a stdio server; which filter it | install docs, config guidance |
| prototype branches | `prototype/tls-trust-pool` (capture-and-unset proof, Linux ×3 + Windows 11) → `go-tls-facts.md` §7; `prototype/langfuse-io-window` (self-hosted 4.46.0 io/metadata window, limits, error shapes) → `langfuse.md` §1.7 | #4/#13, #2/#15, M1 error mapping |
| [2026-09-25-deep-research-run1.md](2026-09-25-deep-research-run1.md) | raw verified findings + all extracted claims of the first deep-research run | all of the above |

## Open questions

- Does **Langfuse Cloud** REST enforce the 14-day window / 50-row cap on io/metadata projections? Self-hosted 4.46.0 does not — `langfuse.md` §1.6–1.7, #15.
- macOS behavior of the `SSL_CERT_*` capture-and-unset design — CI proof in #13.
- Claude Code, Cursor, Windsurf env inheritance is undocumented (`mcp-hosts-env.md`); README tells users to reference variables explicitly.
- Official Langfuse MCP/CLI TLS failure root cause (Node default trust is an unreproduced inference).
