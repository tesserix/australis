# Australis

**Australis** is a multi-tenant, grounded-assistant engine that product teams plug into to give their app an on-brand AI assistant — grounded in that product's own knowledge (documents *and* live/structured data), answering with citations, never fabricating.

*Australis* is Latin for **"southern"** — as in the *Aurora Australis* and *Crux Australis*, the Southern Cross that has guided navigators beneath the southern sky for millennia. That's the role of this engine: the fixed point the product family steers by. Grounded, cited answers that guide you true and never drift into hallucination.

One engine, many tenants. Each product registers a **knowledge connector**, a **model policy**, **config & rules** (persona, guardrails, output contract), and an **eval set** — and gets retrieval, grounding, citations, isolation, budgeting, caching, memory, model routing, and proactive digests for free.

- **First tenant:** Kora (nutrition coaching).
- **Fast-follow:** home-chef, mark8ly (ecommerce) — both production-ready.
- **Design target (informs the architecture now, integrates later):** HMS (hospital management) — sets the hard bar for isolation, model choice (self-hosted MedGemma for PHI), clinical guardrails, and single-tenant/on-prem deployability.

> **Status: design / pre-implementation.** This repo currently holds the product requirements and design record — no engine code yet.

## Design record

**Start here:** [`docs/PLAN.md`](./docs/PLAN.md) — the phased plan and the three
checkpoints worth stopping at.

| Document | What it settles |
| --- | --- |
| [`docs/PRD.md`](./docs/PRD.md) | product requirements, tenant contract, roadmap |
| [`docs/PLAN.md`](./docs/PLAN.md) | phased implementation plan across both tracks |
| **Decisions** | |
| [`adr/0001-mcp-integration-boundary.md`](./docs/adr/0001-mcp-integration-boundary.md) | original MCP integration boundary; source-location rules are partially superseded by ADR-0004 |
| [`adr/0002-shared-brain-and-learning-flywheel.md`](./docs/adr/0002-shared-brain-and-learning-flywheel.md) | what "our own model" means, what may be learned across products, and what may never be |
| [`adr/0003-evaluation-platform-and-grader.md`](./docs/adr/0003-evaluation-platform-and-grader.md) | shared eval store on global CNPG, grader service, Langfuse as eval UI |
| [`adr/0004-product-owned-mcp-connectors.md`](./docs/adr/0004-product-owned-mcp-connectors.md) | product-owned connectors by default; ownership, auth, mesh, rollout and migration |
| **Design** | |
| [`design/mcp-hld.md`](./docs/design/mcp-hld.md) | connector path — context, lifecycle, failure domains |
| [`design/mcp-lld.md`](./docs/design/mcp-lld.md) | the `ToolRetriever` port, resolution, invocation, validations |
| [`design/brain-and-flywheel.md`](./docs/design/brain-and-flywheel.md) | capture, corpus, training, evaluation, promotion — HLD and LLD |
| [`design/tenancy-and-identity.md`](./docs/design/tenancy-and-identity.md) | the five isolation layers, mesh identity, cache keying, scaling and throttle |
| [`design/orchestration-hld.md`](./docs/design/orchestration-hld.md) | global supervisor — context, invariants, workflows, failure domains |
| [`design/orchestration-lld.md`](./docs/design/orchestration-lld.md) | supervisor internals — modules, contracts, ceilings, durable path |
| **Guides** | |
| [`guides/authoring-an-mcp-server.md`](./docs/guides/authoring-an-mcp-server.md) | how to write, publish and register a connector |
| [`guides/product-mcp-onboarding.md`](./docs/guides/product-mcp-onboarding.md) | reusable end-to-end product onboarding, security, ADK, tests, evals and FAQ |
| [`servers/README.md`](./servers/README.md) | Australis-owned connectors: layout, invariants, CI behaviour |
| **Diagrams** | |
| [`diagrams/australis-architecture.drawio`](./docs/diagrams/australis-architecture.drawio) | 7 pages — context · lifecycle · resolution · invocation · build & publish · flywheel · learning tiers |
| [`diagrams/australis-tenancy.drawio`](./docs/diagrams/australis-tenancy.drawio) | 4 pages — isolation layers · stateless resolution · deployment topology · throttle ladder |
| [`diagrams/australis-orchestration.drawio`](./docs/diagrams/australis-orchestration.drawio) | 4 pages — context · task shapes · supervised hand-off · LLD |
| [`diagrams/product-mcp-lifecycle.drawio`](./docs/diagrams/product-mcp-lifecycle.drawio) | 4 pages — product architecture · release lifecycle · identity and secrets · promotion gates |

## The three ideas the design rests on

**Federated source, one platform contract.** A product-domain connector lives
beside the product API and is independently built, deployed and operated by that
team. Australis owns consumer pins, selection, grounding and evaluation;
Agentic Platform owns Registry, Gateway and the mesh contract. Only connectors
whose data and lifecycle are owned by Australis live under `servers/`.
([ADR-0004](./docs/adr/0004-product-owned-mcp-connectors.md))

**Grounding is structural, not learned.** Citations and the confidence gate live
in code and run around the model, never inside it. That is what lets Australis
train and swap its own models freely: the worst a bad adapter can do is answer
poorly, never fabricate a source.
([ADR-0002](./docs/adr/0002-shared-brain-and-learning-flywheel.md))

**Statelessness is a rebuild guarantee, not amnesia.** The engine never
remembers a tenant's tools; it re-derives them each request from tenant config,
the content-addressed Registry, and each connector's own database. A cold
replica differs from a warm one in latency only, never in answers — which is
what makes horizontal scaling safe.
([tenancy-and-identity §7](./docs/design/tenancy-and-identity.md))

Australis is **independent of Otto** (the separate infra/SRE-automation assistant) — different audience, domain, trust model, and stack. They may share patterns, not a runtime.
