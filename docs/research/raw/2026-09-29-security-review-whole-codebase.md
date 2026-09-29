# Whole-codebase security review before v0.1.0 (#156)

Date: 2026-09-29 (UTC). Ticket: #156 (spec #150, T10). Reviewed commit: `883d880` on the spec branch. At that
commit tickets #145, #127, #62, #94, #154 (MCP Registry) and #155 (security mapping) are all merged.
Their handoff commits are `c62bbc3`, `3d9656a`, `7a00b86`, `9e92131`, `0c2c618` and `883d880`.

## Method

The built-in `/security-review` command reviews a diff only: it runs `git diff origin/HEAD...` and
failed here with `fatal: ambiguous argument 'origin/HEAD...'`. So its method was applied to the whole
tree instead of a diff. The method is to report only concrete, exploitable issues or breaches of a
documented control with confidence over 80%, each with file:line, an exploit scenario and a fix.

Five independent reviewers each read one area, read-only:

| Area | Scope |
|---|---|
| Startup | `cmd/langfuse-mcp`, `internal/config`, `internal/trust`, `internal/transport` |
| Langfuse client | `internal/langfuse` |
| Catalog and sanitizer | `internal/catalog`, `internal/sanitize` |
| MCP surface | `internal/server`, `internal/workflows` |
| Supply chain | `.github/`, `packaging/`, `scripts/`, `skills/`, `go.mod`, tracked files scanned for secrets and personal paths |

Each reviewer checked the code against `.claude/rules/security.md` and the risk-to-control mapping in
`docs/research/security.md`.

## Findings

Each finding was fixed on the #156 branch, with a test at the seam named.

| # | Severity | Control (OWASP ID with year) | Finding | Fix | Test (seam) |
|---|---|---|---|---|---|
| F1 | LOW | LLM02:2025 / MCP08:2025 / MCP10:2025 (audit metadata only); LLM01:2026 / MCP06:2025 (cursor never in the audit line, #93) | When a request got no answer (DNS, refused or reset connection, timeout, refused redirect), the audit line's `cause` held Go's `*url.Error` text. That text includes the full request URL, so the caller's path and query values reached stderr: a trace ID, a `filter`, or the continuation cursor that `get_trace_tree` follows. For a refused redirect it also held the target Langfuse chose. | `langfuse.Client.send` drops the URL from an `*url.Error` and keeps its method and wrapped cause, so the failure classes still match (`withoutURL`). | `TestTheAuditLineOfAFailedRequestHoldsNoRequestOrRedirectURL` (server, in-memory client) |
| F2 | LOW | MCP01:2025 (keys never in the config file, ADR-0011) | The config-file key refusal stripped `export ` only when a single space followed `export`. `export<TAB>LANGFUSE_SECRET_KEY=…` was logged as an unknown key and startup went on, so the key stayed in the plaintext file with no error. | Strip `export` followed by any blanks, the way a shell reads it. | `TestLoadRefusesLangfuseKeysInTheConfigFile` cases "export and a tab" and "export and a blank run"; `FuzzLoadConfigFile` seed (`config.Load`) |
| F3 | LOW | LLM01:2025 / LLM01:2026 (invisible-character smuggling); MCP06:2025 | The hidden-character stripping kept variation selectors (U+FE00–FE0F, U+E0100–E01EF) and the other default-ignorable code points (Hangul fillers U+115F, U+3164, U+FFA0, and the combining grapheme joiner U+034F). The 256 variation selectors can encode any byte invisibly after a visible character ("variation-selector smuggling"), so text written into Langfuse could carry an instruction a human reviewer never sees. The catalog's own copy of the check had the same gap. | `hidden()` in `internal/sanitize` and `internal/catalog` also drops `unicode.Variation_Selector` and `unicode.Other_Default_Ignorable_Code_Point`. Cost: an emoji loses its VS16 presentation selector, which is cosmetic. | `TestPayloadDropsVariationSelectorsAndInvisibleFillers` (`sanitize.Payload`); `TestMessageDropsVariationSelectorsAndInvisibleFillers` (`sanitize.Message`); `TestSearchOperationsRefusesAnInvalidQueryWithoutEchoingIt` cases for a variation selector and a Hangul filler (server, the `plainText` refusal of `query`, `operationId` and `traceId` now covers them); `FuzzPayload` and `FuzzMessage` invariants widened; `TestAnIndexLineIsTheSummaryCleanedOfHiddenCharactersLinksBoldAndExcess` (catalog load) |
| F4 | LOW (docs) | MCP07:2025 (HTTP mode) | The README "Security model" row "HTTP mode is local-only" described the bearer token and the `Origin`/`Host` checks as shipped. The HTTP transport does not exist: `internal/transport` is stdio only. | The row is marked **Planned** and links #160 (milestone Later). | none needed: a docs change. The HTTP line of `.claude/rules/security.md` "Transport" is marked Planned too. |
| F5 | LOW (docs) | "Controls are cited with OWASP IDs and the year" | Mapping row "LLM10:2025 / LLM06:2026 / ASI02 (budgeting)" cited ASI02 with no year. | Changed to `ASI02:2026`. | none needed: a docs change |

No HIGH or MEDIUM finding was reported in any area.

## Reported but below the bar (no change)

- **Base URL with `user:pass@`.** `LANGFUSE_BASE_URL` accepts userinfo. It is never used for auth,
  because `SetBasicAuth` overrides it. It is never logged in full: the host is rendered with
  `url.Redacted()`, and since F1 no request URL reaches the audit line. Optional hardening, with no
  concrete impact.
- **Invalid values echoed on stderr.** An invalid environment value of `LANGFUSE_MCP_IGNORE_AMBIENT_CA`
  or of the rate-limit and concurrency variables is echoed quoted on stderr. No documented control
  forbids this: the rule covers only the proxy and write-mode values, and config-file values are
  never echoed. These variables hold no secret.
- **Redaction next to `_` or a digit.** The key pattern `\b[ps]k-lf-` does not match a key directly
  after `_` or a digit. This is a deliberate, tested choice, and the configured key pair is still
  redacted by exact match.
- **Release environment and tag ruleset.** The `release` environment's restriction to `v*` tags and
  the `v*` tag ruleset are documented as Planned. Until the maintainer creates them, anyone with
  write access who pushes a `vX.Y.Z` tag reaches the publish jobs. This is the normal trust boundary
  for write access. It is part of the release ticket's README "Cutting a release" steps (#158, T13).

## Controls verified to hold (summary)

- **Redirects.** A redirect is refused on any change of scheme, host or port.
- **Writes and retries.** A write is never retried. Every request passes through one rate limit and
  one concurrency cap, retries and detection probes included.
- **Size caps.** Response bytes are capped after decompression. The error body and the drain are
  capped too.
- **Proxy.** The proxy CONNECT reply is dropped.
- **Keys.** `KeyPair` renders as `[REDACTED]` in fmt, JSON and slog. The `/health` version reaches
  text only through `KnownVersion`.
- **Parameters.**
  - Path parameters refuse traversal, `/`, `\` and URLs.
  - The Folder-name allow-list matches the rules and fails closed.
  - Unknown parameters are refused and echoed only redacted and cut.
  - Numbers: NaN and Inf are refused, and integers are bounded to ±2^53.
  - The metrics query guard holds: size, depth, keys, `row_limit` and re-encoding.
  - The write body caps and schema errors never echo a value or a caller key.
  - The 17 exclusions cannot be reached.
- **Write gate and confirmation.**
  - The write gate is fixed at startup.
  - The HMAC confirmation state binds the operation ID, the canonical path, the sorted query, the
    sorted-key body and the expiry. The key is 32 `crypto/rand` bytes, and states are compared with
    `hmac.Equal`.
  - Confirmation fails closed without form elicitation. The confirmed request is the one sent.
- **Text and payloads.**
  - Confirmation text is stripped, JSON-quoted and redacted.
  - Tool names and descriptions are static.
  - `get_trace_tree` checks its arguments, and a cursor stays inside the envelope.
- **Startup.**
  - `SSL_CERT_*` is captured and removed first.
  - TLS 1.2 is the minimum, and no skip-verify option exists.
  - The proxy value is validated and never echoed.
  - Windows environment names are canonicalized without shadowing.
  - The build-info version is limited to release tags.
  - The stdio handshake cannot change the tool set.
- **Supply chain.**
  - Every action is pinned by SHA, and no untrusted `${{ }}` is interpolated in `run:`.
  - `id-token`, `attestations` and `packages: write` appear only in the tag-gated jobs.
  - `persist-credentials: false` is set except on the two bot jobs that push.
  - pip is installed with `--require-hashes`. The MCPB CLI is installed with `npm ci --ignore-scripts`
    from a lockfile with integrity hashes.
  - The `mcp-publisher` download is checked against a SHA-256, and the base image is pinned by digest.
  - The npm shim spawns with `shell: false`.
  - The MCPB manifest marks both keys `sensitive`.
- **Tracked files.** They hold no secret, private key or `/home/<user>` path. The only `sk-lf-`/`pk-lf-`
  literals are marked test fakes.

## Planned rows of the mapping: checked by hand against GitHub

On 2026-09-29 (UTC), with `gh api repos/rodrigorjsf/langfuse-api-mcp/issues/<n>`:

| Issue | State | Milestone | Rows |
|---|---|---|---|
| #150 | open | M7 First public release | MCP03:2025 / ASI04:2026 (signing), MCP04:2025 / LLM03:2025 / LLM04:2026 (signing and provenance), MCP09:2025 (published verification) |
| #160 | open | Later | MCP07:2025 / MCP01:2025 (HTTP transport) |
| #161 | open | Later | MCP04:2025 / LLM04:2026 (#123, npm install scripts) |

Every `planned` row links an open issue that carries a milestone.
