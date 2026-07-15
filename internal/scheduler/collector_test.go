package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/providertest"
	"github.com/danstis/ai-usage-dashboard/internal/store"
)

func TestCollector_UnknownProviderReturnsErrNotFound(t *testing.T) {
	t.Parallel()

	stack := newTestStack(t)
	c := NewCollector(stack.providers, stack.credentials, stack.db)

	_, err := c.Collect(context.Background(), "does-not-exist")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected store.ErrNotFound, got %v", err)
	}
}

func TestCollector_DisabledProviderReturnsErrProviderDisabled(t *testing.T) {
	t.Parallel()

	stack := newTestStack(t)
	c := NewCollector(stack.providers, stack.credentials, stack.db)

	_, err := c.Collect(context.Background(), "fake-provider")
	if !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("expected ErrProviderDisabled, got %v", err)
	}
}

func TestCollector_UncredentialedEnabledProviderReturnsErrProviderUncredentialed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	if _, err := stack.providers.SetEnabled(ctx, "fake-provider", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	c := NewCollector(stack.providers, stack.credentials, stack.db)

	_, err := c.Collect(ctx, "fake-provider")
	if !errors.Is(err, ErrProviderUncredentialed) {
		t.Fatalf("expected ErrProviderUncredentialed, got %v", err)
	}
}

func TestCollector_NoFetcherRegisteredReturnsErrFetcherNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	if _, err := stack.providers.SetEnabled(ctx, "no-creds-provider", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	c := NewCollector(stack.providers, stack.credentials, stack.db)

	_, err := c.Collect(ctx, "no-creds-provider")
	if !errors.Is(err, provider.ErrFetcherNotFound) {
		t.Fatalf("expected provider.ErrFetcherNotFound, got %v", err)
	}
}

func TestCollector_PropagatesFetcherError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	if _, err := stack.providers.SetEnabled(ctx, "no-creds-provider", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	upstream := errors.New("upstream down")
	f := providertest.NewFetcher(provider.Metadata{ID: "no-creds-provider"}, nil)
	f.SetError(upstream)
	stack.providers.RegisterFetcher(f)

	c := NewCollector(stack.providers, stack.credentials, stack.db)
	_, err := c.Collect(ctx, "no-creds-provider")
	if !errors.Is(err, upstream) {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

// TestCollector_FullAcceptanceLoop is the P2 acceptance path: register a
// Fetcher, credential its provider, enable it, Collect, and assert the
// resulting snapshot carries the Fetcher's metrics and the credentials it
// was called with round-tripped through the encrypted credential store.
func TestCollector_FullAcceptanceLoop(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)

	meta := provider.Metadata{
		ID:   "fake-provider",
		Name: "Fake Provider",
		CredentialFields: []provider.CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	}
	limit := int64(1000)
	want := []provider.UsageMetric{
		{Name: "monthly_spend", Window: "month", Unit: "usd_cents", Used: 250, Limit: &limit},
	}
	fetcher := providertest.NewFetcher(meta, want)
	stack.providers.RegisterFetcher(fetcher)

	if err := stack.credentials.SetValues(ctx, "fake-provider", map[string]string{"api_key": "sk-test-123"}); err != nil {
		t.Fatalf("set credentials: %v", err)
	}
	if _, err := stack.providers.SetEnabled(ctx, "fake-provider", true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	c := NewCollector(stack.providers, stack.credentials, stack.db)
	before := time.Now().UTC()
	snap, err := c.Collect(ctx, "fake-provider")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}

	if fetcher.CallCount() != 1 {
		t.Fatalf("expected Fetcher to be called once, got %d", fetcher.CallCount())
	}
	if fetcher.LastCreds()["api_key"] != "sk-test-123" {
		t.Fatalf("expected Fetcher to receive decrypted credentials, got %+v", fetcher.LastCreds())
	}

	if len(snap.Metrics) != 1 || snap.Metrics[0].Name != "monthly_spend" || snap.Metrics[0].Used != 250 {
		t.Fatalf("unexpected snapshot metrics: %+v", snap.Metrics)
	}
	if snap.CollectedAt.Before(before) {
		t.Fatalf("expected CollectedAt >= %v, got %v", before, snap.CollectedAt)
	}

	// The snapshot must also be durably persisted, not just returned.
	persisted, err := stack.db.GetSnapshot(ctx, "fake-provider")
	if err != nil {
		t.Fatalf("GetSnapshot() returned error: %v", err)
	}
	if len(persisted.Metrics) != 1 || persisted.Metrics[0].Used != 250 {
		t.Fatalf("expected persisted snapshot to match, got %+v", persisted.Metrics)
	}
}

func TestCollector_ProviderWithNoCredentialFieldsNeedsNoCredentials(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	if _, err := stack.providers.SetEnabled(ctx, "no-creds-provider", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	f := providertest.NewFetcher(provider.Metadata{ID: "no-creds-provider"}, []provider.UsageMetric{
		{Name: "requests", Window: "day", Unit: "count", Used: 1},
	})
	stack.providers.RegisterFetcher(f)

	c := NewCollector(stack.providers, stack.credentials, stack.db)
	snap, err := c.Collect(ctx, "no-creds-provider")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(f.LastCreds()) != 0 {
		t.Fatalf("expected empty credential map for a provider with no declared fields, got %+v", f.LastCreds())
	}
	if len(snap.Metrics) != 1 || snap.Metrics[0].Used != 1 {
		t.Fatalf("unexpected snapshot metrics: %+v", snap.Metrics)
	}
}

func TestCollector_SecondCollectReplacesSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	if _, err := stack.providers.SetEnabled(ctx, "no-creds-provider", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	f := providertest.NewFetcher(provider.Metadata{ID: "no-creds-provider"}, []provider.UsageMetric{
		{Name: "requests", Window: "day", Unit: "count", Used: 1},
	})
	stack.providers.RegisterFetcher(f)
	c := NewCollector(stack.providers, stack.credentials, stack.db)

	if _, err := c.Collect(ctx, "no-creds-provider"); err != nil {
		t.Fatalf("first Collect() returned error: %v", err)
	}

	f.SetMetrics([]provider.UsageMetric{{Name: "requests", Window: "day", Unit: "count", Used: 2}})

	snap, err := c.Collect(ctx, "no-creds-provider")
	if err != nil {
		t.Fatalf("second Collect() returned error: %v", err)
	}
	if len(snap.Metrics) != 1 || snap.Metrics[0].Used != 2 {
		t.Fatalf("expected replace-on-write with Used=2, got %+v", snap.Metrics)
	}
}
