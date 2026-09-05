# `servers/` — Australis-owned MCP connectors

Product-domain connectors live in their product repository by default. This
directory is reserved for connectors whose backing data and release lifecycle
are owned by Australis. See
[ADR-0004](../docs/adr/0004-product-owned-mcp-connectors.md) and the
[product onboarding guide](../docs/guides/product-mcp-onboarding.md).

## The rule that makes this safe

> **Federated source, one platform contract.**

Every server remains an independent build unit with its own lockfile, image,
tag, Registry object, credential and deployment. Product connectors follow the
same contract in their owning repository; there is no shared Australis servers
image and no deployment that carries two tenants at once.

## Layout

```text
servers/
├─ _shared/                  vetted shared library — keep small, or empty
└─ australis-evals/          example engine-owned connector
   ├─ pyproject.toml         own lockfile
   ├─ Dockerfile             own image
   ├─ mcp-authoring.json     manifest source
   ├─ server.json            generated — do not hand-edit
   ├─ mcpserver.yaml         generated — do not hand-edit
   ├─ src/
   └─ tests/
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
| 9 | Private data paths use verified `ctx.subject` at one repository choke point; public service reads declare that profile explicitly | the runtime verifies caller context when required; only the product can authorize rows |

Invariants 8 and 9 come from
[tenancy-and-identity](../docs/design/tenancy-and-identity.md). Invariant 9 is
the only one on this list with no framework behind it, which is why it is also
the one that leaks.

## CI behaviour

Path-filtered. A change under one Australis-owned server builds, tests, images
and publishes exactly that server. Product-owned connector CI lives in the
product repository and follows the same sequence.

```text
change detected → lint + typecheck → conformance testkit (no network)
                → compile manifests → assert generated files match
                → build image → push → agentic apply → wait for activation
```

Product-owned connectors run **contract tests** with their product API CI.
Nightly staging tests remain mandatory only for exceptional cross-repository
dependencies.

## Adding a server

```bash
mkdir -p servers/<domain> && cd servers/<domain>
uv init
uv add "tesserix-mcp-runtime[manifest] @ https://github.com/tesserix/tesserix-mcp-runtime/releases/download/v0.1.0-rc.6/tesserix_mcp_runtime-0.1.0rc6-py3-none-any.whl"
```

Then follow the [authoring guide](../docs/guides/authoring-an-mcp-server.md) and
the [product onboarding guide](../docs/guides/product-mcp-onboarding.md) before
opening the PR.
