# Klook experiences MCP

Disabled candidate. search_activities returns partner_contract_required. No unofficial endpoints, scraping or fabricated inventory. A partner API contract and sandbox credentials are needed before implementing a live adapter.

Uses the pinned published Tesserix MCP Runtime 0.1.0rc6 and native Streamable HTTP.
Gateway JWT, MCP key, Roamie tenant and read scope are checked by the runtime.
Each package owns its dependencies, lockfile, Dockerfile and compiled manifests.
Gateway export is disabled until registry and authentication integration is tested.
Secrets belong in environment/ExternalSecret references; none are checked in.

```sh
uv sync --frozen
uv run ruff check .
uv run ruff format --check .
uv run mypy --strict src/
uv run pytest
uv run python scripts/compile_manifest.py
uv build
```

The compiler uses the installed runtime version for serverInfo compatibility.
`mcp-authoring.json` is the source; `server.json` and `mcpserver.json` are generated.
Run tests and regenerate manifests before publication. Container builds were not
verified locally because the Docker daemon was unavailable. No live rollout occurred.

Ownership follows Australis ADR-0004: product connectors live with the product;
shared third-party connectors are independent Australis server packages.
