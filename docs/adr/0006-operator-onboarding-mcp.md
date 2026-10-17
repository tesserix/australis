# ADR-0006: Stateless operator onboarding through the shared gateway

Status: accepted for the privileged operator integration, 2026-10-17.

## Decision

The onboarding service owns the MCP server beside its identity API. Australis
owns the Claude stdio bridge, pinned contract and operator guidance. Registry
owns discovery and route export; the existing AgentGateway owns internet
admission. The endpoint is
`https://mcp.tesserix.app/mcp/onboarding/operator-onboarding-mcp`.

The MCP adapter calls the existing onboarding service in process. It does not
forward an MCP bearer token to another resource, mint an administrator token,
or store workflow state. PostgreSQL, private operator claims, Zitadel and held
GCS receipts retain their current ownership. Each HTTP request verifies its
own bearer token and constructs its authorized tool set. No session ID,
sticky routing, standalone SSE connection, new database, queue, deployment or
upstream API key is needed. The API's existing two replicas serve the endpoint.

The operational client is separate from the grounded assistant's read-only
ToolRetriever. ADR-0001's read-only default remains in effect for that engine.
This explicitly requested operator surface permits additive hospital requests
under a separate signed create role. Adoption requires an additional role;
the default Claude identity does not have it. Registry marks this surface
`consumerProfile: privileged-operator` and records write effects.

## Capacity and failure contract

The initial planning envelope is 5 calls/second peak, 80:20 reads:writes,
256 KiB maximum request body, and at most 100 tenant summaries per page. Status
responses are a few KiB. At 100 new customers/month, 1,200 customers at twelve
months and 3,600 at thirty-six months need no new datastore. These are sizing
assumptions, not measured traffic. Target existing API availability of 99.9%
monthly; p99 below one second for status/acceptance, with a hard 15-second
request deadline. Operator completion is asynchronous and has no synchronous
latency promise. Gateway quotas remain per verified OAuth subject; the API
also bounds each subject to 5 requests/second with a burst of ten per replica.

Zitadel/JWKS failure fails closed. Registry failure prevents new discovery;
existing last-known-good routes remain managed by route-sync. API/database
failure returns an error; the adapter never treats stale state as completion.
Operator, Helivanta or GCS failure leaves a resumable pending claim. A client
timeout after acceptance is handled by replaying the same idempotency key and
exact intent, then polling the returned tenant UUID. The bridge does not
automatically retry writes. A restarted MCP replica recovers no local state.

No additional standing pods or database connections are allocated. The
incremental cost is API CPU, audit events and retained onboarding objects;
the gateway and route-sync alerts cover the existing shared delivery path.

## Security boundary

Assets are customer identity boundaries, initial administrator memberships,
tenant metadata and retained receipts. Threats include internet callers,
another product, an agent with a generic gateway grant and injected customer
names. The gateway checks the signed gateway audience and `agentgateway.mcp`.
The onboarding MCP additionally checks RS256, issuer, expiry, the onboarding
resource audience, an access-token client identifier and project-scoped roles
granted by the exact Tesserix organization. Generic role claims and unsigned
identity headers confer no authority. All arguments and outputs have closed
schemas; tool results carry no passwords, API keys or clinical records.

The initial delegated authority is product-wide Helivanta onboarding, not a
hospital user's clinical session. Its signed roles are
`onboarding.helivanta.read`, `.create`, and `.adopt`. No role implies another:
read is always required, create and adopt are independent. Every privileged
tool invocation is logged before execution in the append-only API audit
ledger; the durable service also records provisioning events under the same
machine subject. This identifies the agent, not an assumed human behind it.

The operator issues the dedicated machine credential once into the exact
OpenBao path `onboarding/prod/app/onboarding-mcp-claude-prod`. Readers are
namespace-bound and read-only; writers remain the existing identity operator.
There is no new GCP Secret Manager dependency. No existing identity, login
policy or grant is replaced. SOC2 and HIPAA certification are not established
by this integration; contractual, organizational and broader product controls
remain separate requirements.

## Rollout and rollback

Release the API behind its initially disabled MCP configuration, then enable
the gateway project, exact network admission, Registry seed, OpenBao policy
and new machine claim through GitOps. Bootstrap adds the declared machine
roles without changing existing grants. Verify live discovery, read, create,
same-key replay, changed-intent conflict, readiness and denial paths. Retain
synthetic test claims and receipts; never delete a real customer to clean up a
test. A failed rollout is disabled by reverting MCP enablement and its new
Registry route/claim publication after reviewing retained resources. Roll back
the API image only to a verified prior immutable digest. Customer data and
archive history are not rolled back or deleted.

Embedding the adapter in the data-owning API avoids a second admin credential
and a new token-exchange boundary. A separate proxy service was rejected for
those costs. Direct Claude-to-private-API access was rejected because it
bypasses Registry and Gateway policy.
