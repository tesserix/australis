package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type ToolContract struct {
	Name              string
	InputSchema       json.RawMessage
	OutputSchema      json.RawMessage
	InputFingerprint  string
	OutputFingerprint string
	Effects           []string
	DirectAccess      bool
}

func ValidateTool(tool ToolContract) error {
	if strings.TrimSpace(tool.Name) == "" {
		return fmt.Errorf("tool name is required")
	}
	input, err := decodeSchema(tool.InputSchema)
	if err != nil {
		return fmt.Errorf("input schema: %w", err)
	}
	output, err := decodeSchema(tool.OutputSchema)
	if err != nil {
		return fmt.Errorf("output schema: %w", err)
	}
	if err := validateClosedSchema(input, "input schema"); err != nil {
		return err
	}
	if err := validateClosedSchema(output, "output schema"); err != nil {
		return err
	}
	if field, ok := identityField(input); ok {
		return fmt.Errorf("input schema contains identity field %q", field)
	}
	if len(tool.Effects) != 0 {
		return fmt.Errorf("tool must be read-only")
	}
	if tool.DirectAccess {
		return fmt.Errorf("direct access must be disabled")
	}
	if err := verifyFingerprint("input", tool.InputSchema, tool.InputFingerprint); err != nil {
		return err
	}
	if err := verifyFingerprint("output", tool.OutputSchema, tool.OutputFingerprint); err != nil {
		return err
	}
	return nil
}

func FingerprintSchema(schema json.RawMessage) (string, error) {
	value, err := decodeJSON(schema)
	if err != nil {
		return "", err
	}

	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("encode canonical schema: %w", err)
	}
	digest := sha256.Sum256(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")))
	return hex.EncodeToString(digest[:]), nil
}

func verifyFingerprint(kind string, schema json.RawMessage, expected string) error {
	actual, err := FingerprintSchema(schema)
	if err != nil {
		return fmt.Errorf("%s fingerprint: %w", kind, err)
	}
	if actual != expected {
		return fmt.Errorf("%s fingerprint mismatch", kind)
	}
	return nil
}

func decodeSchema(schema json.RawMessage) (map[string]any, error) {
	if len(schema) == 0 {
		return nil, fmt.Errorf("is required")
	}
	value, err := decodeJSON(schema)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be a JSON object")
	}
	return object, nil
}

func decodeJSON(document []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode JSON: trailing content")
	}
	return value, nil
}

func validateClosedSchema(schema map[string]any, path string) error {
	if schema["type"] != "object" {
		return fmt.Errorf("%s must describe an object", path)
	}
	return walkObjects(schema, path)
}

func walkObjects(value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		_, hasProperties := typed["properties"]
		if typed["type"] == "object" || hasProperties {
			closed, ok := typed["additionalProperties"].(bool)
			if !ok || closed {
				return fmt.Errorf("%s must be closed with additionalProperties=false", path)
			}
		}
		for key, child := range typed {
			if err := walkObjects(child, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range typed {
			if err := walkObjects(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func identityField(value any) (string, bool) {
	schema, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for name := range properties {
			if _, blocked := blockedIdentityFields[normaliseField(name)]; blocked {
				return name, true
			}
		}
	}
	for _, child := range schema {
		if field, blocked := identityField(child); blocked {
			return field, true
		}
		items, ok := child.([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			if field, blocked := identityField(item); blocked {
				return field, true
			}
		}
	}
	return "", false
}

func normaliseField(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, name)
}

var blockedIdentityFields = map[string]struct{}{
	"actorid":     {},
	"ownerid":     {},
	"principal":   {},
	"principalid": {},
	"subject":     {},
	"subjectid":   {},
	"tenant":      {},
	"tenantid":    {},
	"user":        {},
	"userid":      {},
}
