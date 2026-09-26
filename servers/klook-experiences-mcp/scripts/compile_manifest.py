import json
from importlib.metadata import version
from pathlib import Path

from tesserix_mcp_manifest import ServerAuthoringManifest, ToolSummary, compile_manifests

from klook_experiences_mcp.server import catalog

document = json.loads(Path("mcp-authoring.json").read_text())
document["tools"] = [
    ToolSummary.from_runtime(tool).model_dump(mode="json") for tool in catalog().manifests
]
compiled = compile_manifests(
    ServerAuthoringManifest.model_validate_json(json.dumps(document)),
    runtime_version=version("tesserix-mcp-runtime"),
)
Path("server.json").write_bytes(compiled.server_json)
registry = json.loads(compiled.registry_manifest)
registry["metadata"]["name"] = "klook-experiences-mcp"
registry["spec"]["name"] = "klook-experiences-mcp"
registry["spec"]["credentialRef"]["header"] = "X-MCP-Key"
registry["spec"]["serviceSelector"] = {
    "namespaces": {"matchLabels": {"kubernetes.io/metadata.name": "australis"}},
    "services": {"matchLabels": {"app.kubernetes.io/name": "klook-experiences-mcp"}},
}
Path("mcpserver.json").write_text(json.dumps(registry, indent=2, sort_keys=True) + "\n")
