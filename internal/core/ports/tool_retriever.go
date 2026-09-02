package ports

import (
	"context"
	"encoding/json"

	"github.com/tesserix/australis/internal/core/evidence"
)

type ToolRetriever interface {
	Tools(ctx context.Context) ([]ToolDescriptor, error)
	Invoke(ctx context.Context, call ToolCall) (evidence.Bundle, error)
}

type ToolDescriptor struct {
	Name           string
	Summary        string
	WhenToUse      []string
	NotFor         []string
	InputSchema    json.RawMessage
	OutputSchema   json.RawMessage
	Idempotency    Idempotency
	RiskLevel      Risk
	RequiredScopes []string
}

type ToolCall struct {
	Tool      string
	Arguments json.RawMessage
	TenantID  string
	RequestID string
}

type Idempotency string

const IdempotencyNotApplicable Idempotency = "not_applicable"

type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)
