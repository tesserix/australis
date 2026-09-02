package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	mcpadapter "github.com/tesserix/australis/internal/adapter/mcp"
)

func TestValidateToolAcceptsPinnedReadOnlyClosedContract(t *testing.T) {
	t.Parallel()

	tool := validTool(t)
	if err := mcpadapter.ValidateTool(tool); err != nil {
		t.Fatalf("ValidateTool() error = %v", err)
	}
}

func TestValidateToolFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edit func(*mcpadapter.ToolContract)
		want string
	}{
		{name: "missing output schema", edit: func(tool *mcpadapter.ToolContract) { tool.OutputSchema = nil }, want: "output schema"},
		{name: "open input schema", edit: func(tool *mcpadapter.ToolContract) { tool.InputSchema = schema(t, `{"type":"object","properties":{}}`) }, want: "closed"},
		{name: "open nested output", edit: func(tool *mcpadapter.ToolContract) {
			tool.OutputSchema = schema(t, `{"type":"object","additionalProperties":false,"properties":{"summary":{"type":"object","properties":{}}}}`)
		}, want: "closed"},
		{name: "write effect", edit: func(tool *mcpadapter.ToolContract) { tool.Effects = []string{"write"} }, want: "read-only"},
		{name: "direct access", edit: func(tool *mcpadapter.ToolContract) { tool.DirectAccess = true }, want: "direct access"},
		{name: "tenant identity input", edit: func(tool *mcpadapter.ToolContract) {
			tool.InputSchema = schema(t, `{"type":"object","additionalProperties":false,"properties":{"tenant_id":{"type":"string"}}}`)
		}, want: "tenant_id"},
		{name: "subject identity input", edit: func(tool *mcpadapter.ToolContract) {
			tool.InputSchema = schema(t, `{"type":"object","additionalProperties":false,"properties":{"filter":{"type":"object","additionalProperties":false,"properties":{"subject":{"type":"string"}}}}}`)
		}, want: "subject"},
		{name: "identity in referenced definition", edit: func(tool *mcpadapter.ToolContract) {
			tool.InputSchema = schema(t, `{"type":"object","additionalProperties":false,"properties":{"filter":{"$ref":"#/$defs/filter"}},"$defs":{"filter":{"type":"object","additionalProperties":false,"properties":{"user_id":{"type":"string"}}}}}`)
		}, want: "user_id"},
		{name: "input fingerprint mismatch", edit: func(tool *mcpadapter.ToolContract) { tool.InputFingerprint = strings.Repeat("0", 64) }, want: "input fingerprint"},
		{name: "output fingerprint mismatch", edit: func(tool *mcpadapter.ToolContract) { tool.OutputFingerprint = strings.Repeat("0", 64) }, want: "output fingerprint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tool := validTool(t)
			tt.edit(&tool)
			err := mcpadapter.ValidateTool(tool)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateTool() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestFingerprintSchemaMatchesRuntimeCanonicalJSON(t *testing.T) {
	t.Parallel()

	digest, err := mcpadapter.FingerprintSchema(schema(t, `{"description":"daily ≤ target","required":[],"properties":{},"additionalProperties":false,"type":"object"}`))
	if err != nil {
		t.Fatalf("FingerprintSchema() error = %v", err)
	}
	const want = "ad90319d69793d7917a25bcbd18b93ece8d195320506f3fe1920656781881292"
	if digest != want {
		t.Fatalf("FingerprintSchema() = %q, want %q", digest, want)
	}
}

func validTool(t *testing.T) mcpadapter.ToolContract {
	t.Helper()
	input := schema(t, `{"type":"object","additionalProperties":false,"properties":{"start_date":{"type":"string"}},"required":["start_date"]}`)
	output := schema(t, `{"type":"object","additionalProperties":false,"properties":{"source_locator":{"type":"string"}},"required":["source_locator"]}`)
	inputFingerprint, err := mcpadapter.FingerprintSchema(input)
	if err != nil {
		t.Fatalf("FingerprintSchema(input) error = %v", err)
	}
	outputFingerprint, err := mcpadapter.FingerprintSchema(output)
	if err != nil {
		t.Fatalf("FingerprintSchema(output) error = %v", err)
	}
	return mcpadapter.ToolContract{
		Name:              "daily_log_summary",
		InputSchema:       input,
		OutputSchema:      output,
		InputFingerprint:  inputFingerprint,
		OutputFingerprint: outputFingerprint,
		DirectAccess:      false,
	}
}

func schema(t *testing.T, value string) json.RawMessage {
	t.Helper()
	if !json.Valid([]byte(value)) {
		t.Fatalf("invalid test schema: %s", value)
	}
	return json.RawMessage(value)
}
