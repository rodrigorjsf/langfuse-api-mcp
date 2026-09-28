# langfuse-api-mcp — glossary

## Server surface

**Operation**: one method+path pair of the Langfuse public API, identified by its OpenAPI `operationId`.
_Avoid_: endpoint (when you mean a single method), route, action

**Catalog**: the set of in-scope operations the server can execute for the connected deployment: the bundled union of Langfuse release specs, minus the exclusion list, filtered by the deployment profile.
_Avoid_: registry, tool list

**Operation index**: the compact listing `search_operations` returns — one line per catalog operation (its ID and the first line of its description; for a legacy operation, what it does and its replacement), grouped by tag, optionally filtered by a query.
_Avoid_: API list, endpoint list

**Operation description**: what `describe_operation` returns for one operation — every parameter with its location, type, required flag, enum, bounds and default.
_Avoid_: operation docs, spec (for this per-operation view)

**Read operation**: an operation using HTTP GET; executable only through `execute_read`.

**Write operation**: an operation using POST, PUT, PATCH or DELETE; executable only through `execute_write`.
_Avoid_: mutation (outside code comments)

**Destructive operation**: a write operation using DELETE, PUT or PATCH — one that removes or overwrites existing data; it runs only after a confirmation.

**Confirmation**: the user's explicit acceptance of one destructive operation call, asked by the server through the client (elicitation); a declined or cancelled confirmation, or a client that cannot ask, refuses the call before any request reaches Langfuse.
_Avoid_: approval (that is the client's own per-tool permission prompt), consent

**Write mode**: the server state, fixed at startup by `LANGFUSE_MCP_ALLOW_WRITES=true`, in which `execute_write` is registered. Default is off.
_Avoid_: admin mode, unsafe mode

**Workflow tool**: a dedicated read tool for a high-traffic agent workflow (e.g. trace investigation), as opposed to the generic `execute_*` tools.

**User skill**: an Agent Skill shipped by this project that the agent's client loads, teaching the agent how to reach its goal with this server's tools; it runs nothing itself, and the server can neither load it nor rely on it being loaded.
_Avoid_: Agent Skill (Langfuse's own skill for its CLI), prompt (an MCP prompt is a user-invoked command), workflow tool

**Tool error**: a tool result with `isError: true` carrying a stable error code, message, hint and retryability (ADR-0008).
_Avoid_: exception, failure response

## Langfuse connection

**Host**: the base URL of a Langfuse deployment (`LANGFUSE_BASE_URL`, alias `LANGFUSE_HOST`); the API lives under `<host>/api/public`.
_Avoid_: endpoint, server URL

**Cloud host**: one of the four Langfuse Cloud region hosts (EU, US, JP, HIPAA); any other host is a self-hosted instance. Always given as a URL, never by region name.
_Avoid_: region preset

**Self-hosted instance**: a Langfuse deployment run by the user's organization at its own host.

**Operation family**: a group of operations that a deployment turns on or off together through its write mode (legacy family, v4 read family, experiments family).

**Legacy operation**: a deprecated operation, part of the legacy family; exposed only when the connected deployment still answers it.
_Avoid_: v1 API, old API

**Unavailable operation**: an operation of the catalog that the connected deployment does not serve (its version lacks the route, or its write mode turns the operation's family off); a call to it returns the tool error `operation_unavailable`, never Langfuse's body.
_Avoid_: missing endpoint, not found (that is a missing resource)

**Deployment profile**: the detected Langfuse version plus the families that answered at startup; it decides which operations are in the catalog for the process lifetime.

**Project key**: a public/secret key pair (`pk-lf-…`/`sk-lf-…`) scoped to one Langfuse project; it has full access to that project.

**Organization key**: a key pair scoped to a Langfuse organization; required by organization operations (projects admin, memberships, SCIM).

**Request limits**: the rate limit (Langfuse requests per minute) and concurrency cap (Langfuse requests in flight) one server process applies to all its Langfuse calls, set only by the operator (`LANGFUSE_MCP_RATE_LIMIT`, `LANGFUSE_MCP_MAX_CONCURRENCY`), never by a tool argument; left unset, the rate limit's default depends on whether the host is a Cloud host; a call they hold past its deadline is *throttled* and never sent.

**Proxy**: the HTTP or SOCKS proxy the server's Langfuse calls go through, named by the operator with `HTTPS_PROXY` (hosts in `NO_PROXY` reached directly) in the environment or the config file, never by a tool argument; the *proxy in use* is shown only as scheme, host and port, never with its credentials.

**Config file**: an optional per-user file at the OS config location holding non-secret settings; environment variables take precedence over it and it may never hold keys.
_Avoid_: dotenv, settings file

## Trust

**Explicit CA source**: a CA file or directory named through this server's own variables (`LANGFUSE_CA_CERT`, `LANGFUSE_CA_CERTS_PATH`) in the environment or the config file, or through any CA variable set in the config file; failing to load it stops startup.

**Ambient CA source**: a CA file or directory named through a widely used variable already present in the environment (`SSL_CERT_FILE`, `SSL_CERT_DIR`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`); failing to load it is logged and skipped.

**Trust pool**: the OS certificate store (or, when the OS offers none, the fallback roots bundled into the binary) plus every explicit and ambient CA source; the only roots the server trusts.
_Avoid_: truststore (Java/Python term), CA bundle (when you mean the whole pool)

## Langfuse domain (as used by the API)

**Trace**: one end-to-end execution of an instrumented LLM application; groups observations.

**Observation**: a timed step inside a trace — span, generation, event and newer typed variants; forms a tree via `parentObservationId`.

**Trace tree**: the observations of one trace in depth-first pre-order, each with its depth, the parent before its children; what `get_trace_tree` returns.

**Payload query**: an observation query that requests the `io` or `metadata` field groups — the application's own content, not timing or cost data.
_Avoid_: heavy query, io query

**Session**: a group of traces sharing a `sessionId` (e.g. one chat conversation).

**Score**: an evaluation value (numeric, categorical or boolean) attached to a trace, observation, session or experiment.

**Prompt**: a versioned, labelled prompt template (text or chat); the `production` label marks the version served by default.

**Dataset / dataset item**: a named collection of inputs (and expected outputs) used to run experiments.

**Folder name**: a prompt or dataset name whose `/`-separated segments place it inside folders (e.g. `support/triage/system`).
_Avoid_: path, directory, nested name — "path" is reserved for the URL path and its path parameters.

**Experiment**: a run of an application over a dataset, producing experiment items linked to traces and scores.
_Avoid_: dataset run (deprecated API name)
