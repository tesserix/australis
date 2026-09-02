# `servers/` — the MCP connector fleet

Every MCP server in the product family is built and published from this
directory. See [ADR-0001](../docs/adr/0001-mcp-integration-boundary.md) for why,
and the [authoring guide](../docs/guides/authoring-an-mcp-server.md) for how.

## The rule that makes this safe

> **Monorepo of source, polyrepo of artifacts.**

Sharing a git repository must never mean sharing a failure domain. Each server
below is an independent build unit with its own lockfile, image, tag, Registry
object, credential and deployment. There is no "the Australis servers image",
and there is no deploy that carries two tenants at once.

## Layout

```
servers/
├─ _shared/                  vetted shared library — keep small, or empty
├─ kora/
│  └─ logs/                  io.github.tesserix/kora-logs
│     ├─ pyproject.toml      own lockfile
│     ├─ Dockerfile          own image
│     ├─ mcp-authoring.json  manifest source
│     ├─ server.json         generated — do not hand-edit
│     ├─ mcpserver.yaml      generated — do not hand-edit
│     ├─ src/
│     └─ tests/
├─ mark8ly/
│  ├─ catalog/
│  └─ orders/
├─ home-chef/
│  └─ recipes/
└─ australis-evals/          engine-owned (ADR-0001 D5)
```

One server per bounded domain. `kora-logs`, not `kora`. A god-server accumulates
unrelated scopes, and scopes are the unit of authorisation.

## Deployment profile

Every server gets a standard `Deployment` and a KEDA `ScaledObject` at **min 1,
max 5**, in its product's namespace, behind that namespace's waypoint, with its
own ServiceAccount and SPIFFE ID. No Knative, no scale-to-zero: min 1 spends
idle capacity to keep cold starts out of a 400 ms tool budget that is a hard
contract.

`minReplicas: 1` means a node drain briefly takes that connector offline. That
is acceptable because the engine degrades to document-only with disclosure
rather than failing. If it is *not* acceptable for your connector, set min 2 and
write the reason in your server's README. Full rationale and the throttle ladder:
[tenancy-and-identity §8–§9](../docs/design/tenancy-and-identity.md).

## Invariants, enforced in CI

| # | Rule | Why |
| --- | --- | --- |
| 1 | A server may not import another server's package | keeps build units independent |
| 2 | `internal/core/` may not import `servers/` | the engine is a client over the wire, not a caller in-process |
| 3 | Generated manifests must match a fresh compile | hand-edited manifests break the digest pin and stop resolving |
| 4 | Every tool declares closed input **and** output schemas | an untyped result cannot be cited |
| 5 | No literal credential anywhere — `credentialRef` only | the compiler rejects secret-shaped keys, and it is right to |
| 6 | All effects read-only in v1 | ADR-0001 D6 |
| 7 | Each server directory has a CODEOWNERS entry | co-locating code must not transfer domain ownership |
| 8 | Own ServiceAccount, and own `Deployment` + `ScaledObject` | transport identity and data identity must name the same unit |
| 9 | Every data path scoped by `ctx.subject` at one repository choke point | the runtime verifies *who is calling*; only you can scope *which rows* |

Invariants 8 and 9 come from
[tenancy-and-identity](../docs/design/tenancy-and-identity.md). Invariant 9 is
the only one on this list with no framework behind it, which is why it is also
the one that leaks.

## CI behaviour

Path-filtered. A change under `servers/kora/logs/` builds, tests, images and
publishes exactly that server. A broken Kora build cannot block a mark8ly
release, and a Kora change cannot trigger an HMS redeploy.

```
change detected → lint + typecheck → conformance testkit (no network)
                → compile manifests → assert generated files match
                → build image → push → agentic apply → wait for activation
```

Nightly, separately: **contract tests** run each connector against its product's
staging API and open an issue on divergence. This is the mitigation for the one
real cost of keeping connectors outside the product repo — schema drift that no
product test would catch. Do not disable it.

## Adding a server

```bash
mkdir -p servers/<product>/<domain> && cd servers/<product>/<domain>
uv init && uv add 'tesserix-mcp-runtime[manifest]'
```

Then follow the [authoring guide](../docs/guides/authoring-an-mcp-server.md) and
work through its checklist before opening the PR. The checklist is short and
every item on it exists because skipping it produces a connector that looks fine
and cites wrong data.
