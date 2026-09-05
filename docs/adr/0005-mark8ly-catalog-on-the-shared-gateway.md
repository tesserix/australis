# ADR-0005: Mark8ly's catalog connector ships on the shared gateway

- Status: Proposed
- Date: 2026-09-05
- Owners: Australis, Agentic Platform, Mark8ly
- Relates to: [ADR-0004](0004-product-owned-mcp-connectors.md),
  [ADR-0001](0001-mcp-integration-boundary.md), and the
  [product onboarding guide](../guides/product-mcp-onboarding.md)
- Supersedes: ADR-0004's product-owned default **for the Mark8ly catalog
  connector only**

## Why this ADR exists

ADR-0004 was accepted on 2026-09-05 and says a new domain connector must be
product-owned:

> The shared `slm-support-platform/mcp-gateway` remains a supported legacy and
> bootstrap implementation. It is not the target architecture for a new
> domain-specific connector. Kora proves the production topology and security
> flow, but a new product must not copy Kora's shared image coupling.

The onboarding guide says the same thing in three more places, including a
Mark8ly-specific row — *"Keep the merged connector in Mark8ly"* — and an
explicit instruction not to move that code elsewhere.

Mark8ly is nonetheless proceeding with the shared-gateway packaging, following
the Kora precedent. This ADR records that, because a decision that contradicts
an accepted ADR and is written down nowhere is a decision the next reader will
either reverse by accident or re-argue from scratch.

It is filed as **Proposed** rather than Accepted: it contradicts an ADR owned by
Australis and the Agentic Platform, and those owners should decide whether this
is a one-off exception or a change to the default.

## Decision

Mark8ly's five read-only catalog tools ship as **tenant tools inside
`slm-support-platform/services/mcp-gateway`**, registered in
`_register_mark8ly()` alongside the eight support tools already there, and
served by the existing `mark8ly-mcp` deployment.

No new image, chart, ArgoCD Application, ExternalSecret, Kargo subscription or
Registry record is created. The route, brokered credential and mesh policy that
already serve `mark8ly-mcp` serve these tools too.

## What this trades away

Recording these plainly, because they are the reasons ADR-0004 chose the other
default and they do not stop being true:

**Closed output schemas are lost.** The shared runtime's `@mcp.tool` derives an
input schema from the handler signature and has no output schema at all.
ADR-0001 invariant 4 requires both — *"an untyped result cannot be cited"* — and
that requirement is what made OpenAPI ingestion unacceptable for this surface in
the first place. Under this decision a catalog result is a typed Python dict
whose shape nothing enforces at the boundary.

**Read-only stops being structural.** The superseded implementation reached its
product API through a client with exactly one method, so a write was not
expressible. In `tenants.py`, `_get_json` and `_post` sit in the same module, so
read-only becomes a review convention.

**Release blast radius returns.** One image serves Kora, HomeChef, Platform,
Stockpilot and Mark8ly. A catalog change rebuilds and promotes the executable
those four other tenants run, coupling their rollback, dependency upgrades and
vulnerability response to ours. ADR-0004's cost analysis of this still applies;
this ADR accepts the cost rather than disputing it.

**Field-level projection weakens.** `tax_code`, `tax_rate_override`,
`tax_category` and internal ids were excluded by being absent from a declared
result type. Excluding them now depends on the tool body doing so and on review
noticing if it stops.

## What is superseded

The Mark8ly Go connector merged in mark8ly#663 — `services/mcp`, five tools with
closed input and output schemas — has no consumer under this decision. The
`go-shared/mcp` package it was built on (v1.10.0, v1.11.0) remains valid and
independently useful; nothing in this ADR retires it.

Mark8ly should decide separately whether that code is deleted or left dormant.
Dormant code that looks maintained is its own hazard.

## What does not change

Everything ADR-0004 says about the platform journey still holds, and Kora
remains the reference for it:

- every invocation goes through AgentGateway, which authenticates the caller's
  Zitadel JWT and injects the product MCP key;
- the connector authenticates to the product API with a **different**
  credential, so compromise and rotation stay isolated;
- the Registry is control plane, never request path;
- routes are reconciled by the route-sync controller from Registry export;
- tools are read-only for this release; and
- the product API remains the final authority for every tenant, user and object.

## Open question for the owners

Is this an exception for one connector, or a reversal of ADR-0004's default?

If it is the default, ADR-0004's Decision section and the onboarding guide's
Mark8ly row, FAQ answer, and "what not to copy from Kora" section all currently
say the opposite and should be rewritten. If it is an exception, this ADR is
sufficient and ADR-0004 stands for the next product.
