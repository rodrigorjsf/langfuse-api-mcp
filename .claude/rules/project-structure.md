# Project structure — where every file goes

Binding layout (ADR-0009). Before creating **any** file, find its domain in the placement table and put it in that directory. If nothing fits, stop and ask — do not invent a directory. A new top-level directory or `internal/` package requires a new ADR, an updated `archify` diagram (`docs/architecture/`) and an update of this file, in the same commit.

Packages are organized **by domain concept** (terms from `CONTEXT.md`), never by technical layer: no `models/`, `handlers/`, `services/`, `utils/`, `common/`, `helpers/`, `types/`, `pkg/`.

```text
.
├── cmd/langfuse-mcp/        main.go only: parse flags → config.Load → build modules → run transport
├── internal/
│   ├── config/              Config: env/flag parsing, validation, defaults, region presets, Secret type
│   ├── trust/               Trust pool: OS store + explicit + ambient CA sources → *x509.CertPool
│   ├── langfuse/            Langfuse HTTP client: Basic auth, transport, proxy, redirects, rate limit,
│   │                        retries, response caps, APIError; knows HTTP, not MCP
│   ├── catalog/             Catalog: operations from the embedded spec, ADR-0004 exclusions, search,
│   │   └── spec/            param validation; spec/ holds the go:embed'ed langfuse-openapi.json
│   ├── sanitize/            Untrusted-data envelope, invisible/bidi Unicode + control-char stripping,
│   │                        size truncation; pure functions
│   ├── workflows/           Workflow tools' logic, one file per workflow (trace.go, errors.go, cost.go…);
│   │                        uses langfuse + sanitize; knows nothing about MCP registration
│   ├── server/              MCP surface: go-sdk server, tool registration + annotations, write-mode gate,
│   │                        search/execute handlers, the ONLY translation of errors into tool errors (ADR-0008)
│   └── transport/           stdio start-up; loopback Streamable HTTP: bind, bearer token, Origin/Host checks
├── skills/<skill-name>/     M6 user-facing skills (SKILL.md + references), authored with /writing-great-skills
├── packaging/               Dockerfile, .goreleaser.yaml inputs, mcpb/manifest.json
├── scripts/                 repeatable maintainer procedures (WHAT/WHY/WHEN/HOW header)
├── docs/                    adr/, architecture/, research/, agents/ — no code
└── .claude/                 rules/, hooks/, settings.json
```

Go tooling files that must sit at the root stay there: `go.mod`, `go.sum`, `.golangci.yml`, `.gitattributes`, `.gitignore`, `LICENSE`, `README.md`, `CONTEXT.md`, `ROADMAP.md`, `CLAUDE.md`.

## Placement table — "the logic is about…"

| Logic is about… | Directory | Not here |
|---|---|---|
| reading/validating an env var or flag, defaults, region names | `internal/config` | `cmd/`, `langfuse/` |
| loading/parsing certificates, CA files/dirs, ambient CA variables | `internal/trust` | `config/` (config only carries paths) |
| HTTP to Langfuse: headers, auth, retry, backoff, 429, body limits, status → `APIError` | `internal/langfuse` | `server/` |
| which operations exist, exclusions, operation search, operation-param schema checks | `internal/catalog` | `server/` |
| cleaning or wrapping Langfuse payloads before the agent sees them | `internal/sanitize` | inline in handlers |
| a multi-call read flow (trace tree, error triage, cost/latency spike) | `internal/workflows/<flow>.go` | `server/` (server only registers it) |
| tool names, descriptions, annotations, input/output schemas, `isError` shape, write gate | `internal/server` | anywhere else |
| listening, tokens, Origin/Host validation, stdio setup | `internal/transport` | `server/` |
| process wiring and exit codes | `cmd/langfuse-mcp` | — |
| test fixtures / golden files | `<package>/testdata/` | a shared top-level fixtures dir |
| integration tests against a real Langfuse (`//go:build integration`) | `<package>/*_integration_test.go` | separate test tree |
| a user skill | `skills/<name>/` | `docs/` |

## Dependency direction (no cycles, enforced by `depguard` in `.golangci.yml` from M1)

```text
cmd → transport → server → workflows → langfuse → trust
                         ↘ catalog     ↘ sanitize
config is imported only by cmd; every other package receives plain values/structs (accept dependencies, don't create them).
```

- Lower packages never import higher ones (`langfuse` never imports `server`; `catalog` imports nothing internal).
- `server` is the only package importing the MCP SDK besides `transport`.
- Tests live beside the code (`foo_test.go`, package `foo_test` for interface-level tests).
