package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// waitFor polls cond until it returns true or timeout elapses, failing the
// test otherwise.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestScheduler_TickCollectsOnlyEnabledProviders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	if _, err := stack.providers.SetEnabled(ctx, "no-creds-provider", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// fake-provider is left disabled.

	enabled := newTestFetcher(provider.Metadata{ID: "no-creds-provider"}, []provider.UsageMetric{
		{Name: "requests", Window: "day", Unit: "count", Used: 1},
	})
	disabled := newTestFetcher(provider.Metadata{ID: "fake-provider"}, nil)
	stack.providers.RegisterFetcher(enabled)
	stack.providers.RegisterFetcher(disabled)

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second)

	s.tick(ctx)

	if enabled.callCount() != 1 {
		t.Errorf("expected enabled provider to be collected once, got %d calls", enabled.callCount())
	}
	if disabled.callCount() != 0 {
		t.Errorf("expected disabled provider to never be collected, got %d calls", disabled.callCount())
	}
}

func TestScheduler_FailingProviderDoesNotStopOtherCollections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	if _, err := stack.providers.SetEnabled(ctx, "no-creds-provider", true); err != nil {
		t.Fatalf("enable no-creds-provider: %v", err)
	}
	if _, err := stack.providers.SetEnabled(ctx, "fake-provider", true); err != nil {
		t.Fatalf("enable fake-provider: %v", err)
	}
	if err := stack.credentials.SetValues(ctx, "fake-provider", map[string]string{"api_key": "k"}); err != nil {
		t.Fatalf("set credentials: %v", err)
	}

	failing := newTestFetcher(provider.Metadata{ID: "no-creds-provider"}, nil)
	failing.err = errors.New("upstream timeout")
	succeeding := newTestFetcher(provider.Metadata{
		ID: "fake-provider",
		CredentialFields: []provider.CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	}, []provider.UsageMetric{{Name: "requests", Window: "day", Unit: "count", Used: 5}})
	stack.providers.RegisterFetcher(failing)
	stack.providers.RegisterFetcher(succeeding)

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second)

	s.tick(ctx)

	if failing.callCount() != 1 {
		t.Errorf("expected failing provider to still be attempted once, got %d", failing.callCount())
	}
	snap, err := stack.db.GetSnapshot(ctx, "fake-provider")
	if err != nil {
		t.Fatalf("expected the succeeding provider's snapshot to be persisted despite the other's failure: %v", err)
	}
	if len(snap.Metrics) != 1 || snap.Metrics[0].Used != 5 {
		t.Fatalf("unexpected snapshot for succeeding provider: %+v", snap.Metrics)
	}
}

func TestScheduler_UnregisteredFetcherDoesNotStopOtherCollections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	if _, err := stack.providers.SetEnabled(ctx, "no-creds-provider", true); err != nil {
		t.Fatalf("enable no-creds-provider: %v", err)
	}
	if _, err := stack.providers.SetEnabled(ctx, "fake-provider", true); err != nil {
		t.Fatalf("enable fake-provider: %v", err)
	}
	if err := stack.credentials.SetValues(ctx, "fake-provider", map[string]string{"api_key": "k"}); err != nil {
		t.Fatalf("set credentials: %v", err)
	}
	// Deliberately register no Fetcher for no-creds-provider.
	succeeding := newTestFetcher(provider.Metadata{
		ID: "fake-provider",
		CredentialFields: []provider.CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	}, []provider.UsageMetric{{Name: "requests", Window: "day", Unit: "count", Used: 9}})
	stack.providers.RegisterFetcher(succeeding)

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second)

	s.tick(ctx)

	snap, err := stack.db.GetSnapshot(ctx, "fake-provider")
	if err != nil {
		t.Fatalf("expected the pollable provider's snapshot to be persisted: %v", err)
	}
	if len(snap.Metrics) != 1 || snap.Metrics[0].Used != 9 {
		t.Fatalf("unexpected snapshot: %+v", snap.Metrics)
	}
}

func TestScheduler_RunTicksAndCollectsUntilContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	stack := newTestStack(t)
	if _, err := stack.providers.SetEnabled(context.Background(), "no-creds-provider", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	f := newTestFetcher(provider.Metadata{ID: "no-creds-provider"}, []provider.UsageMetric{
		{Name: "requests", Window: "day", Unit: "count", Used: 1},
	})
	stack.providers.RegisterFetcher(f)

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, 10*time.Millisecond, time.Second)

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	waitFor(t, 2*time.Second, func() bool { return f.callCount() >= 2 })

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s of context cancellation")
	}
}

func TestScheduler_RunReturnsPromptlyOnAlreadyCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stack := newTestStack(t)
	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second)

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not return promptly for an already-cancelled context")
	}
}
