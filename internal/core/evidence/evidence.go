package evidence

import (
	"fmt"
	"strings"
	"time"
)

type CitationKind string

const (
	CitationTool     CitationKind = "tool"
	CitationDocument CitationKind = "document"
)

type Citation struct {
	Kind    CitationKind
	Source  string
	Locator string
	Digest  string
}

type Item struct {
	Content   string
	Citation  Citation
	Score     float64
	Retrieved time.Time
}

type Bundle struct {
	items []Item
}

func NewBundle(items ...Item) (Bundle, error) {
	for i, item := range items {
		if err := item.validate(); err != nil {
			return Bundle{}, fmt.Errorf("evidence %d: %w", i, err)
		}
	}
	return Bundle{items: append([]Item(nil), items...)}, nil
}

func (b Bundle) Items() []Item {
	return append([]Item(nil), b.items...)
}

func (item Item) validate() error {
	if strings.TrimSpace(item.Content) == "" {
		return fmt.Errorf("content is required")
	}
	if item.Citation.Kind != CitationTool && item.Citation.Kind != CitationDocument {
		return fmt.Errorf("citation kind is invalid")
	}
	if strings.TrimSpace(item.Citation.Source) == "" {
		return fmt.Errorf("citation source is required")
	}
	if strings.TrimSpace(item.Citation.Locator) == "" {
		return fmt.Errorf("citation locator is required")
	}
	if strings.TrimSpace(item.Citation.Digest) == "" {
		return fmt.Errorf("citation digest is required")
	}
	if item.Score < 0 || item.Score > 1 {
		return fmt.Errorf("score must be between zero and one")
	}
	if item.Retrieved.IsZero() {
		return fmt.Errorf("retrieval time is required")
	}
	return nil
}
