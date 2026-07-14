package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

func int64Ptr(v int64) *int64 { return &v }

func TestSnapshots_GetSnapshot_NeverCollectedReturnsErrNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.GetSnapshot(ctx, "openai"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestSnapshots_ReplaceThenGetSnapshot_RoundTripsIncludingNilFields(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	collectedAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	metrics := []store.Metric{
		{Name: "requests", Window: "day", Unit: "count", Used: 42, Limit: int64Ptr(100), Remaining: int64Ptr(58), ResetAt: &collectedAt},
		{Name: "spend", Window: "month", Unit: "usd_cents", Used: 500, Limit: nil, Remaining: nil, ResetAt: nil},
	}

	if err := s.Replace(ctx, "openai", metrics, collectedAt); err != nil {
		t.Fatalf("Replace() returned error: %v", err)
	}

	got, err := s.GetSnapshot(ctx, "openai")
	if err != nil {
		t.Fatalf("GetSnapshot() returned error: %v", err)
	}
	if got.ProviderID != "openai" {
		t.Fatalf("expected ProviderID %q, got %q", "openai", got.ProviderID)
	}
	if !got.CollectedAt.Equal(collectedAt) {
		t.Fatalf("expected CollectedAt %v, got %v", collectedAt, got.CollectedAt)
	}
	if got.LastError != nil {
		t.Fatalf("expected nil LastError, got %v", *got.LastError)
	}
	if len(got.Metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %+v", got.Metrics)
	}
	if got.Metrics[0].Limit == nil || *got.Metrics[0].Limit != 100 {
		t.Fatalf("expected first metric Limit=100, got %+v", got.Metrics[0])
	}
	if got.Metrics[1].Limit != nil {
		t.Fatalf("expected second metric Limit=nil, got %+v", got.Metrics[1])
	}
	if got.Metrics[1].Remaining != nil {
		t.Fatalf("expected second metric Remaining=nil, got %+v", got.Metrics[1])
	}
	if got.Metrics[1].ResetAt != nil {
		t.Fatalf("expected second metric ResetAt=nil, got %+v", got.Metrics[1])
	}
}

func TestSnapshots_SecondReplaceOverwritesNoAccumulation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	first := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	second := time.Date(2026, 7, 14, 11, 0, 0, 0, time.UTC)

	if err := s.Replace(ctx, "openai", []store.Metric{{Name: "requests", Window: "day", Unit: "count", Used: 1}}, first); err != nil {
		t.Fatalf("first Replace() returned error: %v", err)
	}
	if err := s.Replace(ctx, "openai", []store.Metric{{Name: "requests", Window: "day", Unit: "count", Used: 2}}, second); err != nil {
		t.Fatalf("second Replace() returned error: %v", err)
	}

	got, err := s.GetSnapshot(ctx, "openai")
	if err != nil {
		t.Fatalf("GetSnapshot() returned error: %v", err)
	}
	if len(got.Metrics) != 1 {
		t.Fatalf("expected replace-on-write with exactly 1 metric, got %+v", got.Metrics)
	}
	if got.Metrics[0].Used != 2 {
		t.Fatalf("expected latest Used=2, got %d", got.Metrics[0].Used)
	}
	if !got.CollectedAt.Equal(second) {
		t.Fatalf("expected CollectedAt %v, got %v", second, got.CollectedAt)
	}
}

func TestSnapshots_ReplaceDoesNotAffectOtherProviders(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	if err := s.Replace(ctx, "openai", []store.Metric{{Name: "requests", Window: "day", Unit: "count", Used: 1}}, now); err != nil {
		t.Fatalf("Replace(openai) returned error: %v", err)
	}

	if _, err := s.GetSnapshot(ctx, "anthropic"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for anthropic, got: %v", err)
	}
}
