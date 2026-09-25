---
status: accepted
---
# Read-only by default; writes are a server-side opt-in

Tool annotations are hints clients may ignore, so they cannot be the write gate. `execute_write` is **not registered** unless the operator sets `LANGFUSE_MCP_ALLOW_WRITES=true`; with it unset the server is incapable of mutating Langfuse regardless of what a prompt-injected model asks. When enabled, `execute_write` carries `destructiveHint: true`, its description states that it performs changes intended only for operations the user explicitly requested, and — when the client advertises elicitation — destructive operations (DELETE) ask the user to confirm.

## Considered Options

- **Writes on by default, gated only by descriptions/annotations** — the model of the official Langfuse MCP (read-only is left to a client allowlist). Rejected: not a deterministic control (OWASP MCP02 scope creep, LLM06 excessive agency).

## Consequences

- Tension with Anthropic's review criterion that descriptions must not *instruct* the model: descriptions state intent declaratively ("Performs changes. Intended for operations the user explicitly requested.") rather than imperatively.
