// Package providertest contains shared provider test doubles importable across internal packages.
package providertest

import (
	"context"
	"sync"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// Fetcher is a deterministic provider.Fetcher test double shared across packages.
type Fetcher struct {
	meta    provider.Metadata
	mu      sync.Mutex
	metrics []provider.UsageMetric
	err     error
	calls   int
	creds   map[string]string
}

// NewFetcher returns a Fetcher seeded with metadata and deterministic metrics.
func NewFetcher(meta provider.Metadata, metrics []provider.UsageMetric) *Fetcher {
	return &Fetcher{meta: meta, metrics: cloneMetrics(metrics)}
}

// Metadata satisfies provider.Fetcher.
func (f *Fetcher) Metadata() provider.Metadata { return f.meta }

// FetchUsage satisfies provider.Fetcher and records the call inputs.
func (f *Fetcher) FetchUsage(_ context.Context, creds map[string]string) ([]provider.UsageMetric, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.creds = cloneCreds(creds)
	if f.err != nil {
		return nil, f.err
	}
	return cloneMetrics(f.metrics), nil
}

// SetError configures FetchUsage to return err on future calls.
func (f *Fetcher) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// SetMetrics replaces the metrics returned by future FetchUsage calls.
func (f *Fetcher) SetMetrics(metrics []provider.UsageMetric) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.metrics = cloneMetrics(metrics)
}

// CallCount returns how many times FetchUsage has been invoked.
func (f *Fetcher) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// LastCreds returns a copy of the most recent credential map FetchUsage received.
func (f *Fetcher) LastCreds() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneCreds(f.creds)
}

func cloneMetrics(metrics []provider.UsageMetric) []provider.UsageMetric {
	out := make([]provider.UsageMetric, len(metrics))
	copy(out, metrics)
	return out
}

func cloneCreds(creds map[string]string) map[string]string {
	if creds == nil {
		return nil
	}
	out := make(map[string]string, len(creds))
	for k, v := range creds {
		out[k] = v
	}
	return out
}
