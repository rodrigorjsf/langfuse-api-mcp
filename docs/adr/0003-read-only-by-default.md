---
status: accepted
---
# Read-only by default; writes are a server-side opt-in

Tool annotations are hints clients may ignore, so they cannot be the write gate. `execute_write` is **not registered** unless the operator sets `LANGFUSE_MCP_ALLOW_WRITES=true`; with it unset the server is incapable of mutating Langfuse regardless of what a prompt-injected model asks. When enabled, `execute_write` carries `destructiveHint: true`, its description states that it performs changes intended only for operations the user explicitly requested, and — when the client advertises elicitation — destructive operations (DELETE) ask the user to confirm.

## Considered Options

- **Writes on by default, gated only by descriptions/annotations** — the model of the official Langfuse MCP (read-only is left to a client allowlist). Rejected: not a deterministic control (OWASP MCP02 scope creep, LLM06 excessive agency).

## Consequences

- Tension with Anthropic's review criterion that descriptions must not *instruct* the model: descriptions state intent declaratively ("Performs changes. Intended for operations the user explicitly requested.") rather than imperatively.

## Amendment: confirmation is server-enforced and fail-closed (2026-09-27, M4 grilling)

With write mode on, the agent holds untrusted input (Langfuse payloads), sensitive data and state change at once — the [A,B,C] configuration of the LLM01:2026 Rule of Two — so a destructive call needs a human decision per action, and a client-side permission prompt driven by `destructiveHint` is a hint the client may skip. The server therefore enforces it:

- **Destructive** now means DELETE, PUT or PATCH (CONTEXT.md). PUT upserts and PATCH updates overwrite data in place; `promptVersion_update` alone can change which prompt version `production` serves. POST creates are not confirmed by the server and rely on the client's own permission prompt — a known limit: `prompts_create` with the `production` label also changes the served prompt.
- `execute_write` asks for a **confirmation** before sending a destructive call, through elicitation. The confirmation text names the operation, method, path parameters and a summary of the body, all stripped of hidden characters and cut, because the arguments come from the model and may carry injected text.
- **Fail-closed.** When the client does not advertise elicitation, a destructive call is refused with a tool error and never sent; "when the client advertises elicitation" in the original decision no longer holds. A declined or cancelled confirmation is also a tool error with no request to Langfuse. There is no operator switch to skip confirmation (add one only when a real client needs it).
- **Mechanism.** go-sdk v1.8.0 refuses a server-initiated `Elicit` on protocol 2026-07-28 and expects multi round-trip requests: the handler returns `InputRequests` with a `RequestState`, the client re-sends the call with `InputResponses` and that state, and the SDK's middleware turns this into a direct `Elicit` for older clients. The `RequestState` is an expiry plus an HMAC, under a random key generated at process start, over the operation and the canonical arguments; the handler keeps no memory between calls. A re-sent call whose state is missing, forged, expired or computed for other arguments is refused, so an accept cannot be sent ahead of the question or rebound to arguments the user did not see (both passed without the signature in the prototype on branch `prototype/m4-mrtr-confirmation`). Replaying the identical call within the expiry is not prevented: every confirmed method (DELETE, PUT, PATCH) is idempotent, so preventing it would add per-process state for little gain.
- **Capability.** The client's capabilities are read per request; a client that advertises no elicitation, or only URL-mode elicitation (which cannot show a confirm form), cannot confirm, and the call fails closed.
- **One tool, not three.** Anthropic's review criteria prefer splitting writes into create/update/delete tools with their own annotations. `execute_write` stays one tool; the per-operation distinction lives in the server-side confirmation, which does not depend on the client honouring annotations. Recorded as a known deviation, to reassess with the Directory listing (M7).

### Considered options

- **Confirm only DELETE** (the original wording). Rejected: PATCH on a prompt label is as high-impact as a delete.
- **Proceed without confirmation when the client cannot elicit** (the original wording). Rejected: it silently drops the only per-action human check in an [A,B,C] configuration.
- **Three tools** (`execute_create`, `execute_update`, `execute_delete`). Rejected for M4: it renames a surface ADR-0002, the glossary and the README already fix, and the client's permission prompt it would enable is weaker than the server-side confirmation.
