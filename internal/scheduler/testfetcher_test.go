package scheduler

import (
	"context"
	"sync"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// testFetcher is a minimal, deterministic provider.Fetcher test double.
// scheduler is a separate package from internal/provider so it cannot reuse
// provider's own unexported fake fetcher — it only needs the exported
// Fetcher contract, so this local double is enough.
type testFetcher struct {
	meta    provider.Metadata
	mu      sync.Mutex
	metrics []provider.UsageMetric
	err     error
	calls   int
	creds   map[string]string
}

func newTestFetcher(meta provider.Metadata, metrics []provider.UsageMetric) *testFetcher {
	return &testFetcher{meta: meta, metrics: metrics}
}

func (f *testFetcher) Metadata() provider.Metadata { return f.meta }

func (f *testFetcher) FetchUsage(_ context.Context, creds map[string]string) ([]provider.UsageMetric, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.creds = creds
	if f.err != nil {
		return nil, f.err
	}
	out := make([]provider.UsageMetric, len(f.metrics))
	copy(out, f.metrics)
	return out, nil
}

func (f *testFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
