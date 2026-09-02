package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	mcpadapter "github.com/tesserix/australis/internal/adapter/mcp"
)

type lookupInput struct {
	StartDate string `json:"start_date" jsonschema:"inclusive RFC 3339 date"`
}

type lookupOutput struct {
	SourceLocator string `json:"source_locator" jsonschema:"stable citation locator"`
}

func TestDiscoverToolsReturnsLiveInputAndOutputSchemas(t *testing.T) {
	t.Parallel()

	server := protocol.NewServer(&protocol.Implementation{Name: "kora-logs", Version: "1.0.0"}, nil)
	protocol.AddTool(server, &protocol.Tool{
		Name:        "daily_log_summary",
		Description: "Return the caller's daily nutrition totals.",
	}, func(context.Context, *protocol.CallToolRequest, lookupInput) (*protocol.CallToolResult, lookupOutput, error) {
		return &protocol.CallToolResult{}, lookupOutput{SourceLocator: "date=2026-09-01"}, nil
	})

	handler := protocol.NewStreamableHTTPHandler(
		func(*http.Request) *protocol.Server { return server },
		&protocol.StreamableHTTPOptions{Stateless: true},
	)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	tools, err := mcpadapter.DiscoverTools(t.Context(), httpServer.Client(), httpServer.URL)
	if err != nil {
		t.Fatalf("DiscoverTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("DiscoverTools() returned %d tools, want 1", len(tools))
	}
	if tools[0].Name != "daily_log_summary" {
		t.Fatalf("tool name = %q, want daily_log_summary", tools[0].Name)
	}
	if len(tools[0].InputSchema) == 0 {
		t.Fatal("input schema is empty")
	}
	if len(tools[0].OutputSchema) == 0 {
		t.Fatal("output schema is empty")
	}
}
