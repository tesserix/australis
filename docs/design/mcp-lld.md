# LLD — `ToolRetriever` port and MCP adapter

- Status: Draft, implements [ADR-0001](../adr/0001-mcp-integration-boundary.md)
- Date: 2026-09-02
- Reads with: [HLD](mcp-hld.md), diagram pages 2–4
- **Assumption flagged:** stack is Go 1.26 (PRD §20 lists this as unconfirmed)
  and tenant config is config-as-code (PRD §20, unresolved). Both are marked at
  the point of use below.

---

## 1. Package layout and the dependency rule

```
australis/
├─ internal/
│  ├─ core/
│  │  ├─ ports/            ToolRetriever, KnowledgeStore, ModelProvider, Meter …
│  │  ├─ evidence/         Evidence, Citation, Confidence
│  │  ├─ retrieval/        hybrid fusion, RRF, rerank, tool-branch orchestration
│  │  └─ compose/          grounded answer composition, confidence gate
│  ├─ adapter/
│  │  ├─ mcp/              ← the ONLY package permitted to import an MCP SDK
│  │  │  ├─ discovery.go   Registry search + fetch + digest verification
│  │  │  ├─ resolver.go    pin, fingerprint-check, cache
│  │  │  ├─ client.go      gateway invocation, deadlines, bulkhead
│  │  │  └─ evidence.go    tool result → []core/evidence.Evidence
│  │  ├─ registryhttp/     thin HTTP client for agentic-registry
│  │  └─ pgvector/
│  ├─ tenant/              config load, validation, model policy
│  └─ brain/               capture · corpus · eval · policy · promote (ADR-0002)
├─ servers/                the connector fleet — one build unit each (ADR-0001 D1)
│  ├─ _shared/
│  ├─ kora/logs/
│  ├─ mark8ly/catalog/
│  └─ australis-evals/
└─ training/               offline LoRA pipelines — never in the serving image
```

**The rules (ADR-0001 D1, D2), enforced in CI:**

1. Nothing under `internal/core/` may import `internal/adapter/...` or any MCP
   SDK. Core defines interfaces; adapters point inward.
2. Nothing under `internal/` may import `servers/`. The engine reaches a
   connector over the wire through the gateway, even though its source sits in
   the same tree. Without this, co-location silently becomes a distributed
   monolith.
3. No server may import another server's package. Build units stay independent.

See §7 for the checks.

## 2. The port

`internal/core/ports/tool_retriever.go`. This is the contract the rest of the
engine sees. It mentions neither MCP nor HTTP.

```go
// ToolRetriever fetches evidence from a tenant's live/structured knowledge.
// Implementations are per-tenant and resolved at config load, never per request.
type ToolRetriever interface {
	// Tools returns the pinned, authorised tool set for this tenant.
	Tools(ctx context.Context) ([]ToolDescriptor, error)

	// Invoke runs one tool and returns evidence. It must respect the deadline
	// on ctx and must not retry a non-idempotent call.
	Invoke(ctx context.Context, call ToolCall) (evidence.Bundle, error)
}

type ToolDescriptor struct {
	Name         string   // fully qualified: <server>/<tool>
	Summary      string   // semantic: what it does
	WhenToUse    []string // semantic: positive cues for selection
	NotFor       []string // semantic: negative cues
	InputSchema  jsonschema.Schema
	OutputSchema jsonschema.Schema // closed; required — see §4
	Idempotency  Idempotency       // v1: always NotApplicable (ADR-0001 D6)
	RiskLevel    Risk
}

type ToolCall struct {
	Tool      string
	Arguments json.RawMessage // validated against InputSchema before dispatch
	TenantID  string          // from verified request context, never from the model
	RequestID string
}
```

Two details carry weight:

- **`OutputSchema` is required, not optional.** An untyped result cannot be
  turned into an attributable `Evidence` item, and PRD §12 forbids shipping an
  uncited substantive claim. A server without output schemas fails validation.
- **`TenantID` is on the struct, not in `Arguments`.** The model never supplies
  it. A tool whose input schema contains a tenant-like field is rejected at
  resolve time (§5), because that shape is a cross-tenant escape hatch.

## 3. Resolution: from tenant config to a pinned tool set

Happens once per tenant-config revision, not per request. Diagram page 3.

### 3.1 Tenant config (config-as-code — assumption per §0)

```yaml
tenant: kora
knowledge:
  tool_kbs:
    - ref: mcpservers/tenant-kora/io.github.tesserix/kora-logs@1.2.3
      registry_digest: sha256:aaaa…
      artifact_digest: sha256:bbbb…
      tools:
        - name: daily_log_summary
          input_fingerprint:  sha256:cccc…
          output_fingerprint: sha256:dddd…
      deadline_ms: 400
      max_calls_per_turn: 3
```

Every digest and fingerprint is explicit. This file *is* the pin — reproducible
config revision, reproducible eval run.

### 3.2 Resolve algorithm

```
for each tool_kb in tenant.knowledge.tool_kbs:
    1. GET /v0/search?q=<capability>&kinds=MCPServer&view=stub&namespace=<tenant ns>
       → Registry-authorised stubs (already visibility/RBAC filtered)
    2. same-origin fetch via stub.fetchPath      → exact MCPServer object
    3. verify object signature against GET /v0/signing-key
    4. assert object digest == config.registry_digest        else FAIL_CLOSED
    5. assert spec.x-tesserix artifact digest == config.artifact_digest  else FAIL_CLOSED
    6. call tools/list through the activated gateway route; require closed input/output schemas
       and assert their canonical fingerprints match Registry + config  else FAIL_CLOSED
    7. assert every tool effect is read-only                 else FAIL_CLOSED  (D6)
    8. assert route_policy.direct_access == false            else FAIL_CLOSED  (D4)
    9. assert no input-schema property matches tenant-identity shapes (§2)
   10. cache under key (tenant_id, config_revision)
```

**Fail closed at every step.** A resolution failure is a tenant-config error
surfaced at load, not a 500 at 3 a.m. Steps 4–6 are what make ADR-0001 D3 real:
without them "pinned" is a comment, not a property.

The generated Registry object carries an output fingerprint, not an output
schema. Therefore step 6 deliberately reads the live MCP contract during
activation. Runtime invocation still validates every result against that
activated output schema; request-time discovery is forbidden.

### 3.3 Cache

| Property | Value | Why |
| --- | --- | --- |
| Key | `(tenant_id, config_revision)` | never shared across tenants (HLD §6) |
| Value | resolved descriptors + endpoint + digests | ~20 KB per server |
| Size | bounded LRU, 500 entries | working set is < 2 MB (ADR-0001) |
| TTL | 15 min soft / 24 h hard | soft = refresh; hard = refuse |
| On Registry down | serve soft-expired up to hard TTL | degradable tier |
| On cold cache + Registry down | `tool_retriever_unavailable`, degrade to document-only | never guess |

In-process, not Redis. Two megabytes replicated across N pods is cheaper than a
cache round-trip and removes a failure mode. Revisit only if the working set
grows past a few hundred MB.

**The cache holds the pre-filter descriptor set only.** Scope filtering by
`ctx.scopes` is a pure function applied on every request, after the lookup.
Caching the filtered list under a tenant-keyed entry would eventually serve one
subject's tool list to another — a correctness rule, not an optimisation. See
[tenancy-and-identity §7](tenancy-and-identity.md#7-cache-before-the-filter-never-after).

## 4. Invocation path

### 4.1 Sequence (diagram page 4)

```
planner selects tool
  └─▶ validate Arguments against InputSchema        (reject → no call made)
      └─▶ acquire tenant bulkhead slot              (full → degrade, do not queue)
          └─▶ POST {gateway}/mcp/<server>  Streamable HTTP
              headers: Authorization (workload identity), X-Request-Id, X-Tenant-Id
              deadline: min(config.deadline_ms, remaining budget)
              └─▶ MCP runtime re-authorises per-tool, default-deny
                  └─▶ result
              └─▶ validate result against OutputSchema  (reject → discard as evidence)
                  └─▶ map to []Evidence with Citation
```

### 4.2 Bulkhead (per tenant)

```go
type bulkhead struct {
	maxInFlight int           // default 8
	maxConns    int           // default 4 keep-alive conns
	acquireWait time.Duration // 50ms — then give up, do not queue
}
```

Sized against ~5 peak RPS across all tenants (ADR-0001): 8 in-flight per tenant
is roughly 10x headroom on a single tenant's share. When the bulkhead is full
the call is **abandoned, not queued** — queueing converts a latency problem into
a timeout cascade, and the answer degrades gracefully to document-only anyway.

**These counters are per process, not fleet-wide.** At `maxReplicas: 5` the real
per-tenant ceiling is `8 x 5 = 40`. Deriving the per-process limit from a desired
global ceiling — rather than the reverse — is what keeps scale-out from silently
weakening the noisy-neighbour guarantee. The bulkhead also covers KEDA's 30–60 s
reaction window; see the throttle ladder in
[tenancy-and-identity §9](tenancy-and-identity.md#9-the-throttle-ladder).

### 4.3 Deadlines and retries

- Deadline is `min(tool deadline, remaining turn budget)`. The turn budget is
  decremented as hops complete, so a slow retrieval shrinks the tool budget
  rather than blowing the 2.5 s TTFT target.
- **At most one retry, only on connect-failure or 503 before any bytes are
  read.** Every v1 tool is a read (`not_applicable` idempotency), so this is
  safe; when writes arrive, retry becomes conditional on an idempotency key.
- No retry on timeout. A timed-out tool has probably done its work; retrying
  doubles load on an already-slow dependency.
- **Max `max_calls_per_turn` tool invocations per turn** (default 3, config
  above). Bounds cost and stops planner loops.

### 4.4 Tool result to Evidence

```go
type Evidence struct {
	Content   string
	Citation  Citation // source + locator — mandatory, no zero value permitted
	Score     float64
	Retrieved time.Time // stamped: tool data is live, so freshness is disclosed
}

type Citation struct {
	Kind    CitationKind // CitationTool | CitationDocument
	Source  string       // "kora-logs/daily_log_summary"
	Locator string       // e.g. "user=…, date-range=2026-08-25..2026-08-31"
	Digest  string       // registry_digest of the server that produced it
}
```

`Digest` on the citation is what makes an answer auditable months later: it
identifies the exact server version that produced the claim. PRD §14 asks for
per-answer citation/decision audit; this is that field.

Tool evidence enters the same RRF fusion as document evidence, then the
confidence gate. Below threshold, the engine escalates honestly rather than
guessing (PRD §12) — unchanged by the tool branch's existence.

## 5. Validation rules (fail-closed catalogue)

Collected here so the implementation and its tests share one list.

| # | Rule | When | Failure |
| --- | --- | --- | --- |
| V1 | signature verifies against registry signing key | resolve | config error |
| V2 | `registry_digest` matches config | resolve | config error |
| V3 | `artifact_digest` matches config | resolve | config error |
| V4 | every tool input/output fingerprint matches | resolve | config error |
| V5 | output schema present and closed | resolve | config error |
| V6 | all effects read-only | resolve | config error |
| V7 | `direct_access == false` | resolve | config error |
| V8 | no tenant-identity-shaped input property | resolve | config error |
| V9 | arguments validate against input schema | per call | no call issued |
| V10 | result validates against output schema | per call | result discarded, not cited |
| V11 | deadline not exceeded | per call | degrade to document-only |
| V12 | per-turn call count within limit | per turn | stop calling, compose with what exists |

## 6. Error contract

One shape everywhere; clients branch on `code`, never on message text.

```json
{ "code": "tool_retriever_unavailable",
  "message": "live data for this tenant is temporarily unavailable",
  "request_id": "01J…" }
```

| `code` | HTTP | Meaning | Caller action |
| --- | --- | --- | --- |
| `tool_retriever_unavailable` | 200 + partial | tool branch failed, answer is document-only | render with disclosure |
| `tool_config_invalid` | 500 | resolution failed V1–V8 | page the tenant owner; config bug |
| `tool_budget_exhausted` | 200 + partial | per-turn or per-tenant budget hit | render with disclosure |
| `knowledge_unavailable` | 503 | both branches down | BFF circuit-breaks, product degrades |

Note the 200s. A failed tool branch is a *degraded answer*, not an error — the
whole point of HLD §5's independent failure domains. Only losing both branches
is a 503.

## 7. CI enforcement of the layering rule

ADR-0001 D2 is worthless as a convention. Three checks, all failing the build:

```bash
# 1. core must not import adapters or any MCP SDK
go run ./architecture/check_layers.go

# 2. exactly one package may import the MCP SDK
grep -rl "modelcontextprotocol/go-sdk" --include=*.go internal/ \
  | grep -v '^internal/adapter/mcp/' | grep . && exit 1

# 3. the engine must not import the connector fleet (ADR-0001 D1)
grep -rn 'australis/servers/' --include=*.go internal/ | grep . && exit 1

# 4. generated manifests must match a fresh compile
./scripts/check-manifests-clean.sh

# 5. standard gates
go build ./... && go vet ./... && go test ./...
```

Checks 2 and 3 are deliberately blunt greps. They are unambiguous, have no false
negatives, and a reviewer can verify each by reading one line — which is the
property that matters for invariants this load-bearing. Check 3 is the one that
keeps the monorepo honest; without it, the first person in a hurry imports a
server package directly and the isolation argument in ADR-0001 stops being true.

## 8. Testing

| Layer | Approach |
| --- | --- |
| Port contract | table tests against a fake `ToolRetriever`; core never sees MCP |
| Adapter | `tesserix-mcp-runtime`'s reusable in-process conformance testkit — no network |
| Resolution | golden fixtures per V1–V8, each asserting fail-closed |
| Degradation | fault injection: Registry down (cold/warm cache), gateway 503, tool timeout, bulkhead full — assert answer degrades to document-only with disclosure, never 500 |
| Isolation | two tenants, identical server name, assert no cache or evidence bleed |
| Grounding | eval golden set (PRD §6.4) asserting citation coverage on tool-derived claims |

The degradation row is the one that earns its keep. Every failure mode named in
ADR-0001's dependency table has a test, or the table is fiction.

## 9. Open implementation questions

1. **Go MCP SDK maturity.** `github.com/modelcontextprotocol/go-sdk` needs a
   client-side spike and a pinned version before Phase 1. If it is not ready,
   the fallback is a thin Streamable HTTP client written against the protocol
   directly — the adapter boundary in §1 makes that a contained decision.
2. **Tool selection strategy.** Semantic metadata into the planner prompt versus
   a deterministic capability match. Start deterministic; it is testable and
   costs no tokens. Revisit when tool counts per tenant exceed ~10.
3. **Config-as-code versus admin API** (PRD §20). §3.1 assumes files. If the
   admin API wins, §3.1's shape survives as the API's payload.
4. **Weekly Report path** (PRD §13). Scheduled digests reuse this retriever but
   run outside a chat turn, so the 400 ms budget does not apply. A separate,
   longer batch deadline needs specifying in the proactivity design.
