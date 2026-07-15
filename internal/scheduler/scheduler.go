package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// AuthCooldown lets other packages (internal/credential) clear a provider's
// auth-failure backoff window without importing the scheduler's internal
// registry type. A successful credential set calls Clear so the very next
// tick retries immediately instead of waiting out the remaining window.
type AuthCooldown interface {
	Clear(providerID string)
}

// cooldownEntry tracks one provider's consecutive-auth-failure count and the
// time its cooldown window expires.
type cooldownEntry struct {
	failures int
	until    time.Time
}

// AuthCooldownRegistry is an in-memory, per-provider auth-failure backoff.
// It is scheduler-owned: the gate lives in the Scheduler's tick path, never
// in Collector, so POST /providers/{id}/refresh (which calls Collector
// directly) is never gated. Failures back off exponentially from base,
// capped at max; any successful collect (or an explicit Clear, e.g. from a
// credential update) removes the entry entirely.
type AuthCooldownRegistry struct {
	mu          sync.Mutex
	base        time.Duration
	maxCooldown time.Duration
	entries     map[string]cooldownEntry
	now         func() time.Time
}

// NewAuthCooldownRegistry builds an empty registry. base is the initial
// cooldown window after a single auth failure; maxCooldown caps the
// exponential backoff for a provider that keeps failing.
func NewAuthCooldownRegistry(base, maxCooldown time.Duration) *AuthCooldownRegistry {
	return &AuthCooldownRegistry{base: base, maxCooldown: maxCooldown, entries: map[string]cooldownEntry{}, now: time.Now}
}

// active reports whether providerID is currently within its cooldown window.
func (r *AuthCooldownRegistry) active(providerID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[providerID]
	return ok && r.now().Before(e.until)
}

// recordFailure registers an auth failure for providerID, extending its
// cooldown window with exponential backoff (base, 2*base, 4*base, ...)
// capped at max.
func (r *AuthCooldownRegistry) recordFailure(providerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.entries[providerID]
	e.failures++

	backoff := r.base << (e.failures - 1) // #nosec G115 -- failures is a small bounded counter
	if e.failures > 32 || backoff <= 0 || backoff > r.maxCooldown {
		backoff = r.maxCooldown
	}
	e.until = r.now().Add(backoff)
	r.entries[providerID] = e
}

// Clear removes providerID's cooldown entry entirely, satisfying
// AuthCooldown so a successful credential set (internal/credential) can
// immediately un-gate the next tick.
func (r *AuthCooldownRegistry) Clear(providerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, providerID)
}

// Scheduler polls every enabled provider on a fixed interval, collecting
// fresh usage via a Collector. One provider's error or timeout is isolated
// to that provider: it is logged and the tick continues, it never stops the
// loop or the process. A provider that repeatedly fails authentication is
// backed off via cooldown so it isn't hammered every tick; POST
// /providers/{id}/refresh (which calls Collector directly) is never gated.
type Scheduler struct {
	providers    *provider.Service
	collector    *Collector
	pollInterval time.Duration
	fetchTimeout time.Duration
	cooldown     *AuthCooldownRegistry
}

// New builds a Scheduler that lists enabled providers via providers, polls
// every pollInterval, and bounds each provider's fetch to fetchTimeout.
// cooldown gates the tick path on repeated auth failures; callers typically
// construct it once with NewAuthCooldownRegistry(pollInterval, time.Hour)
// and share it with internal/credential so a credential update clears it.
func New(providers *provider.Service, collector *Collector, pollInterval, fetchTimeout time.Duration, cooldown *AuthCooldownRegistry) *Scheduler {
	return &Scheduler{
		providers:    providers,
		collector:    collector,
		pollInterval: pollInterval,
		fetchTimeout: fetchTimeout,
		cooldown:     cooldown,
	}
}

// Run blocks, ticking every pollInterval and collecting every enabled
// provider on each tick, until ctx is cancelled. It returns promptly on
// cancellation — no goroutine leak, no in-flight tick blocks shutdown
// beyond its own per-provider fetchTimeout.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick collects every enabled provider once. A provider whose fetch fails
// or times out logs and is skipped; it never aborts the remaining
// providers in this tick.
func (s *Scheduler) tick(ctx context.Context) {
	providers, err := s.providers.List(ctx)
	if err != nil {
		slog.Error("scheduler: list providers", "error", err)
		return
	}

	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		s.collectOne(ctx, p.ID)
	}
}

// collectOne runs one provider's collection under a per-provider timeout
// derived from ctx, logging (and swallowing) any failure so it never
// propagates to the caller. A provider currently within its auth-failure
// cooldown window is skipped without attempting the upstream call; a fresh
// auth failure engages the cooldown, any other error leaves it untouched
// (so a transient 5xx keeps retrying every tick), and a success clears it.
func (s *Scheduler) collectOne(ctx context.Context, providerID string) {
	if s.cooldown != nil && s.cooldown.active(providerID) {
		slog.Warn("scheduler: provider is in auth-failure cooldown, skipping", "provider", providerID)
		return
	}

	fetchCtx, cancel := context.WithTimeout(ctx, s.fetchTimeout)
	defer cancel()

	_, err := s.collector.Collect(fetchCtx, providerID)
	if err != nil {
		if errors.Is(err, provider.ErrFetcherNotFound) {
			slog.Warn("scheduler: provider has no registered fetcher, skipping", "provider", providerID)
			return
		}
		if errors.Is(err, provider.ErrAuth) {
			if s.cooldown != nil {
				s.cooldown.recordFailure(providerID)
			}
			slog.Warn("scheduler: provider authentication failed, engaging cooldown", "provider", providerID, "error", err)
			return
		}
		slog.Error("scheduler: collect provider usage failed", "provider", providerID, "error", err)
		return
	}

	if s.cooldown != nil {
		s.cooldown.Clear(providerID)
	}
}
