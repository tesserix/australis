# HLD — MCP knowledge connectors in Australis

- Status: Draft, tracks [ADR-0001](../adr/0001-mcp-integration-boundary.md)
- Date: 2026-09-02
- Scope: how a tenant's live/structured knowledge (PRD §6.1) reaches a grounded
  answer. Document-KB ingestion and retrieval are out of scope except where the
  two paths meet.
- Diagram: [`../diagrams/australis-architecture.drawio`](../diagrams/australis-architecture.drawio), pages 1–3

---

## 1. The one-paragraph version

Every connector in the family is written, built and published from this
repository: a server over a product's own data, compiled to a manifest and
published to the Agentic Registry. Australis then discovers that server by
capability, pins it by digest, and calls its tools through AgentGateway during
retrieval — over the wire, exactly as it would call a server it had never seen
the source of. The Registry is a catalog and is never on the request path; the
gateway is the request path and is never the authoriser; the MCP runtime
authorises every call default-deny. Sharing a repository is a source-tree fact,
not a runtime fact: each server is its own build unit, image and deployment.

## 2. System context

Five parties, four of which already exist.

| Party | Role | Status |
| --- | --- | --- |
| Product (Kora, mark8ly, home-chef, HMS) | owns the data and reviews its connector (CODEOWNERS) | exists |
| Product BFF | holds Australis credentials, circuit-breaks, degrades | per PRD §8 |
| **Australis engine** | retrieval, grounding, citations, routing, budgets | **to build** |
| **`servers/` — the connector fleet** | every product's MCP server, built and published here | **to build** |
| Agentic Registry | catalog, discovery, RBAC, signing | exists |
| AgentGateway | MCP data plane, routes, rate limits | exists |

See diagram page 1.

## 3. Ownership boundaries

Source lives together; runtime does not. That distinction carries the whole
design, so it is worth drawing twice.

**Source tree — one repository:**

```
 australis/
 ├─ internal/core/          ports · evidence · retrieval · compose
 │                          ↳ may NOT import servers/ or any MCP SDK
 ├─ internal/adapter/mcp/   the ONLY package that knows MCP exists
 ├─ internal/brain/         capture · corpus · eval · policy · promote
 ├─ servers/                the connector fleet — one build unit each
 │   ├─ kora/logs/              CODEOWNERS: Kora team
 │   ├─ mark8ly/catalog/        CODEOWNERS: mark8ly team
 │   ├─ home-chef/recipes/      CODEOWNERS: home-chef team
 │   └─ australis-evals/        CODEOWNERS: engine team
 └─ training/               offline only — never in the serving image
```

**Runtime — nothing is shared:**

```
 kora-logs image ──▶ own Deployment ──▶ own SA/SPIFFE ID ──▶ own credential ──▶ Kora's DB
 mark8ly-catalog image ──▶ own Deployment ──▶ own SA/SPIFFE ID ──▶ own credential ──▶ mark8ly's DB
 australis engine ──▶ separate image ──▶ talks to both over the gateway
```

- **Every server is an independent build unit** — own lockfile, image, tag,
  Registry object, credential, deployment (ADR-0001 D1). CI is path-filtered, so
  a Kora change rebuilds Kora's server and nothing else.
- **The product still owns its connector** through CODEOWNERS (D5).
  Co-locating the code does not transfer the domain knowledge in it.
- **Australis owns the port, not the protocol** (D2). One directory knows MCP
  exists, and `internal/core/` may not import `servers/` at all — the engine is
  a client over the wire even though the source sits in the same tree. This is
  what keeps a monorepo from becoming a distributed monolith.
- **The Registry owns no runtime** and the **gateway owns no policy**. Both are
  upstream invariants, not choices we get to relax.

The cost of this arrangement is stated plainly in ADR-0001: schema drift between
a product and its connector is no longer caught by the product's own tests. The
nightly contract-test job is the mitigation, and it ships in Phase 1.

## 4. Lifecycle of one connector

Six stages. Stages 1–3 run in this repo's per-server CI; 4–6 run in the engine
at request time. See diagram page 2.

**1. Author** — under `servers/<product>/<domain>/`, using
`tesserix-mcp-runtime`: typed
`callable_tool`, closed Pydantic input *and* output models, `ToolMetadata`
carrying scopes and effects. Tenant identity arrives on a verified
`CallContext` and is never a tool parameter.

**2. Compile** — `tesserix-mcp-manifest` turns one authoring document into two
byte-stable artifacts: a portable `server.json` and a
`registry.agentic.dev/v1alpha1` `MCPServer` envelope. The manifest is generated
from the code, so tool schema fingerprints cannot drift from the running server.

**3. Publish** — `agentic apply -f mcpserver.yaml`. Content-addressed by
SHA-256; re-applying identical bytes is a no-op, a new tag is a new immutable
revision, and the revision timeline is append-only. GitOps then reconciles an
AgentGateway route (`AgentgatewayBackend` + `HTTPRoute`) so the server becomes
reachable at `{gateway}/mcp/<name>`.

**4. Resolve** — at tenant-config load, Australis searches the Registry by
capability, fetches the exact object, verifies both digests and every tool
fingerprint against the tenant's pinned config, and caches the result
identity-scoped.

**5. Select** — during a chat turn the planner picks zero or more tools from
the resolved set using semantic metadata (`summary`, `when_to_use`, `not_for`).
Semantic metadata is a hint for *usefulness*; it confers no authority.

**6. Invoke** — the call goes over Streamable HTTP through AgentGateway with a
per-tenant deadline. Results carry citation metadata into answer composition,
where the grounding rules of PRD §12 apply unchanged: cited or silent.

## 5. Where the two knowledge kinds meet

PRD §9 requires document KBs and tool KBs to feed one grounding step. They
converge at a single `Evidence` list, not at the model prompt:

```
document KB ──▶ hybrid retrieval (dense ∪ FTS, RRF, rerank) ──┐
                                                              ├──▶ []Evidence ──▶ compose ──▶ cited answer
tool KB (MCP) ──▶ tool selection ──▶ gateway invoke ──────────┘
```

Both branches produce the same `Evidence` shape with a mandatory `Citation`.
This is what lets the confidence gate be a single decision rather than two,
and it is why a tool returning an untyped blob is unacceptable: a claim that
cannot be attributed to a source cannot ship.

**The two branches are independent failure domains.** Tool-KB failure degrades
an answer to document-only with an explicit disclosure. Document-KB failure is
critical. Neither runs inside the other's synchronous section.

## 6. Multi-tenancy

Per ADR-0001 and PRD §11:

- Shared schema, `tenant_id` on every row, enforced in the repository layer —
  never left to individual query authors.
- **Resolution cache is keyed by tenant.** A resolved server for tenant A is
  never visible to tenant B, even though the underlying Registry object may be
  identical. Registry search is itself tenant-filtered before any label
  selector is applied.
- **Per-tenant bulkhead** on the MCP client: a bounded connection pool and a
  bounded in-flight count per tenant. One tenant's slow server cannot consume
  the shared pool. This is the noisy-neighbour control PRD §14 asks for.
- Per-tenant rate limits live at the gateway and at the namespace waypoint;
  per-tenant budget caps stay in the engine's meter.
- **"Tenant" here means a product.** Inside one connector the isolation problem
  is per end user, and it has no framework behind it — that layer, the mesh
  identity beneath it, and the scaling and throttle profile are all in
  [tenancy, identity and scaling](tenancy-and-identity.md).

## 7. Non-functionals

Numbers, SLO, and the full dependency-tier table are in
[ADR-0001](../adr/0001-mcp-integration-boundary.md). The headline:
**peak ~5 MCP tool calls/sec**, p99 budget **400 ms** for the MCP hop inside a
**2.5 s** time-to-first-token target, engine SLO **99.5%** monthly because the
assistant is an enhancement and the host product survives without it.

Two consequences worth stating in the HLD:

- **400 ms is a hard contract, not an aspiration.** A tool that needs a
  multi-second aggregate must have the product pre-materialise it. Kora's
  Weekly Report (PRD §13) is exactly this: aggregate the week deterministically,
  then have the model summarise those numbers.
- **5 RPS is small.** No queue, no sharding, no separate tool-execution service.
  N stateless replicas and one Postgres. If a reviewer sees Kafka in a follow-up
  design for this path, the numbers have not changed and the design has.

## 8. Security posture

Four independent default-deny boundaries; none substitutes for another.

| Boundary | Enforced by | Grants |
| --- | --- | --- |
| Publication | Registry RBAC + tenant claims | who may publish into a namespace |
| Discovery | Registry visibility/RBAC pre-filter | which servers a caller may *see* |
| Routing | AgentGateway + mTLS | which requests reach a server |
| Invocation | MCP runtime per-tool scopes | which tool a verified identity may run |

Discovery is not authorisation. Search returns candidates; the exact version is
authorised again before activation, and the runtime authorises again at call
time. Credentials never appear in a manifest — only `credentialRef` pointing at
GCP Secret Manager, and the manifest compiler rejects secret-shaped keys
recursively.

Egress is declared per server. A tenant server that needs no outbound internet
declares `egress_hosts: []` and gets deny-all, which is the property that makes
an HMS-class PHI deployment reachable later.

## 9. What this design does not do

- **No writes** (ADR-0001 D6). Assist and guide; the product executes.
- **No autonomous tool chaining** beyond a bounded step count in v1. Depth
  limits are in the LLD.
- **No Australis-side embeddings over tool output.** Tool results are evidence
  for one turn, not corpus. Persisting them would silently create an
  un-governed copy of a tenant's live data.
- **No local MCP catalog.** HMS's on-prem no-Registry case is deferred with
  HMS itself.

## 10. Phasing

Maps onto PRD §17.

Full detail in [`../PLAN.md`](../PLAN.md). The connector-path summary:

| Phase | Deliverable | Proves |
| --- | --- | --- |
| 1 | `ToolRetriever` port + MCP adapter + digest-pinned resolution; `servers/kora/logs/` published and called; nightly contract tests | the seam works end to end |
| 2 | Per-tenant bulkheads, deadlines, degradation to document-only, document KB | the failure story is real, not aspirational |
| 3 | Eval harness + onboard home-chef with **zero** engine-core commits | PRD §19 primary metric |
| 4+ | Trace capture, T0 policy learning, adapters | see [ADR-0002](../adr/0002-shared-brain-and-learning-flywheel.md) |
| later | HMS: on-prem catalog mode, verified server-list bundle, clinical guardrails | the hard tenant |

Phase 3's onboarding test is the checkpoint that matters. If it needs an engine
change, stop and revisit ADR-0001 D2 rather than patching around it.
