# Tenancy, identity and scaling

- Date: 2026-09-02
- Governed by: [PRD](../PRD.md) §11, [ADR-0001](../adr/0001-mcp-integration-boundary.md), [ADR-0002](../adr/0002-shared-brain-and-learning-flywheel.md)
- Diagram: [`diagrams/australis-tenancy.drawio`](../diagrams/australis-tenancy.drawio)
- Companion to: [MCP HLD §6, §8](mcp-hld.md), [MCP LLD §3.3, §4.2](mcp-lld.md)

---

## 1. "Tenant" means two different things

The MCP HLD and PRD use *tenant* to mean **a product** — Kora, mark8ly,
home-chef, HMS. That is correct for the engine and wrong for everything below
it, because inside Kora's connector there is exactly one Australis tenant and
many thousands of end users.

Conflating the two produces a system that passes every tenant-isolation test and
still leaks between users. Three planes, three mechanisms:

| Plane | Isolates | Enforced by | Owner |
| --- | --- | --- | --- |
| Engine | product from product | vector namespaces, `tenant_id` in the repository layer, cache keyed by tenant, bulkheads, budgets, corpus partitioning | Australis |
| Identity | claim from assertion | JWKS-verified gateway JWT → immutable `CallContext` | gateway + `tesserix-mcp-runtime` |
| Connector | **user from user, role from role** | `ctx.subject` and `ctx.scopes` applied in the data layer | the connector author |

The third plane is the one with no framework behind it, and it is the one that
leaks. §6 is about it.

## 2. Four layers, none substituting for another

This extends the four default-deny boundaries in [HLD §8](mcp-hld.md) downward
into the mesh. Each layer answers a question the layer above cannot.

| Layer | Answers | Mechanism | Fails closed by |
| --- | --- | --- | --- |
| L0 mesh transport | is this encrypted, and between which workloads | Istio ambient, ztunnel, SPIFFE ID per ServiceAccount | mTLS required; no plaintext peer |
| L1 mesh authz | **which workload** may reach this path | waypoint `AuthorizationPolicy` on source principal | namespace-wide deny-all default |
| L2 identity | **which tenant, which subject, which scopes** | `GatewayJWTContextProvider`, JWKS, fixed issuer/audience/alg | no valid token → no `CallContext` |
| L3 authorisation | **which tool** this identity may invoke | `ToolPolicy` default-deny, fingerprint-bound rules | unruled tool is denied |
| L4 data | **which rows** this subject may read | repository-layer scoping, optionally Postgres RLS | see §6 — this one is on you |

The load-bearing sentence: **the mesh authenticates workloads, not users.**
`spiffe://cluster.local/ns/australis-system/sa/agentgateway` proves a particular
pod is calling. It cannot know whether that call is on behalf of Kora tenant or
of user 4471, because the tenant is a claim inside a token the mesh is merely
forwarding. L1 and L2 are not alternatives.

## 3. L0/L1 — Istio ambient

Ambient splits into ztunnel (L4, per-node, mTLS and SPIFFE identity) and
waypoint proxies (L7, per-namespace, HTTP-aware policy). Australis uses both,
and the waypoint replaces a control the runtime currently has to fake.

`tesserix-mcp-runtime` offers `trusted_proxy_cidrs`, but its own documentation is
explicit that this is a **source-address check only** and requires mesh
authorization plus NetworkPolicy to mean anything. A waypoint policy keyed on
the caller's SPIFFE principal is strictly stronger: it survives pod IP reuse and
cannot be satisfied by anything that merely lands in the right subnet.

### Namespace and identity layout

```
australis-system/     engine, agentgateway, registry client
  sa/australis-engine
  sa/agentgateway
australis-kora/       waypoint + Kora's connectors
  sa/kora-logs        → spiffe://cluster.local/ns/australis-kora/sa/kora-logs
  sa/kora-recipes     → spiffe://cluster.local/ns/australis-kora/sa/kora-recipes
australis-mark8ly/    waypoint + mark8ly's connectors
australis-home-chef/  waypoint + home-chef's connectors
australis-hms/        waypoint + HMS connectors (shared-cluster deployment only)
```

**One ServiceAccount per connector, not per namespace.** That gives each
connector its own SPIFFE ID, at exactly the granularity of its own
least-privilege database credential (ADR-0001 D1). Two independent
least-privilege axes, aligned — transport identity and data identity name the
same unit.

### Policy shape

Per product namespace, in `tesserix-k8s`, reconciled by ArgoCD:

- A deny-all `AuthorizationPolicy` as the namespace default.
- An allow rule on the waypoint: source principal
  `cluster.local/ns/australis-system/sa/agentgateway`, method `POST`, path
  prefix `/mcp/`. Nothing else reaches a connector.
- `PeerAuthentication` STRICT — no plaintext fallback.
- A `NetworkPolicy` per connector limiting egress to its own database and the
  hosts its manifest declares in `egress_hosts`. A connector declaring
  `egress_hosts: []` gets deny-all egress, which is the property that makes the
  HMS-class deployment reachable later (HLD §8).

### What must not happen

The waypoint can also validate the gateway JWT via `RequestAuthentication`.
Adding it is fine as an outer filter. **Removing the in-process validation is
not**, for two reasons:

1. A single misconfigured waypoint would become a total authentication bypass.
   The runtime would have no way to notice it was now trusting an unverified
   header.
2. HMS on-prem may ship as a single-tenant image onto a cluster with no Istio at
   all (PRD §15). The runtime has to be safe with nothing underneath it.

The mesh is defence in depth around an already-safe runtime, never the thing
that makes it safe.

### Cost

One extra hop through the namespace waypoint, low single-digit milliseconds,
against roughly 670 ms of unallocated headroom in the TTFT budget
(ADR-0001). Not material. ztunnel is L4 only, so per-tenant L7 rate limiting
belongs on the waypoint or in-process — never expect ztunnel to do it.

## 4. L2 — where identity actually comes from

Confirmed against `tesserix-mcp-runtime/docs/gateway-identity.md`:

- Exactly one bearer token per request, verified against a JWKS, with fixed
  issuer, audience, `kid` and lifetime, and a fixed algorithm — RS256, ES256 or
  EdDSA. Symmetric algorithms are rejected outright.
- Tenant comes from a configured claim (`tenant_claim="tenant_id"`).
- Authentication runs **before MCP body parsing**, so an unauthenticated caller
  never reaches tool dispatch.
- Forwarded headers and MCP `_meta` **may confirm the verified identity but can
  never create or replace it**.

The result is an immutable `CallContext` carrying tenant, subject and scopes.
Everything downstream reads from it and nothing may reconstruct it from request
content. This is the reason LLD validation V8 rejects any tool whose input
schema declares an identity-shaped property: the model must never be in a
position to name whose data it wants.

## 5. L3 — `ToolPolicy` is stricter than it looks

From `tesserix-mcp-runtime/docs/tool-policy.md`, three properties worth relying
on deliberately rather than discovering later:

- A tool present in the catalog with **no** policy rule is **denied**. Adding a
  tool does not expose it.
- A rule that omits `state` defaults to `experimental` — neither listed nor
  invocable. Publishing is a separate act from enabling.
- Rules bind to a `reviewed_fingerprint`. **Changing a tool's schema invalidates
  its rule**, so the tool becomes non-invocable until someone re-reviews it.
  Schema drift cannot silently widen a tool's reach.

Roles inside a tenant — HMS's clinician / patient / admin split (PRD §11) — ride
as scopes in the token and map to `allowed_scopes` on the rule. No new machinery
for the role case.

## 6. L4 — the layer with nothing behind it

The runtime hands a handler a verified `ctx.tenant_id` and `ctx.subject`. **It
has no way to know that the handler's SQL should filter by them.** Every
cross-user leak in a system shaped like this one lives here.

Four requirements on every connector:

1. **One choke point.** All data access goes through a repository constructed
   *with* the subject, never one that accepts a subject per query. If a handler
   can build a query without a subject, eventually one will.
2. **Never accept identity as a tool argument** — LLD validation V8, enforced at
   resolution time so a violating tool cannot even be published into a tenant's
   set.
3. **Postgres RLS as defence in depth** where the product's database supports
   it. The repository filters; the database refuses regardless.
4. **Least-privilege credential per connector.** `servers/kora/logs/` gets a
   role that reads Kora's log tables and nothing else. This is the payoff for
   one process per domain rather than one per product.

## 7. Cache before the filter, never after

Resolution has a per-tenant part and a per-subject part, and they have different
cache lifetimes:

```
JWT ──▶ tenant, subject, scopes
     ──▶ tenant config @ revision R          }  expensive, per-tenant
     ──▶ Registry fetch by pinned digest     }  cache: LRU 500, TTL 15m,
     ──▶ manifests → tool descriptors        }  key (tenant_id, config_revision)
     ──▶ filter by ctx.scopes                   cheap, per-subject, EVERY request
     ──▶ the tool list the model sees
```

**The scope filter must not be cached under a tenant-keyed entry.** If it is, an
admin's warm entry eventually serves a patient the admin's tool list. The cache
holds the pre-filter descriptor set only; the filter is a pure function applied
per request. This is a correctness rule, not an optimisation — LLD §3.3 gets an
explicit note.

This is also the answer to "how does a stateless engine know its tools". It does
not remember them; it re-derives them each request from three durable stores —
tenant config, the Registry (content-addressed, so a digest always resolves to
identical bytes), and each connector's own database. Statelessness means any
replica can rebuild the full picture from those three. A cold replica differs
from a warm one in latency only, never in answers, which is precisely what makes
horizontal scaling safe.

## 8. Deployment and scaling

**No Knative.** Standard `Deployment` + KEDA `ScaledObject` (which generates an
HPA underneath, so this is HPA *plus* usable triggers, not an alternative to it),
reconciled by ArgoCD from `tesserix-k8s`.

| Workload | min | max | Scale on |
| --- | --- | --- | --- |
| MCP connector (every one) | 1 | 5 | in-flight MCP requests per pod |
| Australis engine | 2 | 5 | in-flight requests per pod |
| Trace capture worker | 1 | 3 | buffer depth |

> The engine is set to min 2 rather than 1 deliberately: at min 1 a rolling
> update or node drain is a full outage for a service with a 99.5% monthly SLO.
> Connectors stay at min 1 because the engine already degrades honestly when one
> is unavailable (§9). Override if the cost matters more than the redundancy.

### Why KEDA rather than a CPU-target HPA

Connectors are IO-bound — they wait on Postgres. Under load, latency climbs
while CPU sits flat at 10–20%. A CPU-target HPA would therefore never scale the
thing that is actually hurting. KEDA's Prometheus scaler lets the trigger be the
signal that correlates with pain:

```
sum(mcp_inflight_requests{service="kora-logs"}) / count(up{service="kora-logs"})
target: 6
```

Scale-up is deliberately fast and scale-down deliberately slow — a zero-second
stabilisation window up, 300 s down — because adding a pod is cheap and flapping
under a diurnal peak is not.

### Sizing

Peak is ~5 MCP tool calls/sec across the *entire fleet* (ADR-0001). At a 400 ms
p99 tool budget, fleet-wide concurrency is ~2, spread across dozens of
connectors. Max 5 replicas per connector is therefore very large headroom, which
is the correct posture for a ceiling: it exists to absorb a bad afternoon, not to
be occupied.

### min 1 has a consequence worth naming

With `minReplicas: 1`, a node drain or a rolling update leaves that connector
briefly unreachable. A `PodDisruptionBudget` cannot fix this — `minAvailable: 1`
against a single replica blocks eviction entirely, which breaks node
maintenance instead.

That is acceptable because the engine's degradation path already covers it: the
tool branch goes down, the answer degrades to document-only with explicit
disclosure, and the response is a 200 with a disclosure rather than a 500 (HLD
§9, LLD §6). Any connector for which that is *not* acceptable must be set to
min 2 and say why in its `servers/<product>/<domain>/README.md`.

## 9. The throttle ladder

Four rungs, outermost first. Each exists because the one below it reacts on a
different timescale.

| # | Rung | Reacts in | Behaviour when exceeded |
| --- | --- | --- | --- |
| 1 | Waypoint L7 per-tenant rate limit | instant | 429 at the mesh; the pod never sees the flood |
| 2 | KEDA scale-out 1 → 5 | 30–60 s | more capacity |
| 3 | Per-tenant bulkhead in-process | instant | abandon after 50 ms, **never queue** (LLD §4.2) |
| 4 | Engine degradation | instant | `tool_budget_exhausted` → 200 + partial answer + disclosure |

Rung 3 is what covers rung 2's reaction window. KEDA cannot add a pod in under
half a minute; the bulkhead holds the line meanwhile and sheds load
deterministically instead of building a queue that turns a latency problem into a
timeout cascade.

**The ladder never terminates in a 500.** A tenant that overruns its share gets a
degraded, honest answer — never an error, and never another tenant's latency.

### Replicas multiply the bulkhead — read this before tuning it

Bulkhead counters are **per process**. `tesserix-mcp-runtime` admission counters
are per-process too (default 16 concurrent per tenant, max 64). So the real
fleet-wide ceiling for one tenant is:

```
effective ceiling = per-process limit x maxReplicas
```

At `maxInFlight: 8` and `maxReplicas: 5` that is **40**, not 8. Scaling out
therefore *weakens* the noisy-neighbour guarantee unless the per-process limit is
derived from the desired global ceiling:

```
per-process limit = desired global ceiling / maxReplicas
```

Every stated per-tenant ceiling in this repo must name which of the two it is.
LLD §4.2's `maxInFlight: 8` is per-process, and with max 5 replicas the global
figure is 40.

## 10. HMS, on-prem, with no mesh

The single-tenant HMS image (PRD §15) may land on a cluster with no Istio, no
KEDA and no Registry. Every control above must therefore survive its own absence:

| Layer | Shared cluster | HMS on-prem |
| --- | --- | --- |
| L0/L1 mesh | ztunnel + waypoint | absent — replaced by a single-tenant network boundary |
| L2 identity | gateway JWT | gateway JWT, unchanged |
| L3 `ToolPolicy` | default-deny | default-deny, unchanged |
| L4 data scoping | repository + RLS | repository + RLS, unchanged |
| Scaling | KEDA 1–5 | fixed replicas, no autoscaler |

L2 through L4 are identical in both, which is the test of whether the layering is
real. If any of them required the mesh, the on-prem deployment would be a
different security model wearing the same name.

## 11. Test matrix

Isolation claims are only worth what their tests are worth. Every row is a CI
test, not a manual check.

| Test | Asserts |
| --- | --- |
| Two subjects, same tenant, same tool | no cross-user rows — the one everybody forgets |
| Two tenants, same tool name | no cross-product bleed |
| Tool argument naming another subject | rejected at resolution (V8), not at runtime |
| Token with wrong audience, issuer or algorithm | rejected before body parsing |
| Symmetric-algorithm token | rejected |
| Direct pod call bypassing the gateway | refused by waypoint policy, not just by CIDR |
| Catalog tool with no policy rule | denied |
| Rule with `state` omitted | not listed, not invocable |
| Tool schema changed, fingerprint stale | not invocable until re-reviewed |
| Warm tenant cache, second subject with narrower scopes | narrower list — proves the filter is post-cache |
| Bulkhead saturated | abandoned at 50 ms, degraded 200, no queue growth |
| Connector scaled to 0 replicas | document-only answer with disclosure, never a 500 |

## 12. What this design does not do

- **No per-tenant cluster or node pool.** Namespace plus waypoint plus
  least-privilege credential is the boundary. A tenant needing hardware
  isolation is the HMS on-prem case, not a variation of the shared deployment.
- **No global rate limiter.** Rung 1 is a local per-pod limit at the waypoint.
  A distributed limiter is a state store on the request path, and at ~5 RPS it
  would cost more availability than it buys fairness.
- **No scale-to-zero.** Deliberately given up. min 1 spends idle capacity to keep
  cold starts out of a 400 ms tool budget that is a hard contract, not an
  aspiration.
- **No mesh-level tenant identity.** SPIFFE identifies workloads. Any design that
  starts minting a SPIFFE ID per tenant is trying to make L1 do L2's job and
  should be rejected in review.
