# ADR-0002: Australis as the shared brain — the learning flywheel

- Status: Proposed
- Date: 2026-09-02
- Owner: Mahesh Sangawar
- Relates to: [PRD](../PRD.md) §5.1, §6.2, §10, §11, §12, §14, §19; [ADR-0001](0001-mcp-integration-boundary.md)
- Supersedes: none

## Context

ADR-0001 settled where connectors live. This ADR settles the larger intent
behind putting them there: **Australis becomes the family's shared brain** — the
single point where every product's assistant behaviour is observed, measured,
and improved over time, rather than hand-tuned product by product.

The stated goal is "our own Australis model", trained from what the family
learns, getting cleaner, better and more performant with use. That goal is
achievable, but only if three things are pinned down first, because each has a
version that works and a version that destroys the product.

**What "our own model" means.** Not a foundation model trained from scratch —
that is a nine-figure exercise against a moving target and is not what anyone
here wants. It means **a small open base model with Australis-trained
adapters**: a Gemma-family base (already in PRD §10's provider list, already the
HMS/MedGemma path), plus LoRA adapters trained on the family's own
grounded-answer traces. The adapter is the proprietary asset. The base is
rented.

**What "learn from other products" can and cannot mean.** PRD §11 makes
per-tenant isolation a first-class guarantee, and PRD §4 sets HMS as the bar.
One weight set trained on Kora's food logs *and* HMS's clinical records is an
unrecoverable isolation failure — weights cannot be un-mixed, cannot be deleted
per-user, and cannot satisfy a DPDP or GDPR erasure request. So cross-product
learning must be split by what is actually being learned (see the three tiers
below). This is the load-bearing constraint of the whole design.

**What "cleaner, better, performant" must be measured as.** PRD §19 already
defines the metrics: golden-set pass rate, citation coverage, hallucination
rate, per-tenant cost. "Better" means those numbers moved. A model change that
cannot be shown to move them does not ship.

### Numbers

Planning assumptions, extending ADR-0001's. Stated so the design can be checked
against them rather than argued about.

| Quantity | Assumption | Note |
| --- | --- | --- |
| Turns/day, Kora at 12 mo | 15,000 | ADR-0001 |
| Turns/day, all four tenants at maturity | ~50,000 | Kora + mark8ly + home-chef + HMS |
| Trace size | ~8 KB | query, evidence refs, answer, citations, metadata |
| Raw trace volume | ~400 MB/day, ~150 GB/year | object store; Postgres holds the index only |
| **SFT-eligible fraction** | **~3%** | survives grounding + independent-signal filter (§D3) |
| SFT examples/month, per live tenant | ~13,500 | 15,000 x 0.03 x 30 |
| Examples needed for a useful LoRA | 1,000 – 50,000 | one month of one live tenant is enough |
| Training cost per adapter run | < $50, ~4 GPU-hours | LoRA on a 12B base |

Two conclusions follow directly, and they shape everything below:

1. **Data volume is not the constraint; data cleanliness is.** One month of one
   tenant produces more examples than a LoRA needs. The hard part is the 97%
   filter, not the 3%.
2. **Training is cheap; serving is not.** Adapter runs cost less than a rounding
   error. The economics of this project live entirely in inference, which is
   also where the "performant" goal pays off.

### SLO impact

The flywheel must not touch the serving SLO from ADR-0001 (99.5% monthly, p99
TTFT < 2.5 s). Capture is asynchronous and fire-and-forget; training is offline;
promotion is a config change. **If the entire learning subsystem is down, chat
is unaffected.** This is non-negotiable and is why capture is a queue write, not
an inline database write.

## Options considered

**Option 1 — Prompt-and-config only.** No training ever. Improve via persona,
guardrails, retrieval tuning and model choice. Cheapest, and genuinely most of
the available gains.

**Option 2 — One shared Australis model across all tenants.** Simplest mental
model, best cross-product transfer, and an unrecoverable isolation failure.

**Option 3 — Tiered learning: shared policy, shared behaviour, per-tenant
domain.** Split what is learned by what data it requires.

**Option 4 — Per-tenant models only, no sharing.** Safe and forfeits the entire
"learn across the family" premise.

## Decision

**Option 3**, staged, with promotion gates. Seven rules.

### D1 — Three learning tiers, split by the data each requires

This is the core of the ADR. Everything else enforces it.

| Tier | What is learned | Trained on | Shared across tenants? | HMS participates? |
| --- | --- | --- | --- | --- |
| **T0 — Policy** | model routing, tool selection, retrieval `k`, rerank weights, confidence thresholds | **metadata only**: which tool, latency, cost, eval pass/fail, citation coverage, refusal correctness. No tenant content, ever. | **Yes** | Yes — metadata carries no PHI |
| **T1 — Behaviour** | answer format, citation discipline, refusal style, honest-escalation phrasing | de-identified, consented traces from opt-in tenants | **Yes**, among opt-in tenants | **No** |
| **T2 — Domain** | tenant vocabulary, domain reasoning, product-specific grounding | that tenant's traces only | **No** — one adapter per tenant | Yes, trained and served entirely within HMS's own boundary |

**T0 is where "learn from other products" actually happens, and it is the tier
most likely to be underestimated.** Knowing that a rerank weight tuned on
mark8ly's catalog also helps home-chef's recipes is real transfer, requires no
content sharing, and is available years before any adapter is worth training.

**T2 adapters never merge.** Kora's adapter is trained on Kora data and served
only for Kora. There is no "combined" adapter, because combining is the one
operation that cannot be undone.

### D2 — Base model is rented; adapters are the proprietary asset

Gemma-family base (aligned with PRD §10's Vertex-hosted open-weights path and
the MedGemma requirement), served on Vertex or self-hosted vLLM. Australis owns
LoRA adapters, the training corpus, the eval suites, and the promotion history.
It does not own, fork, or continue-pretrain a base model.

Rationale: base models improve faster than we can, adapters port to a newer base
in one retraining run, and a forked base is a permanent maintenance liability
that buys nothing the adapters do not already buy.

### D3 — Train only on independently-signalled turns

**The single most important rule in this ADR.** A turn enters the training
corpus only if it carries a signal that did **not** come from the model itself:

- explicit human feedback (thumbs, correction, accepted suggestion);
- an eval golden-set match;
- a product-side outcome (the user logged the recommended meal, the merchant
  applied the suggested price);
- expert review, for tenants where that is affordable.

Turns with no independent signal are stored for analysis and **never** used as
training targets. Training a model on its own unlabelled output is model
collapse with extra steps: quality degrades smoothly, every internal metric
keeps looking fine because the metrics are self-referential, and the failure is
usually noticed by users first.

This rule is the reason the SFT-eligible fraction is ~3% and not ~90%, and that
gap is a feature.

### D4 — Grounding is structural, not learned

Citations, the confidence gate, retrieval, and refusal enforcement stay in
**code** (PRD §12, LLD §4). The model is never the thing that decides whether an
answer is sufficiently grounded.

Consequence, and it is the safety property that makes this whole ADR
acceptable: **a badly-trained adapter cannot fabricate a citation.** The worst a
bad adapter does is phrase a grounded answer poorly or fail the confidence gate
more often — both visible, both measured, both reversible by rolling the adapter
back. It cannot invent a source, because the source list is assembled before the
model is called and validated after.

Any proposal to move grounding into the model — "the fine-tuned model learns not
to hallucinate" — is rejected on sight. That is a probability, not a guarantee,
and PRD §5.1 asks for a guarantee.

### D5 — Promotion is gated, staged, and never automatic

A new adapter reaches production through:

```
train → offline eval vs incumbent → shadow (no user impact) → canary 5% → promote
```

Promotion requires **all** of:

| Gate | Threshold |
| --- | --- |
| Golden-set pass rate | ≥ incumbent, on that tenant's suite |
| Citation coverage on substantive claims | no regression, floor 99% |
| Refusal correctness (declines what it should) | no regression |
| p99 TTFT | ≤ incumbent |
| Cost per answer | ≤ incumbent, unless a pass-rate gain justifies it explicitly |

Any regression blocks promotion. There is no "net positive" override — a model
that answers better but cites worse is not better, it is more dangerous.

Rollback is a config change: the tenant's model policy points back at the
previous adapter version. Adapters are immutable and versioned, so rollback is
always available and always fast.

### D6 — Capture is asynchronous, redacted, consented, and expiring

- **Asynchronous.** Trace capture is a fire-and-forget queue write. It cannot
  add latency to a turn and cannot fail one.
- **Redacted at capture**, not at training time. PII detection and masking run
  before the trace is durable, so raw PII never lands in the corpus.
- **Consented per tenant.** T1 participation is explicit opt-in per tenant, in
  tenant config, defaulting to off. T2 is implied by being a tenant; T0 is
  metadata and requires no content consent.
- **Retention-bounded.** Raw traces 90 days; derived SFT examples 12 months;
  metadata indefinitely. Erasure requests delete traces and derived examples,
  and are honoured in the corpus at the next training run — which is why
  adapters must be retrainable from the corpus, never incrementally patched.

### D7 — Nothing is trained before Phase 4

Phases 1–3 capture, measure and evaluate. No adapter is trained until there is a
golden set that can prove an improvement and a corpus that passed D3's filter.

This ordering is deliberate. The common failure in this kind of project is
training early on a small, dirty, self-labelled corpus, shipping a model that
feels better, and having no instrument capable of noticing that it is worse. The
eval harness is the instrument, and it comes first.

## Consequences

### Positive

- Cross-product learning starts at **T0 in Phase 4**, years before any adapter,
  and needs no content sharing at all. The flywheel turns early.
- Cost and latency improve as T2 adapters let cheap self-hosted models handle
  traffic that today needs a frontier model. This is the concrete meaning of
  "more performant", and PRD §19's cost metric measures it.
- HMS stays reachable. Its adapter trains and serves inside its own boundary; it
  contributes T0 metadata and nothing else. No decision here has to be undone
  when HMS leaves the docs stage.
- Every tenant's improvements are auditable: adapter version, corpus snapshot,
  eval scores, promotion decision. PRD §14's audit requirement extends to the
  model itself.

### Negative — accepted

| Risk | Mitigation | Residual |
| --- | --- | --- |
| **Model collapse from self-training** | D3 independent-signal filter | needs discipline forever; a future shortcut here is the most likely way this project fails |
| Corpus contains PII | D6 redact-at-capture + retention + erasure | redaction is imperfect; assume some leakage and keep retention short |
| Adapter regresses subtly | D5 gates + shadow + canary | golden sets only measure what they contain; blind spots persist until someone writes the case |
| Feedback bias (users thumb-up confident answers) | weight eval and product-outcome signals above thumbs | thumbs remain a biased signal; do not let them dominate |
| **HMS regulatory surface** | HMS excluded from T0-content and T1 entirely; T2 stays on-prem | a trained clinical model may itself be a regulated function — legal review before HMS T2, not after |
| Serving N adapters costs more than one model | LoRA adapters share a base; hot-swap per request | real but small; revisit if tenant count passes ~10 |

### Cost

- **Training:** negligible. Under $50 per adapter run, a handful of runs per
  month.
- **Storage:** ~150 GB/year of traces in object store, plus a Postgres index.
  Tens of dollars a month.
- **Serving:** the only material cost, and it is expected to go **down**. The
  entire economic case for T2 is moving traffic from a frontier model to a
  self-hosted Gemma with a domain adapter. If that does not reduce per-answer
  cost while holding the eval gates, T2 is not worth doing and this ADR should
  be revisited.
- **Human:** the largest real cost is writing and maintaining golden sets. Budget
  it explicitly; it is the instrument everything else depends on.

### Migration and rollback

Nothing to migrate. Rollback at every level is a config change: adapter version
in tenant model policy, T0 policy version, or disabling capture. The one
irreversible action in this ADR is **training a shared adapter on mixed-tenant
content**, which D1 forbids structurally rather than by review — the corpus
builder cannot emit a mixed-tenant dataset because tenant partitioning is
applied before the dataset is assembled, not as a filter afterwards.

## Options rejected, and why

- **Option 1 (never train)** — a legitimate position and the correct one for
  Phases 1–3, which is why D7 adopts it temporarily. Rejected as a permanent
  answer because it forfeits the cost reduction of T2 and the cross-product
  transfer of T0, both of which are measurable and both of which are the stated
  point of a shared brain.
- **Option 2 (one shared model)** — unrecoverable isolation failure. Weights
  cannot be un-mixed or erased per-user, which makes PRD §11 undeliverable and
  HMS impossible. Rejected outright; not a trade-off, a defect.
- **Option 4 (per-tenant only)** — safe but forfeits T0, which is the cheapest
  and earliest-available transfer in the whole design and requires no content
  sharing whatsoever. Rejected as needlessly conservative.

## Open questions

- **Base model and version.** Gemma 3 family assumed; the specific size and the
  serving substrate (Vertex vs self-hosted vLLM) need a cost and latency spike.
- **Who writes golden sets.** D7 makes evals the critical path. Product teams,
  a shared role, or generated-then-reviewed? Unresolved, and it gates Phase 3.
- **Product-outcome signal plumbing.** D3's strongest signal requires products
  to report outcomes back. Needs a contract; likely one endpoint on the existing
  Australis surface.
- **T1 consent granularity.** Per tenant is decided. Per end-user within a
  tenant is not, and Kora's consumer-privacy posture may require it.
- **HMS legal review** before any T2 clinical training. Blocking for HMS only.
