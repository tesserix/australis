import asyncio
import json
from importlib.metadata import version
from pathlib import Path

import httpx
from tesserix_mcp_manifest import ServerAuthoringManifest, ToolSummary, compile_manifests

from booking_demand_mcp.booking import BookingClient
from booking_demand_mcp.server import catalog


async def describe_tools():
    async with httpx.AsyncClient() as http:
        tools = catalog(BookingClient(http, affiliate_id="", token=""))
        return [ToolSummary.from_runtime(tool).model_dump(mode="json") for tool in tools.manifests]


document = json.loads(Path("mcp-authoring.json").read_text())
document["tools"] = asyncio.run(describe_tools())
manifest = ServerAuthoringManifest.model_validate_json(json.dumps(document))
compiled = compile_manifests(manifest, runtime_version=version("tesserix-mcp-runtime"))
Path("server.json").write_bytes(compiled.server_json)
registry = json.loads(compiled.registry_manifest)
registry["metadata"]["name"] = "booking-demand-mcp"
registry["spec"]["name"] = "booking-demand-mcp"
registry["spec"]["credentialRef"]["header"] = "X-MCP-Key"
registry["spec"]["serviceSelector"] = {
    "namespaces": {"matchLabels": {"kubernetes.io/metadata.name": "australis"}},
    "services": {"matchLabels": {"app.kubernetes.io/name": "booking-demand-mcp"}},
}
Path("mcpserver.json").write_text(json.dumps(registry, indent=2, sort_keys=True) + "\n")
