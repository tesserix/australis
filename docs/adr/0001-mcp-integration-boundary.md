# ADR-0001: MCP integration boundary and ownership

- Status: Proposed
- Date: 2026-09-02
- Owner: Mahesh Sangawar
- Relates to: [PRD](../PRD.md) §5.2, §6.1, §7, §8, §11
- Supersedes: none

## Context

Australis is a grounded-assistant engine whose tenants supply knowledge in two
forms: a document corpus, and a read-only connector over the product's own live
data (PRD §6.1). The second form — the "tool retriever" — is the one that makes
the assistant useful, and it is deliberately left undefined in the PRD (§20,
open question: "KB registration UX — how a product declares a tool retriever").

This ADR closes that question. Model Context Protocol is the connector wire
format, and the Tesserix platform already ships the four pieces required:

| Concern | Component | Repo |
| --- | --- | --- |
| Server authoring and serving | `tesserix-mcp-runtime` | `tesserix-mcp-runtime` |
| Manifest compilation | `tesserix-mcp-manifest` | `tesserix-mcp-runtime/packages/` |
| Catalog and discovery | Agentic Registry | `agentic-registry` |
| Data-plane routing | AgentGateway | `tesserix-k8s`, adapter in `agentic-registry/adapters/agentgateway` |

The unresolved question is not *whether* to use MCP. It is **where MCP servers
live** and **how tightly Australis is allowed to bind to MCP**. A proposal was
raised to host every tenant's MCP server inside the `australis` repository on
the grounds that Australis is "the brain". That proposal is the subject of this
decision.

### Numbers this design is sized for

No production traffic exists; these are the stated planning assumptions for
Kora (tenant #1) at 12 months. They are deliberately recorded so a future
reader can see what the design was *not* built for.

| Quantity | Assumption | Derivation |
| --- | --- | --- |
| Kora DAU | 5,000 | product plan |
| Assistant interactions / user / day | 3 | coaching + weekly report |
| Chat requests / day | 15,000 | 5,000 x 3 |
| Mean chat RPS | 0.17 | 15,000 / 86,400 |
| Peak chat RPS | 1.5 | 8x diurnal consumer multiplier |
| MCP tool calls per chat | 1–3 | one retrieval, optional follow-up |
| **Peak MCP tool-call RPS** | **~5** | 1.5 x 3, rounded up |
| Registry objects | < 100 MCPServer revisions | 4 tenants x ~5 servers x tags |
| Resolved-manifest working set | < 2 MB | ~100 manifests x ~20 KB |

Five requests per second and a two-megabyte working set. This is a single
Postgres, a single in-process cache, and N stateless replicas. Any part of this
design that implies more machinery than that is over-built, and the reviewer
should say so.

### SLO

- **Australis chat path:** 99.5% monthly availability, p99 time-to-first-token
  < 2.5 s. Lower than a product-critical SLO on purpose — PRD §5.3 makes the
  assistant an enhancement, and the host product must work without it.
- **Latency budget** (p99, additive):

  | Hop | Budget |
  | --- | --- |
  | Product BFF overhead | 50 ms |
  | Australis request handling + policy | 30 ms |
  | Retrieval (hybrid, pgvector + FTS + rerank) | 150 ms |
  | MCP tool call via AgentGateway | 400 ms |
  | Model time-to-first-token | 1,200 ms |
  | **Total** | **1,830 ms** |

  670 ms of headroom against the 2.5 s target. The MCP hop owns 400 ms of it;
  a tool that cannot answer in 400 ms p99 is a batch job, not a retriever, and
  must be pre-materialised by the owning product.
- **Consistency:** eventual, per operation. A tool retriever may serve data
  seconds stale; the assistant cites *what it retrieved*, so staleness is
  disclosed rather than hidden. No operation in v1 requires strong consistency
  because no operation in v1 writes (see D6).

## Options considered

**Option A — All tenant MCP servers live in `australis`.**
One repo, one CI pipeline, one review queue, one deploy. Attractive for early
velocity while a single person writes everything.

**Option B — MCP servers live in the repo that owns the data; Australis
consumes them via the Registry.** Kora's server in `kora`, mark8ly's in
`mark8ly`, HMS's in `hms`. Australis holds only servers over its *own* data.

**Option C — No MCP. Each tenant exposes a bespoke REST connector and Australis
ships a per-tenant adapter.** Fewest moving parts today.

**Option D — Each product BFF embeds its own MCP client; Australis receives
pre-retrieved context in the request body.** Pushes retrieval to the edge.

## Decision

**Option B**, with six binding rules.

### D1 — An MCP server is owned by the repository that owns its data

A tool retriever reads a product's database through that product's schema,
authorisation model, and release cycle. Co-locating it with the consumer makes
every Kora schema change an Australis change, which fails the PRD's north-star
test verbatim: *"New tenant = connector + config + evals, zero engine-core
changes"* (§3). Under Option A that sentence cannot be true, because the
connector **is** an engine-core change.

Service boundaries follow data ownership. Kora owns Kora's tables; therefore
Kora owns the MCP server over them.

### D2 — Australis binds to MCP only behind the `ToolRetriever` port

MCP is an adapter, not an assumption. PRD §8 already names `ToolRetriever` as
one of the swappable ports; this ADR fixes MCP as its first and default
implementation, not its only possible one. A tenant arriving with a plain
signed-HTTP connector must be onboardable by writing an adapter, without the
engine core learning about it.

No `mcp` package import is permitted outside `internal/adapter/mcp/`. This is
enforced in CI, not by convention (see LLD §7).

### D3 — Servers are discovered through the Registry and pinned by digest

Australis never holds a hardcoded MCP endpoint. It resolves a server through
`GET /v0/search?kinds=MCPServer&view=stub`, fetches the exact object via the
returned `fetchPath`, and pins **both** digests before use:

- `registry_digest` — the signed catalog object;
- `artifact_digest` — the wheel or image actually deployed.

Resolving `latest` at request time is forbidden. A tenant's model policy and
its tool set must be reproducible for a given config revision, or eval results
(PRD §6.4) mean nothing.

### D4 — All invocation goes through AgentGateway

No direct pod-to-pod MCP calls, no `direct_access: true` in a route policy that
Australis consumes. The gateway is where per-tenant rate limits, mTLS, and
request identity live. Registry discovery grants **zero** authority: the
Registry is a catalog, never a proxy and never an authoriser
(`agentic-registry/README.md`, stated as a non-negotiable invariant). The MCP
runtime re-authorises every tool call default-deny regardless of what discovery
returned.

### D5 — Engine-owned MCP servers may live in `australis`

D1 cuts both ways. Australis owns its own operational data — eval suites, KB
ingestion state, per-tenant budget ledgers — and MCP servers over *those* belong
here, under `servers/<name>/`. Expected v1 set:

| Server | Purpose | Visibility |
| --- | --- | --- |
| `australis-evals` | run and report golden-set results | `internal` |
| `australis-kb-admin` | inspect ingestion/index state for a tenant | `internal` |

This is a small, bounded list. If it grows past roughly five servers, or if any
entry reads a *tenant's* data rather than Australis's own, D1 has been violated
and this ADR needs revisiting.

### D6 — v1 tools are read-only

PRD §3 non-goals: *"Not autonomous action/writes into product systems in v1 —
assist & guide; product/user executes."* Therefore every tool Australis invokes
declares `idempotency: not_applicable` and carries only read scopes. A tenant
publishing a write-effect tool is rejected at config-validation time, not
discovered at runtime.

This is a two-way door. When writes arrive, they arrive with idempotency keys
forwarded to the owning product API — the runtime already contracts for it.

## Consequences

### Positive

- The §19 primary success metric becomes measurable. Onboarding home-chef
  touches `home-chef` (server), the Registry (publish), and one Australis config
  row. Zero engine commits. If that turns out to be false, the abstraction
  leaked and we find out on tenant #2 rather than tenant #4.
- Blast radius is per-tenant. A malformed Kora tool breaks Kora's assistant.
  Under Option A it breaks the deploy that carries every tenant's server.
- HMS-class isolation stays reachable. A single-tenant/on-prem Australis (PRD
  §11) ships without any other tenant's connector code in the image — which
  Option A makes impossible without build-time surgery.
- Per-tenant credentials never enter this repo. Servers hold their own
  `credentialRef` to their own secret; Australis holds none of them.

### Negative — accepted

- **More repos, more CI.** Onboarding a tenant now spans two repos and a
  publish step. Mitigated by the authoring guide and `agentic init MCPServer`
  scaffolding; the cost is real and is the price of the isolation above.
- **A new failure mode: Registry unavailable.** Addressed by D3's cache and the
  degradation table below, not eliminated.
- **Cross-repo version skew.** A tenant can publish a tool whose schema
  fingerprint no longer matches Australis's expectation. Detected at resolve
  time via fingerprint comparison and surfaced as a config error, not a
  runtime 500.

### Dependency tiers and failure behaviour

Every dependency gets the sentence: *when X is down, the chat endpoint …*

| Dependency | Tier | When it is down |
| --- | --- | --- |
| Agentic Registry | **degradable** | serve from the identity-scoped resolution cache; refuse only if the cache is cold for that tenant, with `code=tool_retriever_unavailable` |
| AgentGateway | **critical (per tenant)** | that tenant's tool-KB answers fail; document-KB answers still work; response degrades to document-only with an explicit "live data unavailable" note |
| A tenant's MCP server | **degradable** | same as above, scoped to one tenant — bulkheaded connection pool means it cannot starve others |
| Postgres + pgvector | **critical** | document retrieval fails; chat returns 503 |
| Model provider | **degradable** | model router falls back per tenant policy (PRD §10) |
| Australis itself | **optional to the host product** | product BFF circuit-breaks, product works without the assistant (PRD §5.3) |

The rule that makes this hold: the tool-KB path and the document-KB path fail
independently. Neither is allowed to be in the other's synchronous critical
section.

### Cost

Marginal. Discovery is cached, so steady-state Registry QPS is near zero and it
adds no meaningful GCP spend. Each tenant MCP server is one small Knative
service scaling to zero between requests — at 5 peak RPS across all tenants,
this is a rounding error against model inference cost, which dominates the
per-answer budget by two or more orders of magnitude. Per-tenant budget
metering (PRD §7) meters model spend; tool-call spend does not need its own
meter in v1.

### Migration and rollback

There is nothing to migrate — Australis is pre-implementation. The rollback
path for any individual server is GitOps revert of the gateway route plus
Registry `PATCH .../status` to `deprecated`; the previous immutable version
stays resolvable until telemetry confirms no callers. Retiring a route before
callers drain is the one irreversible mistake here, so deprecate-observe-retire
is mandatory, never delete-in-place.

## Options rejected, and why

- **Option A (monorepo of all MCP servers in `australis`)** — makes PRD §3's
  north-star test unsatisfiable by construction, couples every tenant's release
  to the engine's, and puts four tenants' credentials and schemas in one blast
  radius. Rejected on the primary success metric.
- **Option C (bespoke REST connectors)** — cheaper for tenant #1 and more
  expensive for every tenant after. Forfeits the existing runtime's tool
  policy, bounded execution, tenant bulkheads, and manifest supply chain, all
  of which would have to be rebuilt inside Australis. Rejected as a false
  economy.
- **Option D (BFF-side retrieval)** — moves grounding out of the engine, so
  citation discipline and confidence gating (PRD §12) become each product's
  problem to reimplement. Directly contradicts §5.6, "config over code for
  domain specifics". Rejected.

## Open items this ADR does not close

- **Stack confirmation.** PRD §20 still lists Go as proposed. The LLD is
  written in Go against `github.com/modelcontextprotocol/go-sdk`; the SDK's
  maturity for a client-side consumer must be verified before Phase 1, and the
  version pinned in a follow-up ADR.
- **Tenant config surface.** Config-as-code versus admin API (PRD §20) is
  unresolved; the LLD assumes config-as-code and marks the assumption inline.
- **HMS on-prem Registry.** A single-tenant deployment with no reachable shared
  Registry needs a local catalog mode. Deferred with HMS, per PRD §4.
