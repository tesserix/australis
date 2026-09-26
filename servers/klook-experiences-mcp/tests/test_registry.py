import json
from pathlib import Path


def test_registry_routes_use_service_discovery_and_workload_key():
    doc = json.loads((Path(__file__).parents[1] / "mcpserver.json").read_text())
    assert doc["metadata"]["name"] == "klook-experiences-mcp"
    assert doc["spec"]["name"] == "klook-experiences-mcp"
    assert doc["spec"]["credentialRef"]["header"] == "X-MCP-Key"
    assert doc["spec"]["serviceSelector"]["namespaces"]["matchLabels"] == {
        "kubernetes.io/metadata.name": "australis"
    }
    assert doc["spec"]["serviceSelector"]["services"]["matchLabels"] == {
        "app.kubernetes.io/name": "klook-experiences-mcp"
    }
    assert doc["metadata"]["labels"]["mcp.tesserix.app/gateway-export"] == "false"
