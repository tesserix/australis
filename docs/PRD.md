# Australis — Product Requirements Document

**Status:** Draft v0.1 · Design / pre-implementation
**Date:** 2026-07-28
**Owner:** Mahesh Sangawar

---

## 1. Summary

Australis is a **multi-tenant, grounded-assistant engine**. Product teams integrate it to give their application an on-brand AI assistant that answers from *that product's own knowledge* — both document corpora and live/structured data — with **mandatory citations and no fabrication**. The engine owns the hard, reusable machinery (retrieval, ranking, grounding, isolation, model routing, budgeting, caching, memory, proactive digests); each product supplies only a thin, declarative integration.

The bet: the assistant machinery is ~80% the same across products; the differences are *configuration and connectors*, not code. Build the engine once; every product ships an assistant by plugging in.

## 2. Problem & Motivation

Each product in the family will independently want an AI assistant grounded in its own data (Kora: coach a user from their food logs; mark8ly: help merchants/shoppers from catalog & orders; home-chef: recipes & planning; HMS: clinician/patient support from records + medical knowledge). Building that stack N times — RAG pipeline, grounding, citations, per-tenant isolation, model routing, budget metering, safety guardrails, eval harness — is wasteful and inconsistent. Australis centralizes the machinery so products focus on their domain (connectors, persona, rules, evals), and the assistant experience stays consistent and trustworthy across the family.

## 3. Goals & Non-Goals

### Goals
- A single engine multiple products integrate, each with **its own knowledge base(s), model policy, rules, and evals**.
- **Grounded & cited by construction** — every substantive claim traces to a retrieved source; the engine prefers "I don't know / here's who to ask" over guessing.
- **New tenant = connector + config + evals, with zero engine-core changes.** This is the north-star success test.
- Support both **document knowledge** (files/corpora) and **structured/live-data knowledge** (a product's DB/API exposed as tools).
- **Per-tenant model policy**, including self-hosted open models for data-residency/PHI.
- Deployable **shared-multitenant** (consumer products) *and* **single-tenant / on-prem** (HMS-class tenants).
- **Graceful degradation:** if Australis is down, the host product still fully works; it just loses the assistant.

### Non-Goals (for now)
- Not a general chatbot platform for arbitrary external customers (family-internal tenants first).
- Not the real-time, latency-critical path inside a product (e.g. Kora's capture→food-resolution stays in Kora).
- Not Otto (the infra/SRE-automation assistant) — see §16.
- Not autonomous action/writes into product systems in v1 — **assist & guide; the product/user executes.**

## 4. Consumers / Tenants (roadmap)

| Tenant | Domain | Status | Role in Australis |
|---|---|---|---|
| **Kora** | Nutrition coaching | Built (UI+backend ready) | **Tenant #1** — first real integration + proving ground. Low-stakes, well-understood. |
| **home-chef** | Recipes / meal planning | Prod-ready | Fast-follow consumer tenant. |
| **mark8ly** | Ecommerce (merchant + shopper) | Prod-ready | Fast-follow consumer tenant. |
| **HMS** | Hospital management | Docs stage | **Design target** — informs architecture now (isolation, model policy, guardrails, on-prem), integrates later. Sets the hard bar. |

**Sequencing principle:** *Design for the hardest tenant (HMS), build with the easiest (Kora).* Reserve the hard seams as first-class extension points up front; implement only what Kora needs first; let HMS fill its slots when it leaves docs stage.

## 5. Core Principles

1. **Grounded or silent.** Answers cite retrieved sources; low-confidence → honest escalation, never a fabricated fact. Encoded structurally (the model never invents numbers the data doesn't support).
2. **New tenant = config + connector, zero core changes.** If onboarding mark8ly/home-chef forces an engine change, the abstraction leaked.
3. **Enhancement, not dependency.** Australis is always off the host product's critical path and degrades gracefully.
4. **Design for the hardest tenant, implement incrementally.** HMS's requirements shape the seams; we don't build them all up front.
5. **Isolation is first-class**, not an afterthought — especially across sensitive, heterogeneous domains.
6. **Config over code for domain specifics.** Domain smarts (persona, guardrails, output shape, model choice, evals) are per-tenant configuration; the engine is generic machinery.

## 6. The Tenant Contract — what a product registers

A product integrates by registering four things and nothing more:

### 6.1 Knowledge sources (one or more, mixed)
- **Document KB** — a corpus the product provides (help docs, policies, domain articles). Australis ingests → chunks → embeds → indexes.
- **Structured / live-data KB (tool retriever)** — the product exposes a read-only connector (an API/tool) over its own data (Kora's user logs & targets; mark8ly's catalog/orders; home-chef's recipes). Australis calls it as a tool at query time. **This is essential** — for several tenants the "knowledge" is live per-user data, not a document corpus.

### 6.2 Model policy
- Which model per task (chat / embed / rerank / classify), with **fallback chains**.
- Supports hosted APIs (Gemini, OpenAI-compatible) *and* **self-hosted open models** (e.g. MedGemma in a hospital VPC) via an OpenAI-compatible/Vertex adapter.
- Embedding model is pinned per-tenant (embedding spaces cannot be mixed — see §9).

### 6.3 Config & rules
- Persona/voice, system prompt, **guardrails/refusal policy**, output contract (schema), budget caps, rate limits.

### 6.4 Evals
- A golden set (Q → expected grounded behavior) so "good" is measurable per domain and regressions are caught.

## 7. Engine Responsibilities

Australis owns everything not in §6:
- **Ingestion & indexing** for document KBs (content-hash incremental, pluggable chunkers).
- **Retrieval** — hybrid (dense vector ∪ keyword), reciprocal-rank fusion, reranking; tool-retriever invocation for structured KBs.
- **Grounded answer composition** with mandatory citations and confidence gating.
- **Per-tenant isolation** — namespaces, keys, budgets; no cross-tenant leakage.
- **Model routing & fallback** (per §6.2 policy).
- **Budget metering, caching, thread/conversation memory.**
- **Proactive digests** (scheduled/event-driven; see §13).
- **Observability & audit** (traces, citations, decisions).

## 8. Architecture Overview

```
Product UI ──▶ Product BFF (per-product wrapper) ──▶ Australis API (HTTP + SSE)
                                                        │
                    ┌───────────────────────────────────┼───────────────────────────────┐
                    ▼                     ▼               ▼                ▼               ▼
             Tenant/Config         Retrieval        Model Router     Guardrails      Proactive
             & Model Policy     (docs + tools,      (per-policy,     (per-tenant)    scheduler
                Registry         rerank, RRF)        fallback)
                    │                     │
                    ▼                     ▼
             Postgres + pgvector (per-tenant namespaces)   Redis (cache)   Object store (docs)
```

- **Integration pattern:** products never call the engine directly from the client. A thin **per-product BFF** holds the product's Australis credentials, applies timeouts/circuit-breaking, and translates between product and engine. This keeps the client dumb and the failure handling local (degrade gracefully).
- **Contract:** a small, stable HTTP + **SSE (streaming)** surface — `POST /chat`, `GET /chat/stream`, thread/session endpoints, plus tenant/KB admin endpoints. Versioned.
- **Ports & adapters:** `ModelProvider`, `EmbeddingProvider`, `Reranker`, `KnowledgeStore`, `ToolRetriever`, `Cache`, `Meter` are interfaces with swappable implementations — the mechanism behind "config, not code."

## 9. Knowledge & Retrieval

- **Two KB kinds, one retrieval flow:** document KBs (embedded corpus) and tool KBs (live-data connectors) both feed the grounding step; a query can draw from either or both.
- **Per-tenant vector namespaces are mandatory.** Because tenants may use different embedding models (Kora vs HMS), their vectors live in different spaces/dimensions and must never share an index. Namespacing also enforces isolation.
- **Hybrid retrieval:** dense (pgvector, HNSW) ∪ keyword (Postgres FTS), fused (RRF), then reranked, top-k, with mandatory citation metadata carried through to the answer.
- **Ingestion** (document KBs): watch/receive source → content-hash diff (index only what changed) → pluggable chunker per content type → embed → upsert. Idempotent re-runs.

## 10. Multi-LLM Model Policy

- Each tenant declares a **model policy**: task → model + fallback chain. The router selects and falls back on error/latency-budget breach (Kora's existing router is the seed).
- **Provider adapters** cover: Gemini (default cloud), OpenAI-compatible endpoints (incl. NVIDIA/Groq/self-hosted vLLM/Ollama), and Vertex-hosted open weights.
- **Self-hosted for governance:** HMS-class tenants can run an open model (MedGemma/Gemma) **inside their own VPC/on-prem** so PHI never leaves the boundary. Model choice is therefore as much a *data-governance* decision as a capability one.
- **Capability awareness:** adapters normalize (or the policy accounts for) differences in context window, tool-calling, and structured-output support across models.

## 11. Multi-Tenancy & Isolation

- **Two deployment topologies from one codebase:**
  - **Shared multi-tenant** — Kora/home-chef/mark8ly share an engine deployment with per-tenant namespaces, keys, budgets.
  - **Single-tenant / on-prem** — HMS-class tenants get an isolated deployment (own DB, own model, inside their boundary). Config-driven, same code.
- **Isolation guarantees:** per-tenant KB namespaces, per-tenant credentials & budgets, retrieval scoped to the tenant, no cross-tenant reads. For the hardest tenants, isolation may extend to schema/DB-per-tenant.
- **Access scopes** within a tenant (e.g. clinician vs patient vs admin for HMS) are enforced per request.

## 12. Grounding, Citations & Guardrails

- Every substantive answer carries **citations** (source + locator). No citation-worthy claim ships ungrounded.
- **Confidence gating:** below threshold, the engine escalates honestly ("I don't have that; here's who/what can help") instead of guessing.
- **Per-tenant guardrail modules:** refusal policy, disallowed-topic handling, and output contracts are configuration. HMS gets strict clinical guardrails (no diagnosis/prescription beyond cleared scope, human-in-the-loop, citation-to-approved-medical-KB-only, full audit); Kora gets "no medical advice, ground macros in the user's real numbers."
- **Regulatory note:** clinical decision support can be a regulated function in some jurisdictions — HMS guardrails and audit are a compliance surface, not a nicety.

## 13. Proactivity

Beyond request/response, Australis supports **proactive digests** reusing the same grounded machinery, triggered on-demand, on a **schedule** (cron per tenant), or by **events**. First use: **Kora's Weekly Report** — aggregate the week's stats deterministically, have the model summarize *those numbers* into 2–3 grounded takeaways + one focus for next week. Proactive work is queue-backed (a restart delays a digest, never drops it).

## 14. Non-Functional Requirements

- **Resilience / SPOF:** the engine is a single *logical* service but horizontally scaled/stateless (N replicas, multi-AZ, health-checked). The primary mitigation is architectural — **degrade, don't fail** (assistant down ≠ product down), plus BFF circuit-breaker/timeout/serve-last-good, and provider fallback. The real multi-tenant risk to engineer against is **noisy-neighbor** (per-tenant quotas, isolated queues, budget caps), not hardware failure.
- **Stateful SPOF:** the Postgres+pgvector store (KB + memory) needs real HA (primary+replica, PITR backups) and the isolation model above.
- **Security & compliance:** per-tenant isolation; PHI/data-residency for HMS (HIPAA / GDPR-health / India DPDP); auditability of retrieval + answer + decisions; secrets never indexed.
- **Performance:** streaming (SSE) first-token latency matters for chat; proactive/batch tolerant of higher latency. Real-time product-core paths are *not* routed through Australis.
- **Observability:** traces, per-answer citation/decision logs, per-tenant cost/usage, eval scores over time.
- **Budget/cost:** per-tenant budget metering & caps (Kora's meter is the seed); model routing keeps cost within policy.

## 15. Tech Stack (proposed)

- **Language:** Go 1.26 (reuses Kora's `ai` package — provider router, budget meter, cache, resolver; one language across the family, team fluency, fast to ship). Python/LangGraph was considered and set aside: no inherited Python code, and the agent complexity here (grounded RAG + tool-calling + budget + digests) doesn't require it. Revisit only if deep multi-agent reasoning becomes a near-term requirement.
- **Runtime/deploy:** GCP — Knative (shared multi-tenant), plus a single-tenant/on-prem deployable image (HMS).
- **Datastore:** Postgres 15 + **pgvector** (HNSW), per-tenant namespaces; object store for documents; **Redis** for cache.
- **LLM providers:** Gemini (default), OpenAI-compatible (NVIDIA/Groq/self-hosted), Vertex-hosted open weights (MedGemma/Gemma). Reranker: pluggable.
- **Transport:** HTTP + SSE; per-product BFF integration.

## 16. Relationship to other efforts

- **Otto (Sam's, `../otto`)** — a *separate* infra/SRE-automation assistant (internal platform-ops, GitOps corpus, Python/Azure). Different audience, domain, trust model, and stack. **Australis stays independent** — its own repo, stack, and roadmap. At most they trade patterns/learnings, not a runtime or library.
- **Kora's `ai` package** — the seed for Australis's engine (provider routing, budget metering, cache, pgvector retrieval, the "no fabricated numbers" discipline). Australis generalizes and extracts this; Kora keeps its **real-time capture→food-resolution** in-process (latency-critical) and calls Australis only for the assistant/coaching path.

## 17. Phased Roadmap / Decomposition

This is a multi-phase platform, not a single spec.

1. **Engine skeleton + contract** — tenant/product registry, config + model-policy surface, chat HTTP+SSE contract, provider/model ports, per-tenant isolation model, budget metering.
2. **Knowledge layer** — pluggable retrievers: document-KB (ingest→chunk→embed→pgvector→hybrid+rerank) and structured-data/tool-KB.
3. **Grounded answers + guardrails** — citation-mandatory composition, confidence gating, per-tenant guardrail modules, output contracts.
4. **Tenant #1: Kora** — Kora data connector + persona/config + first surface (Weekly Report / coaching), via a Kora BFF.
5. **Proactive layer** — scheduled digests (Kora Weekly Report first).
6. **Later** — HMS-grade hardening (single-tenant/on-prem deploy, self-hosted MedGemma, clinical guardrails, audit) + home-chef & mark8ly onboarding.

## 18. First Slice (MVP)

**Phases 1 + minimal 2 + 4:** an engine skeleton exposing one grounded Q&A path over **one KB type** (start with Kora's structured/tool KB — its logs/targets — since that's what makes a coach smart), with Kora as the first registered tenant, proving the **tenant / config / model-policy / isolation seams end-to-end**. Explicitly deferred: multi-KB mixing, document ingestion polish, proactivity, HMS hardening. The MVP exists to validate the contract and the "new tenant = config + connector" test — not to be feature-complete.

## 19. Success Metrics

- **Onboarding cost:** tenant #2 (home-chef or mark8ly) integrates with **only** connector + config + evals — zero engine-core changes. (Primary.)
- **Groundedness:** eval golden-set pass rate ≥ target; citation coverage on substantive claims ~100%; measured hallucination rate near zero.
- **Resilience:** host product unaffected when Australis is down (verified degradation).
- **Cost:** within per-tenant budget policy; no cross-tenant budget bleed.
- **Kora value:** the Weekly Report / coaching surface ships and is used.

## 20. Open Questions / Decisions Pending

- **Name:** Australis ✅ (decided).
- **Stack:** Go (proposed §15) — confirm.
- **KB registration UX:** how a product declares a tool retriever (contract shape, auth to the product's data) — needs its own mini-design.
- **Reranker choice** for a Go/GCP world (hosted rerank vs. embed-only + heuristic).
- **Tenant onboarding & admin surface** (config-as-code vs. an admin API/UI).
- **HMS specifics** (deferred): on-prem packaging, compliance certification path, clinical-guardrail authority — revisit when HMS leaves docs stage.
- **Memory model** (per-user thread persistence, retention, privacy) — detail in Phase 1.

## 21. Glossary

- **Tenant / product** — an app integrating Australis (Kora, mark8ly, home-chef, HMS).
- **Knowledge base (KB)** — a tenant's grounding source: a document corpus and/or a live-data tool retriever.
- **Model policy** — a tenant's per-task model + fallback configuration.
- **BFF** — per-product backend-for-frontend that fronts Australis for that product.
- **Grounding** — answering only from retrieved sources, with citations.
- **Tool retriever** — a read-only connector a product exposes over its own structured/live data.
