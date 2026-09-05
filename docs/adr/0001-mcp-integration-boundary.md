# ADR-0001: MCP integration boundary and ownership

- Status: Superseded in part by [ADR-0004](0004-product-owned-mcp-connectors.md)
- Date: 2026-09-02
- Owner: Mahesh Sangawar
- Relates to: [PRD](../PRD.md) §5.2, §6.1, §7, §8, §11; [ADR-0002](0002-shared-brain-and-learning-flywheel.md)
- Supersedes: none

> ADR-0004 supersedes D1 and D5 only where this record requires all product
> connector source to live in Australis. The protocol boundary, independent
> artifacts, Registry discovery, Gateway invocation, read-only default, and
> explicit deployment selection remain current.

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

### One more requirement, added after the first draft

Australis is not only an engine. The stated intent is that it becomes the
**shared brain** of the product family: the place where connector code is
written, built and published, and — over time — where the family's assistant
behaviour is learned and improved rather than hand-tuned per product. That
intent changes the calculus below, because a learning system needs its training
signal in one place, and a connector fleet is one of the things it learns from.

The learning half is decided separately in
[ADR-0002](0002-shared-brain-and-learning-flywheel.md). This ADR settles only
where connector *code* lives and how Australis talks to it.

## Options considered

**Option A — All MCP servers live in `australis`; one repo builds and publishes
the fleet.** One place to work, one CI, one publish pipeline, one review queue,
and the connector fleet sits next to the engine that learns from it.

**Option B — MCP servers live in the repo that owns the data.** Kora's server in
`kora`, mark8ly's in `mark8ly`. Maximum isolation, maximum coordination cost.

**Option C — No MCP; bespoke REST connectors with per-tenant adapters.**

**Option D — Each product BFF embeds its own MCP client; Australis receives
pre-retrieved context.**

## Decision

**Option A**, under a shape that keeps its risks bounded:

> **Monorepo of source, polyrepo of artifacts.**
> One repository holds every server's code, build and publish pipeline. Each
> server is nonetheless an independent build unit, an independent image, an
> independent Registry object, and an independent deployment. Nothing about
> sharing a git repo is permitted to make two servers share a failure domain.

That distinction is the whole decision. The velocity benefit of Option A comes
from sharing a *workspace*; the isolation risk of Option A comes from sharing a
*deploy*. Taking the first without the second is available, and it is what the
seven rules below buy.

### D1 — Every MCP server is an independent build unit

`servers/<product>/<domain>/` is its own project: own dependency lockfile, own
container image, own tag, own Registry object, own `credentialRef`, own
ServiceAccount and SPIFFE ID, own `Deployment` and `ScaledObject`. There is no
"the Australis servers image".

CI is path-filtered: a change under `servers/kora/logs/` rebuilds and
republishes exactly that server. A Kora schema change cannot trigger a mark8ly
redeploy, and a broken Kora build cannot block an HMS release.

Shared code between servers is allowed only through `servers/_shared/`, which is
versioned and reviewed like any library. A server importing another server's
package is a build failure.

### D2 — Australis binds to MCP only behind the `ToolRetriever` port

Unchanged from the first draft, and unaffected by co-location. MCP is an
adapter, not an assumption. No `mcp` package import is permitted outside
`internal/adapter/mcp/`, and `internal/core/` may not import `servers/` at all —
the engine consumes servers over the wire like any other client, even though
their source sits in the same tree. Enforced in CI, not by convention (LLD §7).

This rule is what keeps Option A from quietly becoming a distributed monolith.

### D3 — Servers are discovered through the Registry and pinned by digest

Australis holds no hardcoded MCP endpoint, **including for servers built from
this repo**. Resolution goes through `GET /v0/search` → exact fetch → verify
signature → pin both `registry_digest` and `artifact_digest`. Resolving `latest`
at request time is forbidden.

Co-location makes it tempting to shortcut this with an in-process call or a
service-DNS constant. Don't. The pin is what makes a config revision
reproducible, which is what makes an eval result — and therefore the entire
learning loop of ADR-0002 — mean anything.

### D4 — All invocation goes through AgentGateway

No direct pod-to-pod MCP, no `direct_access: true`. The gateway owns per-tenant
rate limits, mTLS and request identity. The Registry is a catalog, never a proxy
and never an authoriser (an upstream invariant, not our choice). The MCP runtime
re-authorises every call default-deny regardless of what discovery returned.

### D5 — Per-server CODEOWNERS

`servers/kora/**` is owned by whoever owns Kora. Co-locating the code must not
transfer ownership of the domain knowledge in it. The person who knows that
`daily_log_summary` must exclude soft-deleted entries is the person who reviews
changes to it, wherever the file happens to live.

### D6 — v1 tools are read-only

PRD §3: *"Not autonomous action/writes into product systems in v1 — assist &
guide; product/user executes."* Every tool declares
`idempotency: not_applicable` and carries only read scopes. A write-effect tool
is rejected at config-validation time. Two-way door: when writes arrive they
arrive with idempotency keys forwarded to the owning product API.

### D7 — Deployment selection is per-server, not per-repo

A single-tenant or on-prem Australis (PRD §11, HMS-class) is assembled from an
explicit server list, not from "everything in `servers/`". Because D1 gives each
server its own image, an HMS deployment contains HMS's servers and no others.
This is the property that makes Option A survivable for a PHI tenant, and it
must be verified by a test that asserts the on-prem bundle's contents, not by
inspection.

## Consequences

### Positive

- **One place to work.** A developer adding a connector clones one repo, runs
  one toolchain, and follows one guide. At current team size this is the
  dominant cost, and Option A removes it.
- **The fleet sits next to the engine that learns from it.** Tool selection
  quality, eval outcomes and connector schemas are all inputs to ADR-0002's
  flywheel. Co-location makes that loop a local change rather than a
  cross-repo negotiation.
- **Consistency by construction.** One lint config, one manifest compiler
  version, one conformance testkit, one publish path. Under Option B these drift
  per repo and get discovered during an incident.
- **Isolation preserved where it counts.** D1 and D7 keep failure domains,
  images, credentials and deployments separate. Co-location is a source-tree
  fact, not a runtime fact.

### Negative — accepted, with mitigations

| Risk | Mitigation | Residual |
| --- | --- | --- |
| One repo carries every tenant's connector | D1 per-server build units; D7 per-server deployment selection | source-tree exposure: anyone with repo read sees every tenant's schema shape |
| Engine and connectors drift into a distributed monolith | D2 CI-enforced import ban, both directions | needs the check to actually run; it is in the required set |
| Blast radius of a bad shared library | `servers/_shared/` versioned and reviewed as a library | a `_shared` bug can reach every server — keep it small, or empty |
| Ownership dilution | D5 per-server CODEOWNERS | review latency when the owning team is busy |
| **Schema drift is detected late** | contract tests in this repo, run nightly against each product's staging API | **this is the real cost of Option A** — see below |

**The residual risk worth naming.** Under Option B a Kora schema change and its
connector change are one commit in one repo, so the product's own tests catch
drift. Under Option A they are two commits in two repos, and nothing in Kora's
CI knows `servers/kora/logs/` exists. The mitigation is a nightly contract-test
job in this repo that runs each connector against its product's staging API and
opens an issue on divergence. That job is not optional decoration; without it,
Option A's failure mode is a connector that silently returns wrong data, which
the assistant then cites confidently. Ship it in Phase 1, not Phase 3.

### Dependency tiers and failure behaviour

Unchanged by co-location. Every dependency gets the sentence: *when X is down,
the chat endpoint …*

| Dependency | Tier | When it is down |
| --- | --- | --- |
| Agentic Registry | **degradable** | serve from the identity-scoped resolution cache; refuse only if cold for that tenant, `code=tool_retriever_unavailable` |
| AgentGateway | **critical (per tenant)** | that tenant's tool-KB answers fail; document-KB answers still work; degrade to document-only with disclosure |
| A tenant's MCP server | **degradable** | same, scoped to one tenant — bulkheaded pool means it cannot starve others |
| Postgres + pgvector | **critical** | document retrieval fails; chat returns 503 |
| Model provider | **degradable** | model router falls back per tenant policy (PRD §10) |
| Australis itself | **optional to the host product** | product BFF circuit-breaks; product works without the assistant (PRD §5.3) |

The tool-KB path and the document-KB path fail independently. Neither is
allowed to be in the other's synchronous critical section.

### Cost

Marginal, and slightly lower than Option B. Discovery is cached, so steady-state
Registry QPS is near zero. Each server is one `Deployment` held at min 1 replica
and capped at 5 (see [tenancy-and-identity](../design/tenancy-and-identity.md)
§8); at ~5 peak RPS across all tenants the idle floor is a rounding error against
model inference, which dominates per-answer cost by two or more orders of
magnitude. Scale-to-zero would save that rounding error and spend a cold start
out of a 400 ms tool budget that is a hard contract — the wrong trade. One CI configuration instead of four is a real saving in
maintenance time, which at current team size is the scarcer resource.

### Migration and rollback

Nothing to migrate — Australis is pre-implementation. Per-server rollback is
GitOps revert of that server's route plus a Registry `PATCH .../status` to
`deprecated`; the previous immutable version stays resolvable until telemetry
confirms no callers. Deprecate → observe → retire, never delete-in-place.

If Option A turns out badly, the exit is cheap **because of D1**: a server that
is already an independent build unit with its own image and its own CODEOWNERS
moves to the product repo by `git filter-repo` and a CI file. That reversibility
is the reason D1 is a rule rather than a suggestion.

## Options rejected, and why

- **Option B (server per product repo)** — maximum isolation, but it splits the
  connector fleet away from the engine that is meant to learn from it
  (ADR-0002), multiplies toolchain drift by tenant count, and taxes every
  connector change with cross-repo coordination at a team size that cannot
  afford it. Its isolation advantages are recoverable under Option A via D1/D7;
  its coordination cost is not recoverable under Option B. Rejected.
- **Option C (bespoke REST connectors)** — forfeits the existing runtime's tool
  policy, bounded execution, tenant bulkheads and manifest supply chain, all of
  which would be rebuilt inside Australis. Rejected as a false economy.
- **Option D (BFF-side retrieval)** — moves grounding out of the engine, so
  citation discipline and confidence gating (PRD §12) become each product's
  problem to reimplement, and the flywheel loses its central observation point.
  Contradicts PRD §5.6. Rejected.

## Open items this ADR does not close

- **Tenant config surface.** Config-as-code versus admin API (PRD §20) is
  unresolved; the LLD assumes config-as-code and marks the assumption inline.
- **HMS on-prem Registry.** A single-tenant deployment with no reachable shared
  Registry needs a local catalog mode. Deferred with HMS, per PRD §4.
- **What the brain learns and from whose data.** Decided in
  [ADR-0002](0002-shared-brain-and-learning-flywheel.md).

## Implementation decision: Go MCP client

Phase 1 pins `github.com/modelcontextprotocol/go-sdk` at `v1.7.0`. Its
Streamable HTTP client supports stateless servers, `tools/list`, structured
tool output, caller-supplied HTTP clients and disabled transport retries. An
in-process integration test proves that Australis can initialize a stateless
server and retrieve both input and output schemas without network access.

Australis sets `MaxRetries: -1`; retry policy remains in the adapter rather
than being multiplied by the SDK transport. This closes the stack and SDK
spike. A protocol-only client is not justified.
