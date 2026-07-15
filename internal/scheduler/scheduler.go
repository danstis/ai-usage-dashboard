package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// Scheduler polls every enabled provider on a fixed interval, collecting
// fresh usage via a Collector. One provider's error or timeout is isolated
// to that provider: it is logged and the tick continues, it never stops the
// loop or the process.
type Scheduler struct {
	providers    *provider.Service
	collector    *Collector
	pollInterval time.Duration
	fetchTimeout time.Duration
}

// New builds a Scheduler that lists enabled providers via providers, polls
// every pollInterval, and bounds each provider's fetch to fetchTimeout.
func New(providers *provider.Service, collector *Collector, pollInterval, fetchTimeout time.Duration) *Scheduler {
	return &Scheduler{
		providers:    providers,
		collector:    collector,
		pollInterval: pollInterval,
		fetchTimeout: fetchTimeout,
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
// propagates to the caller.
func (s *Scheduler) collectOne(ctx context.Context, providerID string) {
	fetchCtx, cancel := context.WithTimeout(ctx, s.fetchTimeout)
	defer cancel()

	if _, err := s.collector.Collect(fetchCtx, providerID); err != nil {
		if errors.Is(err, provider.ErrFetcherNotFound) {
			slog.Warn("scheduler: provider has no registered fetcher, skipping", "provider", providerID)
			return
		}
		slog.Error("scheduler: collect provider usage failed", "provider", providerID, "error", err)
	}
}
