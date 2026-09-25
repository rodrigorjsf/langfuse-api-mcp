# Deep research run 1 — verified findings (2026-09-25)

> Source: `deep-research` workflow run `wf_bec28a45-ef4` (24 sources fetched, 115 claims extracted, 25 verified by 3-vote adversarial check, 24 confirmed, 1 refuted). Raw JSON: [`raw/2026-09-25-deep-research-run1.json`](raw/2026-09-25-deep-research-run1.json).

## Summary

The verified evidence points to Go and the official modelcontextprotocol/go-sdk as the strongest stack for a lightweight, robust, cross-platform Langfuse MCP server. The go-sdk is a Tier 1 SDK, which means it must pass 100% of the conformance tests and ship new spec features before each spec release. It is co-maintained with Google. From v1.7.0 it supports every spec version from 2024-11-05 to the new stateless 2026-07-28 spec. TypeScript, Python and C# are also Tier 1, Rust (rmcp) supports the new spec only in beta, and Java is not mentioned. On corporate TLS, the root cause is language-specific. By default, Node.js ignores both the OS trust store and HTTPS_PROXY unless the process is started with opt-in flags (NODE_USE_SYSTEM_CA or --use-system-ca, NODE_EXTRA_CA_CERTS, NODE_USE_ENV_PROXY). That makes it a likely, though unconfirmed, cause of the official tooling's failures. Go, Rust (rustls-platform-verifier with new_with_extra_roots) and Python (truststore) can all use the OS trust store and add explicit extra CAs in code. Go has one trap: since Go 1.27, setting SSL_CERT_FILE or SSL_CERT_DIR on macOS and Windows turns off platform verification. The server should therefore load explicit CA files and directories into a pool in its own code rather than rely on those environment variables. On the Langfuse side, the API uses HTTP Basic Auth (public key as username, secret key as password) against four cloud regions (EU, US, JP, HIPAA) plus any self-hosted host under /api/public. The official project MCP at <host>/api/public/mcp enables read, write and destructive tools by default and leaves read-only restriction to a client-side allowlist. That gap justifies a server-side read-only mode and write gating in a replacement.

## Findings

### F1 — confidence: high

The Go SDK (modelcontextprotocol/go-sdk) is the best-supported choice among the lightweight candidates. It is official, co-maintained with Google and Tier 1. From v1.7.0 it supports spec versions 2026-07-28, 2025-11-25, 2025-06-18, 2025-03-26 and 2024-11-05. TypeScript, Python and C# are also Tier 1 and were updated for 2026-07-28. Rust (rmcp) supports that spec only in beta, and the release post does not mention Java or Kotlin.

**Evidence:** Tier 1 requires a 100% pass rate on applicable conformance tests and full implementation of non-experimental features, including sampling and elicitation. Tier 1 SDKs must ship new features before a spec release; Tier 2 SDKs have 6 months. The release post says: 'All four Tier 1 SDKs speak 2026-07-28 as of today: TypeScript, Python, Go, and C#... the Rust SDK supports the new spec in beta.' The go-sdk README version table lists 'v1.7.0+ supports: 2026-07-28, 2025-11-25*, 2025-06-18, 2025-03-26, 2024-11-05'. The asterisk covers experimental client-side OAuth, which does not affect a server. The claims behind this finding passed with 3-0 votes.

**Sources:** https://modelcontextprotocol.io/community/sdk-tiers, https://blog.modelcontextprotocol.io/posts/2026-07-28/, https://github.com/modelcontextprotocol/go-sdk

### F2 — confidence: high

MCP spec 2026-07-28 is stateless. It removes the initialize/initialized handshake and the Mcp-Session-Id header, and each request carries its protocol version and client capabilities in _meta. Clients SHOULD also send their client info. This fits the requirement for a stateless server, and the SDK chosen must implement server/discover.

**Evidence:** The changelog lists SEP-2567 (remove sessions and the Mcp-Session-Id header) and SEP-2575 (remove the initialize handshake; add _meta keys io.modelcontextprotocol/protocolVersion and clientCapabilities). Client identity is only a SHOULD, and servers MUST implement server/discover. Vote 3-0.

**Sources:** https://blog.modelcontextprotocol.io/posts/2026-07-28/, https://modelcontextprotocol.io/specification/2026-07-28/changelog

### F3 — confidence: high

Node.js ignores the OS certificate store and the proxy environment variables by default. It trusts a system-installed corporate CA only when NODE_USE_SYSTEM_CA=1 or --use-system-ca is set (v22.19.0+ or v24.6.0+), or when NODE_EXTRA_CA_CERTS points to a PEM file. It honors HTTP_PROXY, HTTPS_PROXY and NO_PROXY only when NODE_USE_ENV_PROXY or --use-env-proxy is set (fetch: v22.21.0+ or v24.0.0+; http/https: v22.21.0+ or v24.5.0+). A Node-based MCP started by a client without these switches will likely fail TLS behind a TLS-intercepting proxy.

**Evidence:** The Node docs say: 'By default, Node.js uses Mozilla's bundled root CAs and does not consult the OS store.' They also say: 'With both enabled, Node.js trusts bundled CAs, system CAs, and the additional certificates specified by NODE_EXTRA_CA_CERTS.' Proxy support: 'Node.js supports these when NODE_USE_ENV_PROXY or --use-env-proxy is enabled.' NODE_EXTRA_CA_CERTS is read only when the process starts. That the official Langfuse MCP fails for this reason is our inference and was not verified. Votes 3-0.

**Sources:** https://nodejs.org/learn/http/enterprise-network-configuration

### F4 — confidence: high

In Go, SSL_CERT_FILE and SSL_CERT_DIR override the default certificate locations. Since Go 1.27, setting either one on macOS or Windows turns off the platform verification APIs, so the OS store is no longer used unless GODEBUG=x509sslcertoverrideplatform=0 is set. SSL_CERT_DIR replaces the default directories instead of adding to them. A robust design should load x509.SystemCertPool() in code and append the configured CA file and directory, not ask users to set those environment variables.

**Evidence:** From pkg.go.dev/crypto/x509 (go1.27.1): 'On platforms which have system APIs for certificate verification (macOS and Windows), setting SSL_CERT_FILE or SSL_CERT_DIR will prevent those APIs from being used... (This changed in Go 1.27.)' This claim passed 3-0. ko.build says 'Go uses SSL_CERT_DIR to determine the directory to check for SSL certificate files.' That claim passed only 2-1 and ko.build is a secondary source, but the official Go docs corroborate it. The recommendation to append in code is a design inference drawn from these facts.

**Sources:** https://pkg.go.dev/crypto/x509, https://ko.build/advanced/root-ca-certificates/

### F5 — confidence: high

Rust's rustls-platform-verifier verifies certificates through the OS: the Windows cert store and CryptoAPI on Windows, and Security.framework with the keychain on macOS 10.14+. On Linux it uses the system CA bundle with webpki. Verifier::new_with_extra_roots adds explicit CAs on top of the platform roots, which fits an explicit ca-cert option directly. On Linux, including Docker, the trust store is loaded once at startup, and there is no revocation checking.

**Evidence:** The README platform table and source code show new_with_extra_roots in others.rs:35, windows.rs:569 and apple.rs:73 (not on Android). If system roots fail to load after extra roots were supplied, the error is only logged. Votes 3-0.

**Sources:** https://github.com/rustls/rustls-platform-verifier

### F6 — confidence: medium

Python can trust the OS store through truststore (Python 3.10+). It uses the Security framework on macOS 10.8+, CryptoAPI on Windows and the OpenSSL default paths on Linux. The application must opt in, either with truststore.inject_into_ssl() or by passing truststore.SSLContext. By default, Python HTTP clients use certifi's bundle.

**Evidence:** The docs describe truststore as 'A library which exposes native system certificate stores through an ssl.SSLContext-like API.' The platform list passed 3-0, but the claim that no extra configuration is needed passed only 2-1 because the developer has to opt in. On Linux and in containers, the CA must still be installed in the image's own store.

**Sources:** https://truststore.readthedocs.io/

### F7 — confidence: high

The Langfuse Public API authenticates with HTTP Basic Auth: the project public key (pk-lf-) is the username and the secret key (sk-lf-) is the password. The base URL is <host>/api/public. There are four cloud hosts: EU https://cloud.langfuse.com, US https://us.cloud.langfuse.com, JP https://jp.cloud.langfuse.com and HIPAA https://hipaa.cloud.langfuse.com. The regions are fully separate, so keys work only on their own region's host. Self-hosted instances use their own host with the same path, so the four cloud hosts should be presets, not a closed allowlist.

**Evidence:** The OpenAPI spec declares only BasicAuth, with the description 'username: Langfuse Public Key - password: Langfuse Secret Key'. The public-api page lists the four regional /api/public URLs, and the data-regions table lists the same four hosts. Organization-level keys use the same Basic scheme for the org, SCIM and project admin endpoints. Votes 3-0.

**Sources:** https://langfuse.com/docs/api-and-data-platform/features/public-api, https://langfuse.com/security/data-regions, docs/langfuse-openapi.json (local, components.securitySchemes.BasicAuth)

### F8 — confidence: high

Langfuse runs an official, project-scoped, stateless MCP server at <host>/api/public/mcp over Streamable HTTP, with Basic auth of base64(pk:sk) and no Bearer tokens. It has endpoints for the four cloud regions, self-hosted and localhost. A separate Docs MCP at https://langfuse.com/api/mcp needs no authentication. Self-hosted instances require HTTPS and a preserved Host header or LANGFUSE_MCP_ALLOWED_HOSTS; otherwise the server returns 403. For environments that can run a CLI, Langfuse recommends its Agent Skill/CLI instead.

**Evidence:** The docs list 'Endpoint: https://cloud.langfuse.com/api/public/mcp - Transport: streamableHttp - Authentication: Basic Auth via authorization header'. The reference page lists the EU, US, JP, HIPAA, self-hosted and local-dev endpoints and describes the Docs MCP as unauthenticated. Votes 3-0 on all three merged claims.

**Sources:** https://langfuse.com/docs/api-and-data-platform/features/mcp-server, https://mcp.reference.langfuse.com/

### F9 — confidence: high

The official Langfuse MCP enables read, write and destructive tools by default, and read-only use depends entirely on an allowlist in the client. The docs do not describe a server-side read-only mode. Write tools include createTextPrompt, createChatPrompt, updatePromptLabels, createScore, createScoreConfig, updateScoreConfig, createModel, createComment, upsertDataset, createEvaluator and createDashboard. Destructive tools include deleteModel, deleteScoreConfig, deleteAnnotationQueueItem, deleteAnnotationQueueAssignment, deleteDatasetItem, deleteDatasetRun, deleteEvaluator, deleteEvaluationRule and deleteDashboard. A replacement should enforce write gating on the server, for example with a read-only default, annotations and explicit-request wording in tool descriptions.

**Evidence:** The docs say: 'Both read and write tools are available by default. If you only want to use read-only tools, configure your MCP client with an allowlist.' The write tools carry destructiveHint annotations, which are hints to the client only. Correction: the upstream claim of 'full CRUD on annotation queues' is overstated. Queues support only list, create and get; full CRUD exists only for queue items. That the server has no read-only mode is inferred from the docs saying nothing about one. Votes 3-0.

**Sources:** https://langfuse.com/docs/api-and-data-platform/features/mcp-server, https://mcp.reference.langfuse.com/

## Caveats

No claims survived verification for large parts of the research question: OWASP LLM Top 10 2025, OWASP Agentic Top 10, MCP security best practices (tool poisoning, confused deputy, token passthrough, SSRF), Matt Pocock's skill concepts, Anthropic's mcp-server-dev skills, MIT vs Apache-2.0, Langfuse rate limits, and measured binary size, memory or startup figures per SDK. Any recommendation on those topics still needs sourcing. The Go recommendation rests on SDK tier and TLS-control evidence. The single-static-binary, low-memory and easy cross-compilation benefits are widely known but were not verified in this run. One claim was refuted 1-2: that SSL_CERT_DIR takes colon-separated lists (semicolon-separated on Windows). Evidence that another verifier quoted from the same page supports it, so treat it as unsettled rather than false. Several claims split 2-1 (the ko.build SSL_CERT_DIR claim, where a secondary source was corroborated by the Go docs, and the truststore 'no extra configuration' claim, which requires an app opt-in). Time sensitivity: the 2026-07-28 spec is two months old, and SDK tiers can be relegated after 4 weeks of conformance failures. The Go 1.27 SSL_CERT_* behaviour change is recent. The Langfuse region list has grown before (JP, HIPAA), and the official MCP tool list is live and changing. That the official Langfuse MCP fails because of Node's default trust behaviour is an inference; the failure was not reproduced.

## Open questions

- Do Go's x509.SystemCertPool() plus an in-code AppendCertsFromPEM behave the same on Windows and macOS under Go 1.27 (platform verifier plus extra roots) as on Linux? Should the server set GODEBUG x509sslcertoverrideplatform=0 to protect against users who export SSL_CERT_FILE?
- What exactly causes the TLS failures in the official Langfuse MCP and CLI? Is it Node started without NODE_USE_SYSTEM_CA or NODE_USE_ENV_PROXY, a remote Streamable HTTP client in the MCP host, or something else? Reproducing it would confirm the design motivation.
- What primary guidance do OWASP (LLM Top 10 2025, the Agentic Top 10, the MCP Top 10) and the MCP spec's security best practices give on write gating, SSRF-safe host configuration, output size limits and prompt injection through tool results? These need sourced findings before the security rules are written.
- What rate limits apply to the Langfuse Public API per plan and endpoint, and how should the server handle 429 responses and paginate without exhausting them? MIT or Apache-2.0: does Apache-2.0's patent grant matter to enterprise adopters of an MCP server?

## Refuted

- (1-2) In Go, SSL_CERT_FILE and SSL_CERT_DIR override the system default CA locations. SSL_CERT_DIR accepts a list separated by colons, or by semicolons on Windows. — https://pkg.go.dev/crypto/x509

## Sources

- [primary] https://modelcontextprotocol.io/community/sdk-tiers — Stack choice: official MCP SDK maturity and footprint
- [primary] https://blog.modelcontextprotocol.io/posts/2026-07-28/ — Stack choice: official MCP SDK maturity and footprint
- [blog] https://www.digitalapplied.com/blog/mcp-sdk-conformance-tiers-what-tier-1-means — Stack choice: official MCP SDK maturity and footprint
- [primary] https://github.com/modelcontextprotocol/go-sdk — Stack choice: official MCP SDK maturity and footprint
- [blog] https://github.com/desty2k/mcp-benchmark — Stack choice: official MCP SDK maturity and footprint
- [blog] https://www.stainless.com/mcp/mcp-sdk-comparison-python-vs-typescript-vs-go-implementations — Stack choice: official MCP SDK maturity and footprint
- [primary] https://nodejs.org/learn/http/enterprise-network-configuration — Corporate TLS / custom CA trust stores
- [primary] https://pkg.go.dev/crypto/x509 — Corporate TLS / custom CA trust stores
- [primary] https://github.com/rustls/rustls-platform-verifier — Corporate TLS / custom CA trust stores
- [primary] https://truststore.readthedocs.io/ — Corporate TLS / custom CA trust stores
- [primary] https://ko.build/advanced/root-ca-certificates/ — Corporate TLS / custom CA trust stores
- [forum] https://github.com/GoogleContainerTools/distroless/issues/668 — Corporate TLS / custom CA trust stores
- [primary] https://langfuse.com/docs/api-and-data-platform/features/public-api — Langfuse API, existing MCPs and agent workflows
- [primary] https://langfuse.com/docs/api-and-data-platform/features/mcp-server — Langfuse API, existing MCPs and agent workflows
- [primary] https://mcp.reference.langfuse.com/ — Langfuse API, existing MCPs and agent workflows
- [primary] https://langfuse.com/security/data-regions — Langfuse API, existing MCPs and agent workflows
- [primary] https://modelcontextprotocol.io/docs/2026-07-28/tutorials/security/security_best_practices — Security: OWASP LLM/Agentic/MCP guidance
- [primary] https://github.com/OWASP/www-project-mcp-top-10 — Security: OWASP LLM/Agentic/MCP guidance
- [primary] https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/ — Security: OWASP LLM/Agentic/MCP guidance
- [primary] https://owasp.org/www-project-top-10-for-large-language-model-applications/assets/PDF/OWASP-Top-10-for-LLMs-v2025.pdf — Security: OWASP LLM/Agentic/MCP guidance
- [primary] https://blog.modelcontextprotocol.io/posts/2026-03-16-tool-annotations/ — Security: OWASP LLM/Agentic/MCP guidance
- [primary] https://github.com/mattpocock/skills/blob/main/skills/engineering/README.md — Engineering process, Claude rules and licensing
- [primary] https://code.claude.com/docs/en/memory — Engineering process, Claude rules and licensing
- [primary] https://github.com/anthropics/claude-plugins-official/blob/main/plugins/mcp-server-dev/skills/build-mcp-server/SKILL.md — Engineering process, Claude rules and licensing

## Appendix — all extracted claims, per source (`[sourced — unverified]`)

Claims below were extracted from fetched sources but most did not go through the 3-vote verification (budget cap). Treat as `[sourced — unverified]` until re-checked. Blocks carry no URL (the workflow dropped it); match a block to the Sources list by topic.

### Block 1 (primary source)

- Node.js does not trust the OS certificate store by default. Since v22.19.0 and v24.6.0 it can be told to trust system CAs (such as corporate root CAs) with NODE_USE_SYSTEM_CA=1 or the --use-system-ca flag. System CAs are then used in addition to Node's bundled CAs.
- NODE_EXTRA_CA_CERTS points Node.js at a PEM file of one or more extra CA certificates, without using the system store. It can be combined with NODE_USE_SYSTEM_CA so that bundled, system and extra CAs are all trusted.
- Node's system CA loading depends on the platform: the Windows Certificate Store on Windows, the Keychain on macOS, and OpenSSL defaults on Linux (SSL_CERT_FILE/SSL_CERT_DIR or /etc/ssl paths).
- Node.js only honors the HTTP_PROXY, HTTPS_PROXY and NO_PROXY environment variables when NODE_USE_ENV_PROXY or --use-env-proxy is enabled. This works for fetch() from v22.21.0 or v24.0.0+, and for node:http/https from v22.21.0 or v24.5.0+.
- Node's NO_PROXY accepts wildcards, domain suffixes, exact IPs, IP ranges and host:port entries.

### Block 2 (primary source)

- rustls-platform-verifier verifies TLS certificates using the operating system's own certificate facilities rather than a bundled static root set, so certificates trusted by the OS (e.g., corporate root CAs) are honored.
- On Windows it uses the Windows platform certificate store and Windows API verification; on macOS (10.14+) it uses platform roots and keychain via Security.framework; on Linux it uses the system CA bundle or user-provided certs with webpki verification.
- Extra (e.g., enterprise/custom) root certificates can be added on top of platform roots via Verifier::new_with_extra_roots, supporting an explicit CA-cert config option.
- The rustls maintainers recommend platform verification as the default for client-side libraries because it gets live trust information, unlike static root bundles.
- Certificate revocation checking is supported on Windows and macOS but not on Linux.

### Block 3 (primary source), publishDate=go1.27.1 docs (fetched 2026-09-25)

- In Go, SSL_CERT_FILE and SSL_CERT_DIR override the system default CA locations. SSL_CERT_DIR accepts a list separated by colons, or by semicolons on Windows.
- Since Go 1.27, setting SSL_CERT_FILE or SSL_CERT_DIR on macOS and Windows turns off the platform verification APIs, so the OS trust store is no longer used, unless GODEBUG x509sslcertoverrideplatform=0 is set.
- SystemCertPool returns a copy of the system pool held in memory. Adding a corporate CA to that copy does not change the disk or other pools, so the server can combine system roots with custom roots without touching the host.
- SetFallbackRoots supplies roots when no system pool or platform verifier exists, for example in a distroless or scratch container with no CA bundle. It can be called only once. Forcing it with GODEBUG=x509usefallbackroots=1 turns off platform verification on Windows and macOS.
- CertPool.AppendCertsFromPEM parses a series of PEM-encoded certificates and reports whether any were parsed. The server can use this to load a user-provided CA file or directory and fail loudly when the file holds no valid certificate.

### Block 4 (primary source)

- Go applications determine which directory to search for trusted root certificate files via the SSL_CERT_DIR environment variable, so pointing it at a mounted/bundled cert directory makes a Go binary trust those CAs.
- Root CA certificates can be bundled into a ko-built Go image via the kodata directory, and trust is achieved by setting SSL_CERT_DIR to $KO_DATA_PATH at runtime.
- Custom CA certificates can be appended to an already-built Go container image with the incert tool, without rebuilding.
- A custom base image with pre-installed root certificates can replace ko's default base image via .ko.yaml or the KO_DEFAULTBASEIMAGE environment variable.

### Block 5 (primary source), publishDate=Truststore 0.10.4 docs (no explicit date)

- Truststore lets Python HTTP clients verify TLS against the operating system's native certificate store instead of certifi's bundled CA list. That means a root CA installed by a corporate proxy in the OS store is trusted without any extra configuration.
- The trust store truststore uses depends on the platform: the Security framework on macOS 10.8+, CryptoAPI on Windows, and OpenSSL on Linux.
- Truststore requires Python 3.10 or later and raises ImportError on older versions. It works with urllib3, requests and aiohttp, either through injection or by passing a truststore.SSLContext directly.
- inject_into_ssl() is meant only for applications and scripts, not libraries. A Python MCP server would call it early, as the application entrypoint.
- The docs list advantages of the system store over certifi: certificates update automatically, missing intermediate certificates are fetched, certificates are checked against CRLs, and the store is managed by the system rather than by each application.

### Block 6 (primary source), publishDate=2026-02-23 (official SDK tiering published; conformance tests available 2026-01-

- Tier 1 MCP SDKs must pass 100% of applicable conformance tests and fully implement the protocol, including optional capabilities like sampling and elicitation. Tier 2 needs an 80% pass rate. Tier 3 has no minimum.
- Tier 1 SDKs must ship new protocol features before a new spec version is released. Tier 2 SDKs have 6 months. Tier 3 SDKs have no timeline commitment.
- Tier 1 SDKs must triage issues within 2 business days and fix critical (P0) bugs within 7 days. P0 includes security vulnerabilities with CVSS >= 7.0.
- Tier 1 and Tier 2 both require a stable release (1.0.0 or higher with no pre-release tag) and a published dependency update policy. Tier 3 SDKs may be experimental or only partly implemented.
- An SDK can be moved down a tier if conformance tests on its latest stable release keep failing for 4 weeks, or if issues go unaddressed for two months. So a tier is not permanent and should be re-checked when choosing a stack.

### Block 7 (forum source), publishDate=2021-01-04

- The user tried to add a custom CA to a distroless image with a multi-stage build: a Debian builder stage installs the certificate and runs update-ca-certificates, then /etc/ssl/certs is copied into the final distroless image.
- Even though the build appeared to add the certificate, the Go-based Kubernetes controller running in the distroless image still failed TLS verification with an unknown-authority error. So a naive copy of the certs directory is not guaranteed to make the runtime trust the CA, and needs to be checked at runtime.
- The issue has no maintainer response, comments or confirmed fix in the fetched content, so the distroless project gives no official, documented way to inject custom CA certificates through this thread.

### publishDate=2026-07-28, sourceQuality=primary

- The 2026-07-28 MCP spec makes the protocol stateless: it removes the initialize/initialized handshake and session identifiers, and each request carries its protocol version, client identity and capabilities in _meta.
- Under the 2026-07-28 spec, Tier 1 SDKs already updated for it are TypeScript, Python, Go and C#. Rust (rmcp) supports it only in beta, and the post does not mention a Java SDK.
- Streamable HTTP requests must now include the Mcp-Method and Mcp-Name headers so gateways can route and meter them.
- Roots, Sampling, Logging and the legacy HTTP+SSE transport are deprecated, with an off-ramp of at least twelve months. Multi Round-Trip Requests (MRTR) replace server-initiated requests, which lets a server get user confirmation mid-call without keeping state.
- Authorization is hardened with RFC 9207 issuer validation, a move from Dynamic Client Registration to Client ID Metadata Documents, and credentials bound to their issuer. List/read responses now carry ttlMs and cacheScope for client caching.

### publishDate=2026-08-21, sourceQuality=blog

- Five of ten official MCP SDKs are Tier 1: TypeScript, Python, C#, Go, and Rust (rmcp). Java and Ruby are Tier 2, and Swift, PHP and Kotlin are Tier 3.
- Tier 1 means complete protocol implementation, including all non-experimental features and optional capabilities such as sampling and elicitation. Tier 2 requires at least an 80% conformance-test pass rate.
- Rust rmcp passed 67/67 server and 50/50 client conformance tests (conformance suite 0.2.0-alpha.11). Its stable release is rmcp v3.0.1, published July 29, 2026.
- The latest MCP spec revision at the time was 2026-07-28, which is newer than 2025-11-25. The rmcp SDK tracks it the same day it ships.

### Block 8 (primary source)

- The Go SDK is the official MCP SDK for Go, used for both servers and clients, and is maintained together with Google.
- Go SDK v1.7.0 and later support the MCP spec versions 2026-07-28, 2025-11-25, 2025-06-18, 2025-03-26 and 2024-11-05.
- The SDK ships separate packages for the core MCP APIs, JSON-RPC transport, OAuth primitives and OAuth extensions.
- Tool handlers are typed, and the SDK builds each tool's input JSON schema from Go struct tags.
- The SDK's license is permissive: new contributions are Apache 2.0 and existing code is MIT, so enterprises can use it.

### Block 9 (blog source)

- For MCP servers that mainly proxy an upstream API (such as a Langfuse REST wrapper), the language choice barely affects performance. Every framework tested stayed under 3 ms p50 proxy latency, and network latency to the upstream is the real bottleneck.
- Idle memory differs a lot by language. The Rust rmcp server used about 7 MB idle RSS, Go (mcp-go) about 21 MB, and Python FastMCP about 97 MB.
- Go and Rust start fastest, at 33 ms and 38 ms. Under load, Go used 128 MB RSS versus 169 MB for Rust and 185 MB for Python. The Go server used the community mark3labs mcp-go 0.47.0, not the official go-sdk.
- On the echo proxy tool (p50), Rust rmcp 1.3.0 measured 0.38 ms, Go 0.50 ms, C# 0.60 ms, TypeScript SDK 1.29.0 on Node 24 0.76 ms, and Python FastMCP 3.2.0 2.25 ms, all over Streamable HTTP on localhost.
- For compute-heavy tools, the choice of JSON library mattered more than the language. The methodology was 1000 requests per proxy tool, 200 per compute tool, and 3 runs per framework.

### Block 10 (blog source)

- Stainless (a vendor-marketing guide) can generate MCP servers automatically only in TypeScript. Its Python and Go SDKs are for client-side use, so an OpenAPI-to-MCP codegen route through Stainless ties the server to TypeScript.
- The Python, TypeScript and Go MCP SDKs have equal stdio transport support, but only Python and TypeScript are described as having mature HTTP/SSE server implementations. Go's HTTP/SSE support is described as mainly used on the client side.
- Stdio transport support is the same across the Python, TypeScript and Go SDKs for local and desktop clients.
- The page gives only qualitative performance claims, with no benchmarks, memory figures or binary-size data. It says Go's goroutines make it efficient for concurrent workloads, and that Python CPU-bound work is limited by the GIL.
- The page describes all three SDKs as mature or production-ready, but gives no version, date or spec-revision evidence to support this.

### Block 11 (primary source)

- The Langfuse Public API authenticates with HTTP Basic Auth: the project public key is the username and the secret key is the password. Both keys come from the project settings.
- Langfuse Cloud has four regional base URLs: EU (cloud.langfuse.com), US (us.cloud.langfuse.com), Japan (jp.cloud.langfuse.com) and HIPAA US (hipaa.cloud.langfuse.com). Each one has /api/public appended.
- A self-hosted Langfuse instance at v4.36.0 or later serves its own OpenAPI spec at /api/openapi.yaml and an interactive reference at /api/docs. The cloud spec is at cloud.langfuse.com/generated/api/openapi.yml. The Organization API has a separate spec.
- The legacy Ingestion API is deprecated and will be shut down on Langfuse Cloud on November 16, 2026. Traces should be ingested through the OpenTelemetry endpoint instead.
- On Langfuse Cloud, Public API requests count toward a general rate limit applied per organization. The Observations v2 and Scores v3 endpoints use cursor-based pagination.

### Block 12 (primary source)

- Langfuse has a native, authenticated MCP server for the Langfuse data platform. It is exposed at <host>/api/public/mcp over the Streamable HTTP transport and authenticates with Basic Auth: a base64-encoded project-scoped public-key:secret-key pair. Endpoints are listed for Cloud EU, US, Japan, HIPAA US and self-hosted instances.
- The official authenticated MCP server exposes both read and write tools by default. Restricting it to read-only is left to the client, which must configure an allowlist; the server offers no read-only mode of its own.
- The official Langfuse MCP server is stateless, and each API key is scoped to a single project.
- Behind a reverse proxy, a self-hosted deployment of the official MCP endpoint returns 403 unless the public Host header is preserved or LANGFUSE_MCP_ALLOWED_HOSTS lists the accepted hostnames. The docs also call HTTPS required for self-hosted instances. The page documents only an HTTP endpoint and says nothing about CA or TLS trust configuration on the client side.
- Where an agent can install CLI tools and run bash, Langfuse recommends its Agent Skill over the MCP server. The page also names https://mcp.reference.langfuse.com as the canonical list of MCP tools and schemas, for example listPrompts, listObservations and getObservationFilterValues.

### Block 13 (primary source)

- Langfuse Cloud has four regional hosts: EU https://cloud.langfuse.com, US https://us.cloud.langfuse.com, Japan https://jp.cloud.langfuse.com, HIPAA https://hipaa.cloud.langfuse.com; an MCP host config/allowlist must cover all of them.
- Official Langfuse SDKs select the region through the LANGFUSE_BASE_URL environment variable or an init parameter, a naming convention the MCP server can reuse.
- Regions are fully isolated, so API keys and data from one region do not work against another region's host.
- Self-hosting is offered as the alternative to Cloud regions, so the server must also accept arbitrary self-hosted base URLs.
- Secondary (failover) regions stay in the same legal jurisdiction as their primary region.

### Block 14 (primary source)

- Langfuse runs an official authenticated MCP server scoped to one project at /api/public/mcp, with endpoints for each Cloud region (EU, US, Japan, HIPAA US) plus self-hosted and local-dev hosts.
- The Cloud MCP authenticates with HTTP Basic auth built from the public key and secret key. The Docs MCP (https://langfuse.com/api/mcp) needs no authentication.
- The official project MCP exposes both read tools and write/destructive tools. Examples: createTextPrompt, createChatPrompt, updatePromptLabels, creating scores, full CRUD on annotation queues, and deleting model definitions. Any replacement needs to gate writes.
- The official MCP tool surface is not fixed: tools and fields can be added, removed, or changed, and clients are expected to discover them at runtime. Score writes are eventually consistent.
- The official MCP limits observation queries: a date-scoped query can cover at most 14 days, and date-scoped projections return at most 50 items. The page documents no TLS/CA configuration options. (The 14-day and 50-item limits, and the absence of TLS/CA options, come from the fetch tool's summary, not verbatim quotes.)

### Block 15 (primary source), publishDate=2026-07-28 (spec version of the page)

- The MCP spec says MCP servers MUST NOT accept tokens that were not issued specifically for them. Token passthrough to downstream APIs is explicitly forbidden, so a Langfuse MCP must use its own configured Langfuse keys and must not forward credentials supplied by the client.
- MCP servers meant to run locally SHOULD use the stdio transport, or require an auth token (or a restricted IPC channel such as a unix socket) when they expose HTTP. This shapes the default transport and how Streamable HTTP mode is secured.
- In the 2026-07-28 spec, MCP has no protocol-level sessions. Servers that need state mint explicit handles, MUST NOT treat holding a handle as authentication, and SHOULD generate handles with a secure random number generator. This supports a stateless server design.
- SSRF guidance (written for OAuth URL fetching by clients, but applicable to validating a user-set host URL): require HTTPS except on loopback, block private, link-local and metadata IP ranges, check redirect targets, and do not hand-write IP validation.
- Least-privilege scope design: start with a minimal read-only scope set and step up for privileged operations. This backs defaulting the server to read-only and gating write tools behind explicit elevation.

### Block 16 (primary source), publishDate=2025

- The OWASP MCP Top 10 (2025) names Token Mismanagement & Secret Exposure as MCP01. It flags hard-coded credentials, long-lived tokens, and secrets kept in model memory or protocol logs, which supports redacting logs and keeping Langfuse secret keys out of tool output.
- MCP02:2025 is Privilege Escalation via Scope Creep. It warns that permissions that are temporary or loosely defined grow over time, which supports a read-only default mode and explicit gating of write tools.
- The list includes Tool Poisoning (MCP03) and Prompt Injection via Contextual Payloads (MCP06). Content returned by tools, such as Langfuse trace data, is therefore an attack path and should be treated as untrusted.
- MCP04:2025 covers Software Supply Chain Attacks & Dependency Tampering, which supports pinned dependencies, SBOMs, and signed images.
- MCP08:2025 is Lack of Audit and Telemetry. It recommends detailed logs of tool invocations with immutable audit trails, and MCP10 warns about context over-sharing across tasks and users. The project is licensed CC BY-NC-SA 4.0 and describes itself as a living document.

### Block 17 (primary source), publishDate=2025-12-09

- OWASP published the Top 10 for Agentic Applications for 2026 on December 9, 2025, as a downloadable guide; it is the current OWASP agentic-risk baseline an MCP server should be designed against.
- The framework covers critical security risks in autonomous and agentic AI systems, meaning agents that plan, act, and make decisions across workflows. That is the category of consumer an MCP server exposing Langfuse write operations serves.
- The framework was peer-reviewed by more than 100 industry experts, researchers and practitioners, which makes it an authoritative primary source for security requirements.
- OWASP has since published newer related GenAI guidance: an LLM Top 10 2026 (August 2026), an Agent Control Standard (September 2026) and an Industry Framework Crosswalk (September 2026). A 2026-era security design should check these as well as the LLM Top 10 2025.

### publishDate=2026-03-16, sourceQuality=primary

- MCP tool annotations are only hints, and clients must treat them as untrusted unless the server is trusted. A server therefore cannot rely on annotations alone to gate write or destructive operations.
- The annotation defaults are pessimistic: readOnlyHint=false, destructiveHint=true, idempotentHint=false, openWorldHint=true. A tool with no annotations is treated as potentially destructive and open-world.
- Annotations do not protect against prompt injection that arrives through tool results or other content the model reads.
- Annotations do not enforce anything. Real safety guarantees, such as preventing data exfiltration, need deterministic controls like network controls or sandboxing. This supports adding a server-side read-only mode or allowlist on top of the hints.
- The post advises server authors to set readOnlyHint: true on read-only tools, destructiveHint: false on additive operations, and openWorldHint: false on closed-domain tools.

### Block 18 (primary source)

- Matt Pocock's tdd skill does red-green-refactor and builds features or fixes bugs one vertical slice at a time, so TDD and vertical slices are coupled in his process.
- The codebase-design skill describes good architecture as small interfaces, clean seams, and code that is tested through its interface. This can be encoded as a .claude/rules architecture rule.
- The grill-with-docs skill builds the domain model interactively and updates CONTEXT.md (ubiquitous language) and ADRs inline, so the terminology docs are maintained while the work happens rather than afterwards.
- The workflow breaks plans into tracer-bullet tickets that declare their blocking edges. The implement skill then drives TDD at pre-agreed seams and runs a code review before commits.
- The code-review skill reviews along two axes, Standards and Spec, and runs each axis as a parallel sub-agent.

### Block 19 (primary source), publishDate=2025-03-12 (date on the genai.owasp.org LLM Top 10 2025 listing page. The given 

- The OWASP Top 10 for LLM Applications 2025 names Excessive Agency as LLM06:2025. It gives three root causes: excessive functionality, excessive permissions and excessive autonomy. Exposing all 117 Langfuse operations as tools increases the risk from excessive functionality, which is an argument for gating write tools or offering a read-only mode.
- For Excessive Agency, OWASP recommends these mitigations: give each extension only the functionality it needs, replace open-ended tools such as arbitrary URLs with narrower ones, run operations in the individual user's context, require human approval for significant actions, and enforce authorization in the downstream system rather than trusting the LLM's decision.
- OWASP LLM01:2025 (Prompt Injection) defines indirect prompt injection as instructions hidden in content that comes from external sources. For this server, that means Langfuse trace, prompt and dataset payloads it returns are untrusted input. OWASP's mitigations are to separate and clearly mark untrusted content and to require human-in-the-loop controls for privileged operations.
- OWASP advises that privileged functionality use the application's own API tokens and be handled in code, not handed to the model. For this server, that supports keeping the Langfuse keys in server-side configuration and never exposing them in tool inputs or outputs.
- The 2025 list also includes LLM02 Sensitive Information Disclosure, LLM03 Supply Chain, LLM05 Improper Output Handling and LLM10 Unbounded Consumption. These are the categories relevant to secret redaction, pinned or signed dependencies, output validation, and output-size or rate limits. For Excessive Agency, OWASP also recommends monitoring and rate limiting to limit damage.

### Block 20 (primary source)

- Rules in .claude/rules/ can be path-scoped with YAML `paths` glob frontmatter; scoped rules load only when Claude reads matching files, while rules without `paths` load unconditionally at launch — the mechanism for progressive disclosure of language/MCP rules.
- The .claude/rules/ directory is discovered recursively, with one topic per file recommended, so rules can be grouped into subdirectories.
- CLAUDE.md files should be kept under 200 lines. @imports do not save context because imported files still load at launch, so path-scoped rules are the way to reduce always-loaded context.
- CLAUDE.md and rules are advisory context, not hard enforcement. A rule such as 'no done without updated docs' needs a hook, such as PreToolUse, to be guaranteed.
- @path imports can recurse at most four hops, and multi-step procedures belong in skills rather than CLAUDE.md.

### Block 21 (primary source)

- For wrapping a large API surface (dozens to hundreds of endpoints, e.g. 50+), Anthropic's build-mcp-server skill tells builders to expose a search + execute tool pair, optionally promoting the 3-5 most-used actions to dedicated tools, instead of one tool per endpoint, because full tool lists flood the context window. This applies directly to the Langfuse API's 117 operations.
- The skill recommends only two frameworks: the official TypeScript SDK as the default, because it has the best spec coverage and gets new features first, and FastMCP 3.x for Python. It does not recommend Go, Rust, Java or C# SDKs, though it says other frameworks exist.
- The default recommended deployment for a server that wraps a cloud API is a remote Streamable HTTP server. Local stdio launched via npx/uvx is 'not recommended for distribution', and MCPB, a local server bundled with its runtime, is the sanctioned way to ship a server that must run on the user's machine.
- Before submitting a connector to the Anthropic Directory, Claude's pre-submission review criteria require a split between read and write tools, required tool annotations, limits on tool names, and prompt-injection rules.
- Elicitation (a mid-tool user confirmation) is supported in Claude Code from v2.1.76, but servers must check clientCapabilities.elicitation first and have a fallback, because the SDK throws if the client does not advertise the capability. This matters for gating write operations behind an explicit confirmation.
