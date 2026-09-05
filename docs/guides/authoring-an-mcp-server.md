# Guide — authoring an MCP server for Australis

Audience: a product team (Kora, mark8ly, home-chef, HMS) giving Australis access
to its live data. Start with the
[product onboarding guide](product-mcp-onboarding.md), follow
[ADR-0004](../adr/0004-product-owned-mcp-connectors.md), and use this guide for
the Python authoring details.

---

## Before you start: two things people get wrong

**You do not write an MCP server in YAML.** The server is code. The YAML is a
*manifest* — the publish and discovery envelope — and it is **compiled from your
code**, not hand-written. Hand-editing the generated manifest breaks the schema
fingerprints that Australis pins against, and your server stops resolving.

**Keep the server beside the system it adapts.** Product-domain connectors live
in the product repository by default, close to the product API contract and its
reviewers. Only a connector whose data and release lifecycle are owned by
Australis belongs under `australis/servers/`. In either location, the owning
team's CODEOWNERS entry applies.

**Your server is still its own build unit.** Own lockfile, own image, own tag,
own credential, own deployment. CI is path-filtered: your change rebuilds your
server and nothing else. Sharing a repository never means sharing a failure
domain.

---

## The five steps

### 1. Scaffold

In the owning product repository (or `australis` only for an
Australis-owned connector):

```bash
mkdir -p services/<product-domain-mcp> && cd services/<product-domain-mcp>
uv init
uv add "tesserix-mcp-runtime[manifest] @ https://github.com/tesserix/tesserix-mcp-runtime/releases/download/v0.1.0-rc.6/tesserix_mcp_runtime-0.1.0rc6-py3-none-any.whl"
```

This is a release wheel, not a PyPI lookup. Commit `uv.lock`, verify the wheel
checksum and attestation in CI, and update the pin deliberately. See the
[end-to-end onboarding guide](product-mcp-onboarding.md#2-select-runtime-and-adk-responsibilities)
for the current installation and conformance policy.

Name it for a bounded domain, not for the product: `kora-logs`, not `kora`. One
server per domain. A god-server accumulates unrelated scopes, and scopes are the
unit of authorisation.

### 2. Write tools

```python
from pydantic import BaseModel, Field
from tesserix_mcp_runtime import CallContext, ToolMetadata, callable_tool

class DailyLogInput(BaseModel):
    model_config = {"extra": "forbid"}          # closed — required
    start_date: str = Field(description="inclusive, RFC 3339 date")
    end_date:   str = Field(description="inclusive, RFC 3339 date")

class DailyLogOutput(BaseModel):
    model_config = {"extra": "forbid"}          # closed — required
    days: list[DayTotals]
    source_locator: str                          # what Australis cites

@callable_tool(
    metadata=ToolMetadata(
        name="daily_log_summary",
        description="Return the user's logged macro totals per day in a range.",
        required_scopes=("kora:logs:read",),
        effects=(),                              # read-only — v1 requirement
        idempotency="not_applicable",
    )
)
async def daily_log_summary(ctx: CallContext, args: DailyLogInput) -> DailyLogOutput:
    user_id = ctx.subject          # verified identity — NEVER a tool argument
    tenant  = ctx.tenant_id        # verified tenant   — NEVER a tool argument
    ...
```

Non-negotiables, and why each one exists:

| Rule | Why |
| --- | --- |
| Closed input **and** output models (`extra: forbid`) | Australis cannot cite an untyped blob; an open schema fails validation rule V5 |
| Identity and tenant from `CallContext` only | a `user_id` input parameter is a cross-tenant escape hatch; resolution rejects it (V8) |
| For private data, scope every query by verified `ctx.subject` | the runtime proves *who is calling*; nothing but your repository layer can scope *which rows* ([tenancy §6](../design/tenancy-and-identity.md)) |
| Read-only effects in v1 | ADR-0001 D6 — assist and guide, the product executes |
| Return something citable | Australis attaches `source_locator` to the citation, per PRD §12 |
| **p99 under 400 ms** | the engine's latency budget; see "Performance" below |

### 3. Compile the manifest

Write one authoring document — `mcp-authoring.json` — describing ownership,
packaging, scopes, route policy, and semantic metadata. Then:

```python
from pathlib import Path
from tesserix_mcp_manifest import compile_manifests, load_authoring_manifest

manifest = load_authoring_manifest(Path("mcp-authoring.json").read_bytes())
compiled = compile_manifests(manifest, runtime_version="1.2.3")
Path("server.json").write_bytes(compiled.server_json)
Path("mcpserver.json").write_bytes(compiled.registry_manifest)
print(compiled.server_digest, compiled.registry_digest)   # keep these
```

Run this in CI and commit the outputs; CI check 4 fails the build if the
committed manifests do not match a fresh compile. Reviewers should see manifest
diffs in the same PR as the code that caused them — that is the whole point of
generating rather than hand-writing.

The resulting Registry envelope:

```yaml
apiVersion: registry.agentic.dev/v1alpha1
kind: MCPServer
metadata:
  name: io.github.tesserix/kora-logs
  tag: "1.2.3"                 # OCI-style tag; new tag = new immutable revision
  visibility: internal
  labels: { domain: nutrition, product: kora }
spec:                          # ← this block is literally server.json
  name: io.github.tesserix/kora-logs
  version: "1.2.3"
  remotes:
    - { type: streamableHttp, url: https://kora-logs-mcp.internal/mcp }
  credentialRef: { name: kora-logs-mcp, key: token }   # reference only
  x-tesserix:
    required_scopes: [kora:logs:read]
    route_policy: { direct_access: false, gateway_path: /gateway/kora-logs/mcp }
    egress_hosts: []
    semantic:
      summary: Locate a user's logged macro totals for a date range.
      when_to_use: ["asked about what the user ate", "asked about weekly macros"]
      not_for: ["changing a log entry", "medical advice"]
      capabilities: [cap/nutrition-logs-read]
      risk: low
    tools:
      - name: daily_log_summary
        input_fingerprint:  sha256:…
        output_fingerprint: sha256:…
```

**Never put a literal credential in here.** The compiler rejects secret-shaped
keys recursively, and it is right to. Secrets live in GCP Secret Manager and
reach the pod via External Secrets.

### 4. Publish

```bash
agentic apply -f mcpserver.yaml
agentic verify mcpservers io.github.tesserix/kora-logs --tag 1.2.3
```

Content-addressed and idempotent: identical bytes are a no-op, a new tag is a
new immutable revision, and the revision timeline is append-only. The Registry
signs what it serves; `verify` checks that signature.

Then wait for the gateway route to activate, pinning both digests:

```bash
tesserix-mcp-runtime activation \
  --ref mcpservers/tenant-kora/io.github.tesserix/kora-logs@1.2.3 \
  --registry-digest sha256:… \
  --artifact-digest sha256:… \
  --wait-for active --timeout-seconds 120
```

Publication is not activation. A published server that is not routed is not
callable, by design.

### 5. Register with the Australis tenant config

Add the pinned reference to your tenant config (see
[LLD §3.1](../design/mcp-lld.md)):

```yaml
tenant: kora
knowledge:
  tool_kbs:
    - ref: mcpservers/tenant-kora/io.github.tesserix/kora-logs@1.2.3
      registry_digest: sha256:…
      artifact_digest: sha256:…
      tools:
        - name: daily_log_summary
          input_fingerprint:  sha256:…
          output_fingerprint: sha256:…
      deadline_ms: 400
      max_calls_per_turn: 3
```

Every digest is explicit and there is no `latest`. That is what makes a config
revision reproducible, and therefore what makes an eval result mean something.

---

## Performance: the 400 ms contract

Australis budgets **400 ms p99** for your tool inside a 2.5 s time-to-first-token
target (ADR-0001). This is a contract, not a target to approach.

If your answer needs a multi-second aggregate, **pre-materialise it**. Kora's
Weekly Report is the worked example: your product computes the week's stats
deterministically on a schedule, your tool reads the materialised row, and the
model summarises those numbers. The model never does the arithmetic — which is
also how "no fabricated numbers" stays true.

Exceeding the deadline is not an error for the user. Australis degrades to a
document-only answer with an explicit disclosure. It is, however, a silent
quality regression, so alert on your own p99.

---

## Versioning and retirement

- New tag for any schema change. Never mutate a published tag.
- Fingerprints change when schemas change, so Australis's pin fails closed at
  config load with `tool_config_invalid`. That is the system working: bump the
  tenant config in the same change, and it is a two-line diff.
- **Deprecate, observe, then retire.** Mark the old version `deprecated` via the
  Registry status endpoint, watch telemetry until no callers remain, then remove
  the route. Deleting a live route is the one genuinely irreversible mistake in
  this workflow.
- Keep the previous immutable GitOps target until telemetry confirms zero
  callers. Rollback is a revert, not a rebuild.

---

## Checklist before you open the PR

- [ ] One bounded domain; server named `<product>-<domain>`
- [ ] Closed input and output models on every tool
- [ ] No identity, tenant, or user ID in any input schema
- [ ] All effects read-only; idempotency `not_applicable`
- [ ] `required_scopes` minimal and specific
- [ ] `egress_hosts` declared (empty means deny-all — prefer it)
- [ ] `route_policy.direct_access: false`
- [ ] `credentialRef` only; no literal secret anywhere
- [ ] Semantic `summary`, `when_to_use`, `not_for` written for a reader, not an index
- [ ] Manifests generated in CI and committed alongside the code change
- [ ] p99 measured and under 400 ms
- [ ] Conformance tests green (`tesserix-mcp-runtime` testkit, no network)
- [ ] CODEOWNERS entry exists for the connector directory in its owning repo
- [ ] Contract test added, so the nightly job catches drift against your staging API
- [ ] Authorization profile declared: public service read or caller-authorized private read
- [ ] Private data: every query scoped by verified `ctx.subject` at **one** repository choke point
- [ ] Private data: two subjects, same tenant, same tool → no cross-user rows
- [ ] Postgres RLS enabled on the tables you read, where the product supports it
- [ ] Own ServiceAccount, and a DB credential scoped to this domain's tables only
- [ ] `ScaledObject` min 1 / max 5, or min 2 with the reason written down

---

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `tool_config_invalid` at load | digest or fingerprint mismatch | recompile, republish, update the tenant config pin |
| Server resolves but is never called | weak semantic metadata | rewrite `when_to_use` with concrete user phrasings |
| Answers silently lose live data | tool exceeding 400 ms | pre-materialise; check your own p99 |
| `activation_contract_invalid` | route not reconciled yet | check GitOps sync; publication is not activation |
| Result never cited | output schema open, or no citable locator | close the model; return `source_locator` |

---

## One thing the nightly job protects you from

If an Australis-owned connector lives here while adapting a separate product
API, that product's own test suite does not know it exists. If the schema moves
— a column renamed, a soft-delete flag added, an enum extended — the remote
connector can keep returning results that *look* fine.

Product-owned connectors run contract tests in the same CI as the product API.
For the remaining cross-repository case, the nightly contract-test job runs the
connector against staging and opens an issue on divergence. Add a contract test
with every tool and do not disable a noisy job; it is usually detecting real
drift.
