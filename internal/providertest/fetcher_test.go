package providertest

import (
	"context"
	"errors"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

func TestFetcherTracksCallsAndClonesState(t *testing.T) {
	t.Parallel()

	limit := int64(10)
	fetcher := NewFetcher(
		provider.Metadata{ID: "openai"},
		[]provider.UsageMetric{{Name: "requests", Window: "day", Unit: "count", Used: 2, Limit: &limit}},
	)

	creds := map[string]string{"api_key": "secret"}
	metrics, err := fetcher.FetchUsage(context.Background(), creds)
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if fetcher.CallCount() != 1 {
		t.Fatalf("expected CallCount 1, got %d", fetcher.CallCount())
	}
	if fetcher.LastCreds()["api_key"] != "secret" {
		t.Fatalf("expected cloned credentials, got %+v", fetcher.LastCreds())
	}

	creds["api_key"] = "mutated"
	metrics[0].Used = 99
	if fetcher.LastCreds()["api_key"] != "secret" {
		t.Fatalf("expected credentials clone to be stable, got %+v", fetcher.LastCreds())
	}

	fetcher.SetMetrics([]provider.UsageMetric{{Name: "requests", Window: "day", Unit: "count", Used: 5}})
	fetcher.SetError(errors.New("boom"))
	if _, err := fetcher.FetchUsage(context.Background(), nil); err == nil {
		t.Fatal("expected FetchUsage() to return the configured error")
	}
}
