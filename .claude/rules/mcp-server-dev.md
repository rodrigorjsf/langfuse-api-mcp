# MCP development: use the official skills

Whenever you design, add, or change anything MCP-facing (tools, schemas, annotations, transports, elicitation, packaging), **load the official `mcp-server-dev` skills first** and follow them unless an ADR records a deliberate deviation:

| Task | Skill / reference |
|---|---|
| tool surface, descriptions, schemas, errors, annotations | `mcp-server-dev:build-mcp-server` → `references/tool-design.md` |
| confirmations for destructive calls | `build-mcp-server` → `references/elicitation.md` |
| server capabilities, resources, prompts | `build-mcp-server` → `references/server-capabilities.md`, `resources-and-prompts.md` |
| MCPB packaging (`"type": "binary"`), local-server security checklist | `mcp-server-dev:build-mcpb` → `references/manifest-schema.md`, `local-security.md` |

Recorded deviations: Go instead of the TS default (ADR-0001); local-first instead of remote HTTP (ADR-0005). Spec reference: MCP 2026-07-28 (stateless).
