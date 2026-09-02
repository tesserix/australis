# HLD + LLD — the shared brain and the learning flywheel

- Status: Draft, implements [ADR-0002](../adr/0002-shared-brain-and-learning-flywheel.md)
- Date: 2026-09-02
- Reads with: [MCP HLD](mcp-hld.md), [MCP LLD](mcp-lld.md), diagram pages 5–7

---

# Part I — High-level design

## 1. The loop in one picture

```
                    ┌──────────────────────────────────────────────┐
                    │                                              │
   products ──▶ Australis serving ──▶ trace capture ──▶ corpus ──▶ training ──▶ adapters
   (MCP tools)      (grounded,          (async,          (filtered   (LoRA,      (versioned,
                     cited)              redacted)        by D3)      offline)    immutable)
                    ▲                                                              │
                    │                                                              │
                    └──────────── promotion gate ◀── eval harness ◀────────────────┘
                                  (shadow → canary → promote)
```

Five stages, one gate. Everything to the right of "trace capture" is offline: if
it all stops, serving is unaffected. That property is designed in, not hoped
for — capture is a fire-and-forget queue write (LLD §9).

## 2. What actually gets learned

Three tiers, split by the data each one needs (ADR-0002 D1). Confusing them is
the failure mode this whole design exists to prevent.

| | **T0 Policy** | **T1 Behaviour** | **T2 Domain** |
| --- | --- | --- | --- |
| Learns | routing, tool selection, retrieval k, rerank weights, thresholds | format, citation discipline, refusal style | tenant vocabulary, domain reasoning |
| From | metadata only | de-identified consented traces | one tenant's traces |
| Shared | across all tenants | across opt-in tenants | never |
| Artifact | a config bundle | a LoRA adapter | a LoRA adapter per tenant |
| HMS | participates | excluded | on-prem only |
| Available from | **Phase 4** | Phase 6 | Phase 5 |

**T0 is the tier people underestimate.** It is a set of numbers — not a model —
learned from "which tool was picked, did it help, what did it cost". It shares
no content, it needs no consent, it is available first, and it is where
"Australis learns across the family" is literally true. A rerank weight tuned on
mark8ly's catalog helping home-chef's recipes is real transfer at zero privacy
cost.

## 3. What "our own model" is

A rented base, an owned adapter stack:

```
  Gemma-family base (rented, swappable)
      + T1 behaviour adapter        ← shared, opt-in tenants
      + T2 <tenant> domain adapter  ← that tenant only
      + T0 policy config            ← not weights; routing and retrieval numbers
      ─────────────────────────────
      = "the Australis model" for tenant X
```

Australis owns the adapters, the corpus, the eval suites and the promotion
history. It does not fork or continue-pretrain a base (ADR-0002 D2), because
adapters port to a newer base in one retraining run and a forked base is a
permanent liability that buys nothing.

## 4. Where this sits relative to serving

The flywheel adds exactly one thing to the request path: a **capture hook**
after answer composition. Nothing else in ADR-0001's serving design changes.

```
chat turn ──▶ retrieval (doc KB ∪ tool KB) ──▶ compose ──▶ confidence gate ──▶ answer
                                                                 │
                                                                 └──▶ [capture]  (async, non-blocking)
```

The model router already exists in PRD §10. A promoted adapter is just another
entry in a tenant's model policy — the router does not learn a new concept, it
gets a new option.

## 5. Why a bad model cannot hurt much

ADR-0002 D4: **grounding is structural, not learned.** Retrieval, citation
assembly, the confidence gate and refusal enforcement live in code and run
around the model, not inside it.

So the blast radius of a bad adapter is bounded to: worse phrasing, more
frequent honest escalation, slower, or costlier. It **cannot** invent a source,
because the source list is assembled before the model is called and every
substantive claim is validated against it after. This is the property that makes
it safe to iterate on models at all, and it is why no proposal to move grounding
into the weights is acceptable.

## 6. Failure behaviour

| Component | Tier | When it is down |
| --- | --- | --- |
| Trace capture queue | **optional** | traces dropped, serving unaffected, gap recorded in corpus metadata |
| Corpus store | **optional** | capture buffers, then drops; no serving impact |
| Training pipeline | **optional** | no new adapters; incumbents keep serving |
| Eval harness | **optional to serving, blocking to promotion** | nothing promotes — correct behaviour |
| Adapter serving endpoint | **degradable** | model router falls back per tenant policy to the cloud model (PRD §10) |

Every row is optional or degradable. The learning subsystem has no critical
tier by construction, because ADR-0002's SLO clause forbids it from having one.

## 7. Metrics — what "better" means

PRD §19 already defines these; the flywheel makes them the promotion gate
(ADR-0002 D5) rather than a quarterly report.

| Metric | Direction | Gate |
| --- | --- | --- |
| Golden-set pass rate | up | must be ≥ incumbent |
| Citation coverage on substantive claims | up | no regression, floor 99% |
| Refusal correctness | up | no regression |
| p99 TTFT | down | ≤ incumbent |
| Cost per answer | down | ≤ incumbent unless justified |
| Corpus SFT-eligible fraction | up | health signal, not a gate |

The last row is a leading indicator: if the eligible fraction is falling, either
answer quality is falling or feedback plumbing has broken. Alert on it.

---

# Part II — Low-level design

## 8. Package layout

```
australis/
├─ internal/
│  ├─ core/                  serving — unchanged by this design except §9
│  ├─ adapter/mcp/
│  └─ brain/
│     ├─ capture/            trace hook, redaction, queue producer
│     ├─ corpus/             partitioning, D3 filter, dataset assembly
│     ├─ eval/               golden-set runner, scoring, comparison
│     ├─ policy/             T0 learned config: load, version, apply
│     └─ promote/            shadow, canary, gate evaluation, rollback
├─ servers/                  MCP fleet (ADR-0001)
│  └─ australis-evals/       engine-owned MCP server exposing the eval harness
└─ training/                 offline — not part of the serving image
   ├─ pipelines/             LoRA training jobs
   └─ recipes/               per-tier hyperparameters
```

`training/` is deliberately outside `internal/`. It never ships in the serving
image and it may be Python even if the engine is Go — this is the one place a
language split is justified, because the ML tooling lives there.

## 9. Capture

```go
// Emit is called after the confidence gate, on every turn, and must never
// block, error into, or slow the request path. A full buffer drops.
func (c *Capturer) Emit(t Trace) {
	select {
	case c.ch <- t: // buffered, size 4096
	default:
		c.dropped.Inc() // metric, not an error
	}
}
```

Rules, each with a reason:

| Rule | Reason |
| --- | --- |
| Fire-and-forget, drop on full buffer | ADR-0002's SLO clause: learning must never touch serving |
| Redact **before** the trace is durable | raw PII must never land in the corpus, not even briefly |
| Store evidence **references**, not evidence bodies | tool results are a tenant's live data; copying them creates an ungoverned replica (MCP HLD §9) |
| Stamp `tenant_id`, `config_revision`, adapter versions, all digests | reproducibility; a trace whose provenance is unknown is not trainable |
| Partition by tenant at write time | ADR-0002 D1 — mixing must be impossible, not merely discouraged |

### Trace shape

```go
type Trace struct {
	TraceID        string
	TenantID       string    // partition key — set at write, never derived later
	ConfigRevision string    // pins retrieval + tool set (MCP LLD §3)
	ModelPolicy    ModelRef  // base + adapters actually used
	Query          string    // redacted
	EvidenceRefs   []EvidenceRef // source + locator + digest, NOT content
	Answer         string    // redacted
	Citations      []Citation
	Confidence     float64
	Gated          bool      // did the confidence gate fire
	Latency        LatencyBreakdown
	Cost           CostBreakdown
	Signals        []Signal  // arrives later, out of band — see §10
	CapturedAt     time.Time
}
```

`EvidenceRefs` rather than evidence bodies is the single most important field
choice here. It keeps the corpus small, keeps a tenant's live data out of it,
and still allows exact re-derivation at training time by replaying retrieval at
the pinned `ConfigRevision`.

## 10. Signals — the D3 filter

Signals arrive **after** the turn, from different sources, and are joined to the
trace by `TraceID`.

```go
type SignalKind int
const (
	SignalHumanFeedback  SignalKind = iota // thumbs, correction, edit
	SignalEvalMatch                        // golden-set agreement
	SignalProductOutcome                   // user acted on the answer
	SignalExpertReview                     // human review verdict
)
```

**A trace is SFT-eligible only if it carries at least one signal that did not
originate from the model** (ADR-0002 D3). Concretely:

```
eligible(trace) =
     len(trace.Signals) > 0
  && not trace.Gated                     // gated turns are refusals, not exemplars
  && citationCoverage(trace) == 1.0      // never train toward an uncited claim
  && maxSignalWeight(trace) >= threshold
```

Signal weighting, because not all signals are equally honest:

| Signal | Weight | Why |
| --- | --- | --- |
| Expert review | 1.0 | strongest, scarcest |
| Product outcome | 0.9 | behavioural, hard to fake |
| Eval match | 0.8 | objective but only covers what the suite contains |
| Human thumbs-up | 0.4 | biased toward confident-sounding answers |

Thumbs are deliberately weighted low. Users reward fluency and confidence, which
is exactly the failure mode the grounding discipline exists to prevent. Letting
thumbs dominate would train the model toward assured-sounding answers — the
opposite of PRD §5.1.

## 11. Corpus assembly

```
partition by tenant  →  apply D3 filter  →  replay retrieval at ConfigRevision
                     →  de-identify (T1 only)  →  emit dataset snapshot (immutable, digested)
```

Partitioning happens **first**, before filtering. The dataset builder physically
cannot emit a mixed-tenant T2 dataset, because it is invoked per partition and
has no cross-partition read path. ADR-0002 D1 is enforced by the shape of the
code, not by a check that someone could remove.

Every snapshot is immutable and content-digested. An adapter records the digest
of the snapshot it was trained on; that is what makes an erasure request
tractable — delete the traces, rebuild the snapshot, retrain, promote. Adapters
are never incrementally patched, precisely so that this path always exists.

### Retention

| Data | Retention | On erasure request |
| --- | --- | --- |
| Raw traces | 90 days | deleted immediately |
| Dataset snapshots | 12 months | rebuilt without the subject at next run |
| Adapters | indefinite, versioned | retrained from a rebuilt snapshot |
| Metadata (T0) | indefinite | unaffected — carries no subject content |

## 12. T0 policy learning

T0 produces **numbers, not weights**. No ML serving involved.

```yaml
# policy/v14.yaml — learned, versioned, promoted through the same gate
retrieval:
  k_dense: 24
  k_keyword: 16
  rrf_constant: 60
  rerank_top_n: 8
tool_selection:
  min_semantic_score: 0.62
  max_calls_per_turn: 3
confidence:
  gate_threshold: 0.71
routing:
  prefer_cheap_below_complexity: 0.35
```

Fitted offline from metadata across all tenants by straightforward means —
grid search or Bayesian optimisation against the pooled golden sets. No content
is read; the optimiser sees only `(config → metric)` pairs.

Per-tenant overrides are allowed and expected: HMS will want a higher
`gate_threshold` than Kora. The shared policy is the default, not a mandate.

## 13. Training

| Parameter | Value |
| --- | --- |
| Method | LoRA / QLoRA |
| Base | Gemma-family, pinned per adapter |
| Rank | 16 (start), tuned per tier |
| Examples | 1k–50k |
| Compute | ~4 GPU-hours |
| Cost | < $50/run |
| Frequency | monthly per tenant, or on corpus delta > 20% |

Every run records: base model + version, snapshot digest, hyperparameters, seed,
and resulting adapter digest. A run that cannot be reproduced from that record
is a bug, not an inconvenience — reproducibility is what lets you diff two
adapters when one regresses.

## 14. Evaluation and promotion

### Eval harness

Runs a tenant's golden set (PRD §6.4) against a candidate and the incumbent
under identical retrieval and config. Exposed internally as the
`australis-evals` MCP server (ADR-0001 D5), so the same harness is reachable
from tooling and from CI.

```go
type EvalResult struct {
	PassRate         float64
	CitationCoverage float64
	RefusalCorrect   float64
	P99TTFT          time.Duration
	CostPerAnswer    float64
	PerCase          []CaseResult // for diffing two runs case by case
}
```

`PerCase` matters more than the aggregate. Two adapters with identical pass
rates can fail entirely different cases, and the diff is what tells you whether
a change is an improvement or a lateral move.

### Promotion state machine

```
 candidate ──▶ offline eval ──▶ shadow ──▶ canary 5% ──▶ promoted
      │             │             │            │
      └─────────────┴─────────────┴────────────┴──▶ rejected (any gate fails)
                                                        │
                                        promoted ◀──────┴──▶ rolled back
```

| Stage | Traffic | Duration | Exit condition |
| --- | --- | --- | --- |
| Offline eval | none | minutes | all §7 gates pass vs incumbent |
| Shadow | mirrored, output discarded | 24 h | no latency or cost regression at real traffic shape |
| Canary | 5% | 72 h | live metrics hold; no citation-coverage drop |
| Promoted | 100% | — | — |

**No automatic promotion.** The final step is a human decision recorded with the
eval diff, because the gates measure what the golden set contains and a person
should look at what it does not.

Rollback is a tenant model-policy edit pointing at the previous adapter version.
Adapters are immutable, so rollback is always available and takes effect on the
next request.

## 15. Isolation enforcement

The rules that must be tested, not merely documented:

| # | Invariant | Test |
| --- | --- | --- |
| I1 | corpus builder cannot read across tenant partitions | two tenants, assert dataset contains only one partition's traces |
| I2 | T2 adapter is served only to its own tenant | request as tenant B with tenant A's adapter configured → refused |
| I3 | T1 corpus excludes non-consenting tenants | flip consent off, rebuild, assert absence |
| I4 | HMS traces never enter T1 | assert by construction and by test |
| I5 | evidence bodies never persist to the corpus | schema assertion on the trace writer |
| I6 | redaction runs before durability | inject a known PII pattern, assert it is absent from storage |
| I7 | capture failure never fails a turn | fault-inject a full buffer and a dead queue; assert 200 and correct answer |

I7 is the one that protects the SLO, and I1 is the one that protects the
company. Both belong in the required CI set.

## 16. Open implementation questions

1. **Base model and serving substrate.** Gemma size and Vertex vs self-hosted
   vLLM need a cost/latency spike before Phase 5.
2. **Golden-set authorship.** ADR-0002 D7 makes evals the critical path and
   nobody owns them yet. This gates Phase 3.
3. **Product-outcome plumbing.** The strongest D3 signal needs products to
   report back; likely one endpoint on the existing Australis surface. Needs a
   contract.
4. **De-identification method for T1.** Rule-based redaction is the assumed
   starting point; whether it is sufficient for a consumer-privacy posture is
   unverified.
5. **Adapter hot-swap cost.** Serving N LoRAs on one base is standard, but the
   per-swap latency at our traffic shape is unmeasured.
