package evidence_test

import (
	"testing"
	"time"

	"github.com/tesserix/australis/internal/core/evidence"
)

func TestNewBundleAcceptsGroundedEvidence(t *testing.T) {
	t.Parallel()

	item := evidence.Item{
		Content: "2,100 kcal",
		Citation: evidence.Citation{
			Kind:    evidence.CitationTool,
			Source:  "kora-logs/daily_log_summary",
			Locator: "date=2026-09-01",
			Digest:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		Score:     0.92,
		Retrieved: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
	}

	bundle, err := evidence.NewBundle(item)
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if got := bundle.Items(); len(got) != 1 || got[0] != item {
		t.Fatalf("Items() = %#v, want %#v", got, []evidence.Item{item})
	}
}

func TestNewBundleRejectsUngroundedEvidence(t *testing.T) {
	t.Parallel()

	valid := evidence.Item{
		Content: "2,100 kcal",
		Citation: evidence.Citation{
			Kind:    evidence.CitationTool,
			Source:  "kora-logs/daily_log_summary",
			Locator: "date=2026-09-01",
			Digest:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		Score:     0.92,
		Retrieved: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name string
		edit func(*evidence.Item)
	}{
		{name: "empty content", edit: func(item *evidence.Item) { item.Content = "" }},
		{name: "missing source", edit: func(item *evidence.Item) { item.Citation.Source = "" }},
		{name: "missing locator", edit: func(item *evidence.Item) { item.Citation.Locator = "" }},
		{name: "missing digest", edit: func(item *evidence.Item) { item.Citation.Digest = "" }},
		{name: "score below zero", edit: func(item *evidence.Item) { item.Score = -0.01 }},
		{name: "score above one", edit: func(item *evidence.Item) { item.Score = 1.01 }},
		{name: "missing retrieval time", edit: func(item *evidence.Item) { item.Retrieved = time.Time{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := valid
			tt.edit(&item)
			if _, err := evidence.NewBundle(item); err == nil {
				t.Fatal("NewBundle() error = nil, want validation error")
			}
		})
	}
}

func TestBundleItemsReturnsCopy(t *testing.T) {
	t.Parallel()

	item := evidence.Item{
		Content: "grounded",
		Citation: evidence.Citation{
			Kind:    evidence.CitationDocument,
			Source:  "guide",
			Locator: "section=1",
			Digest:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		Score:     1,
		Retrieved: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
	}
	bundle, err := evidence.NewBundle(item)
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}

	items := bundle.Items()
	items[0].Content = "mutated"
	if got := bundle.Items()[0].Content; got != "grounded" {
		t.Fatalf("bundle content = %q, want grounded", got)
	}
}
