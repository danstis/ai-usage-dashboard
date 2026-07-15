package plugintest

import (
	"context"
	"errors"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/providertest"
)

var fixtureMeta = provider.Metadata{
	ID:   "fixture",
	Name: "Fixture Provider",
	CredentialFields: []provider.CredentialField{
		{Name: "api_key", Label: "API Key", Secret: true},
	},
}

// TestStack_LiveMockShape demonstrates the shape a plugin's FetchUsage test
// takes once it registers a Fetcher: the provider becomes "live"
// (HasFetcher/Provider.Live true) and FetchUsage resolves credentials
// through the same production wiring Collector uses.
func TestStack_LiveMockShape(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := NewStack(fixtureMeta)
	fetcher := providertest.NewFetcher(fixtureMeta, []provider.UsageMetric{
		{Name: "monthly_spend", Window: "month", Unit: "usd_cents", Used: 500},
	})
	stack.Providers.RegisterFetcher(fetcher)

	if !stack.Providers.HasFetcher(fixtureMeta.ID) {
		t.Fatal("expected the provider to be live once a Fetcher is registered")
	}

	creds, err := stack.Reveal(ctx, fixtureMeta.ID, map[string]string{"api_key": "sk-test"})
	if err != nil {
		t.Fatalf("Reveal() returned error: %v", err)
	}

	got, err := stack.Providers.FetchUsage(ctx, fixtureMeta.ID, creds)
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if len(got) != 1 || got[0].Used != 500 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
	if fetcher.LastCreds()["api_key"] != "sk-test" {
		t.Fatalf("expected the resolved credential to reach the Fetcher, got %+v", fetcher.LastCreds())
	}
}

// TestStack_ScaffoldedMissingFetcherShape demonstrates the other shape a
// plugin's test suite should cover before it registers a Fetcher: a
// metadata-only entry is not live and FetchUsage reports
// provider.ErrFetcherNotFound rather than a silent failure.
func TestStack_ScaffoldedMissingFetcherShape(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := NewStack(fixtureMeta)

	if stack.Providers.HasFetcher(fixtureMeta.ID) {
		t.Fatal("expected a Stack with no registered Fetcher to be scaffolded, not live")
	}

	_, err := stack.Providers.FetchUsage(ctx, fixtureMeta.ID, nil)
	if !errors.Is(err, provider.ErrFetcherNotFound) {
		t.Fatalf("expected ErrFetcherNotFound for a scaffolded provider, got %v", err)
	}
}
