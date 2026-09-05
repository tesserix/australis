# ADR-0004: Product-owned MCP connectors

- Status: Accepted
- Date: 2026-09-05
- Owners: Australis, Agentic Platform, and participating product teams
- Relates to: [ADR-0001](0001-mcp-integration-boundary.md),
  [MCP HLD](../design/mcp-hld.md), and the
  [product onboarding guide](../guides/product-mcp-onboarding.md)
- Supersedes: ADR-0001 D1 and D5 only where they require all connector source
  to live in this repository

## Decision

The default is a **product-owned, domain-bounded MCP service**:

- connector source and contract tests live with the product API they adapt;
- the product team owns its image, release, deployment, ServiceAccount, API
  credential, SLO, and rollback;
- the Agentic Platform owns Registry publication, AgentGateway routing,
  brokered upstream credentials, and the reusable mesh policy contract; and
- Australis owns consumer configuration, semantic selection, digest pins,
  grounding, citations, agent evaluations, and graceful degradation.

The shared `slm-support-platform/mcp-gateway` remains a supported legacy and
bootstrap implementation. It is not the target architecture for a new
domain-specific connector. Kora proves the production topology and security
flow, but a new product must not copy Kora's shared image coupling.

An Australis-owned connector may still live under `servers/` when its backing
data and operating lifecycle are genuinely owned by Australis. Repository
location follows data and release ownership, not a universal monorepo rule.

## Why this is the default

An MCP connector is a service boundary only when it has a distinct data owner,
failure domain, or release cadence. A product catalog connector has all three:
it translates product-owned schemas, calls a product-owned API, and must change
with that API. Keeping it in the product repository makes schema drift visible
to the same CI and reviewers that change the source API.

The alternative shared image ships unrelated products together. A change for
one tenant rebuilds and promotes the executable used by several others. That
couples rollback, vulnerability response, dependency upgrades, and release
risk even when runtime configuration is tenant-specific. At the current scale,
the small additional idle pod and image storage cost is worth the smaller blast
radius.

This decision preserves the useful parts of ADR-0001: MCP stays behind the
`ToolRetriever` port, every invocation goes through AgentGateway, Registry
references are immutable and digest-pinned, tools are read-only by default,
and deployments remain explicit per server.

## Planning envelope

Until a product supplies measured traffic, design one connector for:

| Quantity | Initial envelope |
| --- | ---: |
| Peak tool calls | 5 requests/second per product |
| Read/write ratio | 100:0 for the first release |
| Request body | at most 64 KiB |
| Response body | at most 500 KiB |
| Tool latency | p99 below 400 ms |
| Availability | 99.9% monthly for an active connector |
| Replicas | 1 initially; 2 when the connector is user-critical |
| Registry convergence | normally 30 seconds; activation objective 120 seconds |

These are release gates, not production measurements. Replace them with
observed traffic before GA. Five calls per second does not justify a queue,
shared cache, new datastore, or scale-to-zero cold starts in a 400 ms budget.

## Ownership model

| Concern | Owner | Repository or control plane |
| --- | --- | --- |
| Tool behavior and product API contract | Product team | Product repository |
| MCP wire/runtime conformance | Product team, using platform contract | Product repository plus `tesserix-mcp-runtime` testkit |
| Image and deployment | Product team | Product CI plus `tesserix-k8s` GitOps |
| Registry record and lifecycle | Agentic Platform with product review | `devai/architecture/registry-seeds/` |
| Gateway route and credential injection | Agentic Platform | Registry export plus route-sync controller |
| Workload identity and mesh authorization | Product and Platform jointly | Product chart plus `istio-config` where needed |
| Tool discovery and invocation | Australis/ADK consumer | Australis tenant config |
| Grounding, citations, quality evals | Australis | Australis eval configuration and observability |

For the merged Mark8ly implementation, application code stays under
`mark8ly/services/mcp/`: `cmd/mcp-catalog` owns process composition,
`internal/catalog` owns the product API projection, `internal/server` owns MCP
transport/authentication, and `internal/config` owns bounded environment
configuration. Kubernetes desired state belongs in `tesserix-k8s`, the
Registry seed belongs in `devai`, and only the consumer pin/eval belongs in
Australis. This split follows control-plane ownership without splitting one
executable across repositories.

## Naming contract

Names include both product and bounded domain. For the Mark8ly catalog:

| Resource | Name |
| --- | --- |
| MCP server, Service, Deployment, ServiceAccount | `mark8ly-catalog-mcp` |
| Registry tenant | `mark8ly` |
| Gateway path | `/mcp/mark8ly/mark8ly-catalog-mcp` |
| Future per-server role | `mcp:mark8ly:mark8ly-catalog-mcp` |
| Gateway Secret | `product-mcp-upstream-keys` |
| Gateway Secret key | `MARK8LY_CATALOG_MCP_KEY` |
| Secret Manager entry | `prod-mark8ly-mcp-catalog-key` |
| Gateway-to-MCP header | `X-MCP-Key` |
| MCP-to-product-API credential | a separate product-owned secret, for example `prod-mark8ly-storefront-key` |

The domain suffix is intentional. `MARK8LY_MCP_KEY` already identifies the
legacy shared support connector and must not be reused. The Secret's key names
are identifiers, not a requirement that every Secret Manager entry begin with
`prod-support-platform-`.

## Registry and route eligibility

A Registry record is routable only when all of the following are true:

1. it is readable by the route-sync Registry identity;
2. `metadata.labels.mcp.tesserix.app/class` is `platform`;
3. `spec.protocolVersion` is `2026-07-28`;
4. it has a valid `spec.remotes[0].url` or `spec.endpoint`;
5. it is not a directory record (`spec.catalog: true` or
   `mcp.devai.io/catalog: "true"`); and
6. gateway export is not disabled by `spec.gatewayExport: false` or
   `mcp.tesserix.app/gateway-export: "false"`.

`visibility: internal` is correct for product connectors. Visibility affects
whether route-sync can read the record; it does not choose a route class.
`authMode: apikey` describes the upstream authentication mode but does not by
itself create credential injection.

`spec.credentialRef` is authoritative for injection. The Registry exporter
turns `secretName`, optional `key`, and optional `header`/`prefix` into an
`AgentgatewayPolicy` attached to that server's backend. The named Kubernetes
Secret must already exist in `agentgateway-system`; the Registry never copies
or stores secret material.

## Authentication and authorization

There are four independent trust crossings:

1. **Agent to AgentGateway:** a short-lived Zitadel JWT. AgentGateway verifies
   signature, algorithm, issuer, audience, expiry, and the machine role.
2. **AgentGateway to connector:** Ambient mTLS authenticates the
   `cluster.local/ns/agentgateway-system/sa/agentgateway-mcp` SPIFFE principal.
   AgentGateway also injects the server-specific `X-MCP-Key` without exposing
   it to the agent.
3. **Connector runtime:** the connector validates the key, allowlists the tool,
   and validates its arguments. A connector exposing private or user-scoped
   data must additionally verify signed caller identity and scopes; a plain
   `X-Tenant-Id` or model argument is never authority. Gateway admission is not
   sufficient authorization for private data.
4. **Connector to product API:** a separate product-owned workload credential.
   The product API remains authoritative for tenant, user, storefront, and
   object-level access.

The currently deployed coarse JWT role is `agentgateway.mcp`; it grants access
to the MCP gateway origin. The route exporter can generate a per-server policy
requiring `mcp:<tenant>:<server>`, but `requireServerScope` is currently false.
Provision server roles to intended callers before enabling that switch. Never
enable enforcement first: every existing caller would fail closed.

The Mark8ly catalog is presently a service-authorized, public-storefront read
surface. Its `store_slug` is a resource locator, not proof that the caller owns
the store. That is acceptable only while every returned field is intentionally
public and read-only. Before adding merchant-private, customer, order, or
tenant-sensitive data, add cryptographically verified caller context and
object-level authorization at the product API.

## Ambient mesh admission

A waypoint is a policy enforcement point, not an allow rule. Every connector
requires all of the following:

- its own ServiceAccount and therefore its own SPIFFE identity;
- `automountServiceAccountToken: false` unless Kubernetes API access is a
  reviewed requirement;
- namespace Ambient enrollment and waypoint binding;
- workload-scoped `PeerAuthentication` in `STRICT` mode;
- a workload selector ALLOW/DENY pair for direct traffic;
- a Service-targeted waypoint ALLOW/DENY pair preserving the original
  AgentGateway principal; and
- Kubernetes NetworkPolicy admission for the HBONE path and declared backing
  API dependencies.

Reuse the current `mcp-gateway` policy shape, but render it from the new
product-owned chart. Add centralized `istio-config` NetworkPolicy entries only
where that chart owns the namespace-level policy. Do not rely on a namespace
waypoint policy alone, and do not broaden a whole namespace merely to admit one
connector.

## Delivery and consumer gate

The first intended caller for `mark8ly-catalog-mcp` is the Mark8ly tenant in
Australis through its ADK tool surface. Production activation requires a named
consumer owner, a pinned server reference, a least-privilege role plan, and a
passing HTTP evaluation bundle.

Do not add the connector as an eighth synchronized subscription to Mark8ly's
existing seven-image Warehouse. Requiring unrelated images to share one tag
turns a connector release into a product-wide freight barrier. Give the
connector an independent Warehouse/freight subscription and promotion step.

If no consumer is ready, build and deploy it only to staging, publish it as
internal with gateway export disabled, and run conformance and canary tests.
Enable the production route and Kargo subscription when the consumer gate is
satisfied. An unused production route is not validation.

## Failure behavior

| Failure | Required behavior |
| --- | --- |
| Registry unavailable | Existing routes continue from last-known-good state; new activation waits and alerts |
| Export invalid or below prune floor | Route-sync retains last-known-good resources and becomes unready; never lower the floor to force pruning |
| AgentGateway unavailable | Tool retrieval degrades; Australis may answer from document knowledge with disclosure |
| Connector unavailable | Failure is isolated to this server/product; no other product image rolls back |
| Product API unavailable | Connector returns a stable dependency-outage result within its deadline |
| Credential mismatch during rotation | Calls fail closed; overlap old/new credentials through the product's supported rotation procedure |
| Tool call delivered twice | Reads remain safe; future writes require end-to-end idempotency at the product API |
| Crash after downstream response | No connector state is recovered; the caller may retry only within the bounded read retry policy |

## `MIN_RESOURCES`

`MIN_RESOURCES=27` is a catastrophic-prune guard for the permanent
AgentGateway platform baseline. It is not an inventory count and should not be
incremented for every MCP server. Dynamic connector routes normally raise the
desired count above the floor.

Bump the floor only when the permanent Registry-owned platform baseline grows,
and update the matching platform-seed expected count in the same reviewed
change. A per-connector safety invariant belongs in activation status and
last-known-good reconciliation, not in one global integer.

If the guard trips: keep pruning disabled by the controller, inspect Registry
availability/authentication, source namespaces, export qualification, and the
full desired resource count; restore the valid export; then verify route
acceptance and authenticated protocol probes. Do not reduce the value during
an incident merely to make reconciliation green.

## Consequences

### Positive

- Product schema and connector changes share CI and reviewers.
- A connector release cannot redeploy four unrelated products.
- Every domain receives an independent image, identity, credential, SLO, and
  rollback.
- Australis remains protocol-oriented and can consume Python, Go, or another
  conforming implementation.

### Cost

- More small deployments, dashboards, and release pipelines exist.
- Platform conformance must be enforced across repositories.
- Shared fixes require versioned library upgrades rather than one image build.

At the stated traffic, compute cost is small compared with model inference.
The operational cost is controlled with one reusable chart contract,
conformance suite, dashboard template, and onboarding checklist.

### Rejected alternatives

- **One shared executable for every product:** rejected because release and
  rollback blast radius crosses product boundaries.
- **Embed tools directly in Australis:** rejected because it couples the engine
  to product schemas and bypasses Registry/Gateway policy.
- **Direct ADK-to-product API calls:** rejected because it duplicates auth,
  discovery, telemetry, and grounding behavior in every agent.
- **Deploy without a consumer:** rejected for production because pod health
  does not prove discoverability, selection, authorization, or useful results.

## Migration and rollback

Existing shared Kora, Mark8ly support, HomeChef, and Platform connectors are not
forced to migrate. Move one bounded domain when it next needs independent
release cadence or material change. Publish the product-owned server under a
new name, run both routes, move one consumer pin, observe, then deprecate the
old tools. Never reuse the old server name for a different contract.

Rollback changes only the consumer pin and the product-owned deployment. Keep
the previous immutable image and Registry revision available until telemetry
shows zero callers to the superseded version.
