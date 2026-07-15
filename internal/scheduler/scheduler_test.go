package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/providertest"
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

	enabled := providertest.NewFetcher(provider.Metadata{ID: "no-creds-provider"}, []provider.UsageMetric{
		{Name: "requests", Window: "day", Unit: "count", Used: 1},
	})
	disabled := providertest.NewFetcher(provider.Metadata{ID: "fake-provider"}, nil)
	stack.providers.RegisterFetcher(enabled)
	stack.providers.RegisterFetcher(disabled)

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second, NewAuthCooldownRegistry(time.Hour, time.Hour))

	s.tick(ctx)

	if enabled.CallCount() != 1 {
		t.Errorf("expected enabled provider to be collected once, got %d calls", enabled.CallCount())
	}
	if disabled.CallCount() != 0 {
		t.Errorf("expected disabled provider to never be collected, got %d calls", disabled.CallCount())
	}
}

func TestScheduler_FailingProviderDoesNotStopOtherCollections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	failing := providertest.NewFetcher(provider.Metadata{ID: "no-creds-provider"}, nil)
	failing.SetError(errors.New("upstream timeout"))
	succeeding := providertest.NewFetcher(provider.Metadata{
		ID: "fake-provider",
		CredentialFields: []provider.CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	}, []provider.UsageMetric{{Name: "requests", Window: "day", Unit: "count", Used: 5}})
	setupTwoProviderScenario(t, stack, failing, succeeding)

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second, NewAuthCooldownRegistry(time.Hour, time.Hour))

	s.tick(ctx)

	if failing.CallCount() != 1 {
		t.Errorf("expected failing provider to still be attempted once, got %d", failing.CallCount())
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
	// Deliberately register no Fetcher for no-creds-provider.
	succeeding := providertest.NewFetcher(provider.Metadata{
		ID: "fake-provider",
		CredentialFields: []provider.CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	}, []provider.UsageMetric{{Name: "requests", Window: "day", Unit: "count", Used: 9}})
	setupTwoProviderScenario(t, stack, nil, succeeding)

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second, NewAuthCooldownRegistry(time.Hour, time.Hour))

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
	f := providertest.NewFetcher(provider.Metadata{ID: "no-creds-provider"}, []provider.UsageMetric{
		{Name: "requests", Window: "day", Unit: "count", Used: 1},
	})
	stack.providers.RegisterFetcher(f)

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, 10*time.Millisecond, time.Second, NewAuthCooldownRegistry(10*time.Millisecond, time.Hour))

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	waitFor(t, 2*time.Second, func() bool { return f.CallCount() >= 2 })

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s of context cancellation")
	}
}

func TestScheduler_AuthCooldown_SkipsUntilWindowElapses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	fetcher := setupNoCredsProviderFetcher(t, stack, nil)
	fetcher.SetError(fmt.Errorf("bad key: %w", provider.ErrAuth))

	cooldown := NewAuthCooldownRegistry(time.Minute, time.Hour)
	now := time.Now()
	cooldown.now = func() time.Time { return now }

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second, cooldown)

	s.tick(ctx)
	if fetcher.CallCount() != 1 {
		t.Fatalf("expected the first tick to attempt the upstream call and hit the auth failure, got %d calls", fetcher.CallCount())
	}

	s.tick(ctx)
	if fetcher.CallCount() != 1 {
		t.Fatalf("expected the second tick (still within the cooldown window) to skip the upstream call, got %d calls", fetcher.CallCount())
	}

	now = now.Add(2 * time.Minute)
	s.tick(ctx)
	if fetcher.CallCount() != 2 {
		t.Fatalf("expected the tick after the cooldown window elapsed to attempt again, got %d calls", fetcher.CallCount())
	}
}

func TestScheduler_AuthCooldown_ClearsOnSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	fetcher := setupNoCredsProviderFetcher(t, stack, []provider.UsageMetric{{Name: "requests", Window: "day", Unit: "count", Used: 1}})
	fetcher.SetError(fmt.Errorf("bad key: %w", provider.ErrAuth))

	cooldown := NewAuthCooldownRegistry(time.Minute, time.Hour)
	now := time.Now()
	cooldown.now = func() time.Time { return now }

	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second, cooldown)

	s.tick(ctx)
	if !cooldown.active("no-creds-provider") {
		t.Fatal("expected cooldown to be active after an auth failure")
	}

	now = now.Add(2 * time.Minute)
	fetcher.SetError(nil)
	s.tick(ctx)

	if cooldown.active("no-creds-provider") {
		t.Error("expected a successful collect to clear the cooldown")
	}
	cooldown.mu.Lock()
	_, exists := cooldown.entries["no-creds-provider"]
	cooldown.mu.Unlock()
	if exists {
		t.Error("expected a successful collect to remove the cooldown entry entirely, not just expire it")
	}
}

func TestScheduler_AuthCooldown_NonAuthErrorsDoNotCooldown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	fetcher := setupNoCredsProviderFetcher(t, stack, nil)
	fetcher.SetError(errors.New("upstream 500"))

	cooldown := NewAuthCooldownRegistry(time.Minute, time.Hour)
	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second, cooldown)

	s.tick(ctx)
	s.tick(ctx)

	if cooldown.active("no-creds-provider") {
		t.Error("expected a non-auth error to leave the cooldown entry absent")
	}
	if fetcher.CallCount() != 2 {
		t.Errorf("expected both ticks to attempt the upstream call (no cooldown gating on non-auth errors), got %d", fetcher.CallCount())
	}
}

func TestCollector_RefreshBypassesCooldown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := newTestStack(t)
	fetcher := setupNoCredsProviderFetcher(t, stack, []provider.UsageMetric{{Name: "requests", Window: "day", Unit: "count", Used: 1}})

	cooldown := NewAuthCooldownRegistry(time.Hour, time.Hour)
	cooldown.recordFailure("no-creds-provider")
	if !cooldown.active("no-creds-provider") {
		t.Fatal("expected cooldown to be active")
	}

	// The on-demand refresh endpoint calls Collector.Collect directly (see
	// internal/api/refresh_adapter.go) — it never consults the scheduler's
	// cooldown registry, so a provider mid-cooldown still gets attempted.
	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	if _, err := collector.Collect(ctx, "no-creds-provider"); err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if fetcher.CallCount() != 1 {
		t.Fatalf("expected the refresh path to attempt the upstream call despite an active cooldown, got %d calls", fetcher.CallCount())
	}
}

func TestScheduler_RunReturnsPromptlyOnAlreadyCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stack := newTestStack(t)
	collector := NewCollector(stack.providers, stack.credentials, stack.db)
	s := New(stack.providers, collector, time.Hour, time.Second, NewAuthCooldownRegistry(time.Hour, time.Hour))

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
