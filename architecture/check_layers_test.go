package main

import "testing"

func TestForbiddenImport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		imported string
		blocked  bool
	}{
		{name: "core to standard library", path: "internal/core/evidence/evidence.go", imported: "time"},
		{name: "core to core", path: "internal/core/ports/tool_retriever.go", imported: module + "/internal/core/evidence"},
		{name: "adapter to MCP SDK", path: "internal/adapter/mcp/discovery.go", imported: "github.com/modelcontextprotocol/go-sdk/mcp"},
		{name: "core to adapter", path: "internal/core/retrieval/retrieval.go", imported: module + "/internal/adapter/mcp", blocked: true},
		{name: "core to MCP SDK", path: "internal/core/retrieval/retrieval.go", imported: "github.com/modelcontextprotocol/go-sdk/mcp", blocked: true},
		{name: "other adapter to MCP SDK", path: "internal/adapter/registry/client.go", imported: "github.com/modelcontextprotocol/go-sdk/mcp", blocked: true},
		{name: "engine to server", path: "internal/tenant/config.go", imported: module + "/servers/kora/logs", blocked: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := forbiddenImport(tt.path, tt.imported) != ""
			if got != tt.blocked {
				t.Fatalf("forbiddenImport(%q, %q) blocked = %t, want %t", tt.path, tt.imported, got, tt.blocked)
			}
		})
	}
}
