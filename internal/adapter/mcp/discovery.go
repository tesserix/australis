package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

type LiveTool struct {
	Name         string
	Description  string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
}

func DiscoverTools(ctx context.Context, httpClient *http.Client, endpoint string) ([]LiveTool, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("discover MCP tools: HTTP client is required")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("discover MCP tools: endpoint is required")
	}

	client := protocol.NewClient(
		&protocol.Implementation{Name: "australis", Version: "0.1.0"},
		nil,
	)
	session, err := client.Connect(ctx, &protocol.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           httpClient,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("discover MCP tools: connect: %w", err)
	}

	tools, listErr := listTools(ctx, session)
	closeErr := session.Close()
	if listErr != nil {
		return nil, listErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("discover MCP tools: close session: %w", closeErr)
	}
	return tools, nil
}

func listTools(ctx context.Context, session *protocol.ClientSession) ([]LiveTool, error) {
	var tools []LiveTool
	params := &protocol.ListToolsParams{}
	for {
		page, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("discover MCP tools: list: %w", err)
		}
		for _, tool := range page.Tools {
			input, err := marshalSchema(tool.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("discover MCP tools: %s input schema: %w", tool.Name, err)
			}
			output, err := marshalSchema(tool.OutputSchema)
			if err != nil {
				return nil, fmt.Errorf("discover MCP tools: %s output schema: %w", tool.Name, err)
			}
			tools = append(tools, LiveTool{
				Name:         tool.Name,
				Description:  tool.Description,
				InputSchema:  input,
				OutputSchema: output,
			})
		}
		if page.NextCursor == "" {
			return tools, nil
		}
		params.Cursor = page.NextCursor
	}
}

func marshalSchema(schema any) (json.RawMessage, error) {
	if schema == nil {
		return nil, nil
	}
	document, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return document, nil
}
