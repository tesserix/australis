# Australis

**Australis** is a multi-tenant, grounded-assistant engine that product teams plug into to give their app an on-brand AI assistant — grounded in that product's own knowledge (documents *and* live/structured data), answering with citations, never fabricating.

*Australis* is Latin for **"southern"** — as in the *Aurora Australis* and *Crux Australis*, the Southern Cross that has guided navigators beneath the southern sky for millennia. That's the role of this engine: the fixed point the product family steers by. Grounded, cited answers that guide you true and never drift into hallucination.

One engine, many tenants. Each product registers a **knowledge connector**, a **model policy**, **config & rules** (persona, guardrails, output contract), and an **eval set** — and gets retrieval, grounding, citations, isolation, budgeting, caching, memory, model routing, and proactive digests for free.

- **First tenant:** Kora (nutrition coaching).
- **Fast-follow:** home-chef, mark8ly (ecommerce) — both production-ready.
- **Design target (informs the architecture now, integrates later):** HMS (hospital management) — sets the hard bar for isolation, model choice (self-hosted MedGemma for PHI), clinical guardrails, and single-tenant/on-prem deployability.

> **Status: design / pre-implementation.** This repo currently holds the product requirements and design record — no engine code yet. See [`docs/PRD.md`](./docs/PRD.md).

Australis is **independent of Otto** (the separate infra/SRE-automation assistant) — different audience, domain, trust model, and stack. They may share patterns, not a runtime.
