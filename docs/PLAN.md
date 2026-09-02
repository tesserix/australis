# Australis — implementation plan

- Date: 2026-09-02
- Governed by: [PRD](PRD.md), [ADR-0001](adr/0001-mcp-integration-boundary.md), [ADR-0002](adr/0002-shared-brain-and-learning-flywheel.md)
- Diagram: [`diagrams/australis-architecture.drawio`](diagrams/australis-architecture.drawio)

---

## The shape of the plan

Two tracks that meet.

```
Track A — Serving          Phase 1 ──▶ 2 ──▶ 3 ─────────────────────────┐
(engine + connector fleet)                                              │
                                                                        ▼
Track B — Brain                    Phase 3 (capture) ──▶ 4 ──▶ 5 ──▶ 6  ✦ the flywheel turns
(measure, learn, improve)
```

Track A must reach Phase 3 before Track B can produce anything, because Track B
learns from what Track A observes and is measured by an eval harness Track A
does not have until then. **Nothing is trained before Phase 4** (ADR-0002 D7).

The ordering is the point. The common way this class of project fails is
training early against a small, dirty, self-labelled corpus, shipping something
that feels better, and having no instrument capable of detecting that it is
worse.

---

## Phase 1 — The seam works end to end

**Goal:** prove that a tenant's live data reaches a grounded, cited answer, and
that the pieces are pinned rather than wired.

| # | Deliverable | Done when |
| --- | --- | --- |
| 1.1 | Go MCP SDK spike; pin a version or choose the thin-client fallback | ADR appended with the decision and evidence |
| 1.2 | `internal/core/ports` — `ToolRetriever`, `Evidence`, `Citation` | core compiles with no adapter import |
| 1.3 | `internal/adapter/mcp` — discovery, resolver, client | conformance testkit green, no network in tests |
| 1.4 | Digest pinning + validations V1–V8 | each has a fail-closed golden fixture |
| 1.5 | CI layering checks (LLD §7) | build fails if `core` imports MCP or `servers/` |
| 1.6 | `servers/kora/logs/` — first MCP server, per-server image | published, routed, callable through the gateway |
| 1.7 | Per-server CI: path-filtered build, image push, manifest publish | a change under one server rebuilds only that server |
| 1.8 | **Nightly contract tests** against Kora staging | drift opens an issue automatically |
| 1.9 | `POST /chat` + SSE, single tool KB, mandatory citations | one real Kora question answered with a citation |
| 1.10 | Namespace + SA + waypoint policy per product (tenancy §3) | a direct pod call bypassing the gateway is refused by mesh authz, not by CIDR |
| 1.11 | `Deployment` + KEDA `ScaledObject`, min 1 / max 5 (tenancy §8) | connector scales on in-flight requests; no Knative, no scale-to-zero |
| 1.12 | Scope filter applied **after** the resolution cache (tenancy §7) | warm tenant cache + narrower-scope subject → narrower tool list |

Current checkpoint (2026-09-03): 1.1 and 1.2 are implemented; 1.3 has live
`tools/list` discovery but not Registry resolution or invocation; 1.4 has
canonical fingerprinting and V5–V8 validation but not Registry signature and
digest checks V1–V4; 1.5 is implemented. The generated Registry object carries
the output fingerprint but not the output schema, so V5 is evaluated against
the live `tools/list` contract during activation, then its fingerprint is
compared with the pinned Registry/config value.

**1.8 is not optional and does not move to a later phase.** It is the mitigation
for the one genuine cost of the monorepo decision (ADR-0001, "the residual risk
worth naming"): a connector whose product schema moved underneath it returns
wrong data that the assistant then cites confidently. Without the nightly job,
that failure is silent.

**1.10–1.12 belong in Phase 1, not later.** Retrofitting an isolation boundary
onto a running fleet means re-reviewing every connector already shipped; adding
it before the second connector exists costs one afternoon. The cache-ordering
rule in 1.12 is the same argument in miniature — it is free now and a
correctness bug to discover later (tenancy §7).

**Exit criteria:** a Kora question is answered from live data, with a citation,
through the gateway, from a digest-pinned server, reachable only from the
gateway's SPIFFE principal. Deliberately deferred: document KBs, multi-KB
mixing, proactivity, any learning.

---

## Phase 2 — It degrades honestly

**Goal:** make the failure story real rather than aspirational.

| # | Deliverable | Done when |
| --- | --- | --- |
| 2.1 | Per-tenant bulkheads (LLD §4.2) | one tenant's slow server cannot starve another — proven by test |
| 2.2 | Deadlines, single-retry rule, per-turn call cap | fault-injection suite green |
| 2.3 | Degradation to document-only with explicit disclosure | tool branch down → 200 + disclosure, never 500 |
| 2.4 | Resolution cache with soft/hard TTL | Registry down + warm cache → answers; cold cache → honest refusal |
| 2.5 | Error contract (LLD §6) wired end to end | BFF branches on `code`, never on message text |
| 2.6 | Document KB: ingestion, chunking, embedding, hybrid retrieval | both KB kinds feed one `[]Evidence` |
| 2.7 | Per-tenant budget metering | budget exhaustion degrades, does not fail |
| 2.8 | Throttle ladder rungs 1–4 wired (tenancy §9) | sustained overload from one tenant yields degraded 200s, never a 500 |
| 2.9 | Per-process bulkhead limits derived from the global ceiling | documented ceiling equals `per-process x maxReplicas`, and a test asserts it |
| 2.10 | Per-end-user isolation tests in the connector template (tenancy §11) | two subjects, same tenant, same tool → no cross-user rows |

**Exit criteria:** every row of ADR-0001's dependency table has a test that
proves the stated behaviour. A table without tests is fiction.

---

## Phase 3 — Measurement, and the second tenant

**Goal:** the two things that make everything after this possible — an
instrument, and proof that the abstraction holds.

| # | Deliverable | Done when |
| --- | --- | --- |
| 3.1 | Eval harness + Kora golden set | pass rate, citation coverage, refusal correctness all reported |
| 3.2 | `servers/australis-evals/` — harness as an MCP server | reachable from CI and tooling |
| 3.3 | Trace capture (brain LLD §9) — async, redacted, partitioned | I5, I6, I7 invariant tests green |
| 3.4 | Signal ingestion (§10) — thumbs, eval match, product outcome | signals join to traces by `TraceID` |
| 3.5 | **Onboard home-chef with zero engine-core commits** | git log proves it: connector + config + evals only |
| 3.6 | Corpus partitioning + D3 filter | I1 test green; SFT-eligible fraction reported |

**3.5 is the checkpoint that matters.** It is PRD §19's primary success metric.
If onboarding home-chef requires an engine change, stop and revisit ADR-0001 D2
rather than patching around it — the abstraction leaked and finding out at
tenant #2 is the entire reason for sequencing it here.

**Exit criteria:** two tenants live, quality is measured rather than asserted,
and traces are accumulating cleanly.

---

## Phase 4 — T0: the flywheel starts turning

**Goal:** cross-product learning, with no content shared and no model trained.

| # | Deliverable | Done when |
| --- | --- | --- |
| 4.1 | Metadata warehouse over traces | `(config → metric)` pairs queryable across tenants |
| 4.2 | T0 policy fitting (brain LLD §12) | retrieval k, RRF constant, rerank weights, thresholds fitted offline |
| 4.3 | Versioned policy bundles + per-tenant overrides | `policy/vN.yaml`, promotable and rollbackable |
| 4.4 | Promotion gate applied to policy changes | a policy bundle ships only if it beats the incumbent on pooled golden sets |
| 4.5 | Proactive digests — Kora Weekly Report (PRD §13) | queue-backed; a restart delays, never drops |

This is where "Australis learns from all the products" becomes literally true,
and it happens **without a single byte of content crossing a tenant boundary**.
A rerank weight tuned on mark8ly's catalog improving home-chef's recipes is real
transfer at zero privacy cost. It is also available a year or more before any
adapter is worth training, which is why it comes first.

**Exit criteria:** a measured quality gain on at least one tenant attributable
to a policy learned from a different tenant's metadata.

---

## Phase 5 — T2: the first adapter

**Goal:** cheaper and faster at equal or better quality, for one tenant.

| # | Deliverable | Done when |
| --- | --- | --- |
| 5.1 | Base model + serving substrate spike (Gemma size, Vertex vs vLLM) | cost and p99 measured, decision recorded |
| 5.2 | Training pipeline in `training/` | run reproducible from (base, snapshot digest, hyperparameters, seed) |
| 5.3 | Immutable dataset snapshots | adapter records the digest it was trained on |
| 5.4 | First Kora T2 adapter | trained on Kora traces only; I1 and I2 tests green |
| 5.5 | Shadow → canary → promote machinery (brain LLD §14) | no automatic promotion; decision recorded with the eval diff |
| 5.6 | Erasure path | delete traces → rebuild snapshot → retrain → promote, end to end |

**The economic test for this phase:** per-answer cost must fall while every
quality gate holds. If a self-hosted Gemma with a domain adapter cannot beat the
frontier model on cost at equal quality, T2 is not worth doing and ADR-0002
should be revisited rather than pushed through.

**Exit criteria:** Kora served by an Australis-trained adapter, cheaper, at
equal or better eval scores, with a working rollback.

---

## Phase 6 — T1 and the rest of the family

| # | Deliverable |
| --- | --- |
| 6.1 | Per-tenant consent for T1, defaulting off |
| 6.2 | De-identification pipeline, verified |
| 6.3 | T1 behaviour adapter across opt-in tenants; HMS excluded by construction (I4) |
| 6.4 | mark8ly onboarded; T2 adapter per live tenant |
| 6.5 | Adapter hot-swap on a shared base, cost measured |

---

## Deferred to HMS

Tracked, not scheduled — HMS is a design target that informs the architecture
now and integrates later (PRD §4).

| Item | Blocked on |
| --- | --- |
| Single-tenant / on-prem packaging, verified server-list bundle (ADR-0001 D7) | HMS leaving the docs stage |
| Local catalog mode (no reachable shared Registry) | same |
| Self-hosted MedGemma model policy | same |
| Clinical guardrails, human-in-the-loop, approved-KB-only citation | same |
| **Legal review before any T2 clinical training** | blocking; a trained clinical model may itself be a regulated function |

---

## What is deliberately not in this plan

- **Writes into product systems.** PRD §3 non-goal; ADR-0001 D6. Assist and
  guide; the product executes.
- **A forked or continue-pretrained base model.** ADR-0002 D2.
- **One shared model across tenants.** ADR-0002 rejects it as a defect, not a
  trade-off.
- **Learned grounding.** Citations and the confidence gate stay in code
  (ADR-0002 D4). This is what keeps a bad adapter from being dangerous.
- **A queue, sharding, or a separate tool-execution service for the MCP path.**
  At ~5 peak RPS the numbers do not support it (ADR-0001).

---

## The three checkpoints worth stopping at

| Phase | Checkpoint | If it fails |
| --- | --- | --- |
| 3.5 | home-chef onboards with zero engine commits | the abstraction leaked — fix ADR-0001 D2, do not work around it |
| 4 | a policy learned from tenant A measurably helps tenant B | cross-product learning is not real here; T0 may not be worth the warehouse |
| 5 | adapter is cheaper at equal quality | T2 does not pay for itself — stop at T0 and revisit ADR-0002 |

Each is a genuine decision point, not a milestone to be declared met. The value
of writing them down now is that they are cheap to fail against later.
