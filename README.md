# Australis

**Australis** is a multi-tenant, grounded-assistant engine that product teams plug into to give their app an on-brand AI assistant — grounded in that product's own knowledge (documents *and* live/structured data), answering with citations, never fabricating.

*Australis* is Latin for **"southern"** — as in the *Aurora Australis* and *Crux Australis*, the Southern Cross that has guided navigators beneath the southern sky for millennia. That's the role of this engine: the fixed point the product family steers by. Grounded, cited answers that guide you true and never drift into hallucination.

One engine, many tenants. Each product registers a **knowledge connector**, a **model policy**, **config & rules** (persona, guardrails, output contract), and an **eval set** — and gets retrieval, grounding, citations, isolation, budgeting, caching, memory, model routing, and proactive digests for free.

- **First tenant:** Kora (nutrition coaching).
- **Fast-follow:** home-chef, mark8ly (ecommerce) — both production-ready.
- **Design target (informs the architecture now, integrates later):** HMS (hospital management) — sets the hard bar for isolation, model choice (self-hosted MedGemma for PHI), clinical guardrails, and single-tenant/on-prem deployability.

> **Status: design / pre-implementation.** This repo currently holds the product requirements and design record — no engine code yet.

## Design record

| Document | What it settles |
| --- | --- |
| [`docs/PRD.md`](./docs/PRD.md) | product requirements, tenant contract, roadmap |
| [`docs/adr/0001-mcp-integration-boundary.md`](./docs/adr/0001-mcp-integration-boundary.md) | who owns an MCP server, and how far Australis may bind to MCP |
| [`docs/design/mcp-hld.md`](./docs/design/mcp-hld.md) | high-level design — context, lifecycle, failure domains |
| [`docs/design/mcp-lld.md`](./docs/design/mcp-lld.md) | low-level design — the `ToolRetriever` port, resolution, invocation |
| [`docs/guides/authoring-an-mcp-server.md`](./docs/guides/authoring-an-mcp-server.md) | for product teams: how to write, publish, and register a connector |
| [`docs/diagrams/australis-mcp.drawio`](./docs/diagrams/australis-mcp.drawio) | four pages: context, lifecycle, resolution, invocation |
| [`docs/design/orchestration-hld.md`](./docs/design/orchestration-hld.md) | global supervisor/orchestrator — context, invariants, workflows, failure domains |
| [`docs/design/orchestration-lld.md`](./docs/design/orchestration-lld.md) | supervisor/orchestrator internals — modules, contracts, ceilings, durable path |
| [`docs/diagrams/australis-orchestration.drawio`](./docs/diagrams/australis-orchestration.drawio) | four pages: context, task shapes, supervised hand-off, LLD |

Australis is **independent of Otto** (the separate infra/SRE-automation assistant) — different audience, domain, trust model, and stack. They may share patterns, not a runtime.
