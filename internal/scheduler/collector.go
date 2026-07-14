// Package scheduler implements the P2/S5 integration slice: a Collector
// that performs one provider's credential-load-fetch-persist cycle, and a
// Scheduler that drives Collector on a timer for every enabled provider.
// The on-demand refresh HTTP endpoint (internal/api) and the background
// ticker both call the same Collector so the two paths can never diverge.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/credential"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// ErrProviderDisabled is returned by Collector.Collect when providerID is a
// known provider that is not currently enabled.
var ErrProviderDisabled = errors.New("scheduler: provider is disabled")

// ErrProviderUncredentialed is returned by Collector.Collect when
// providerID is enabled but is missing a stored value for one or more of
// its declared credential fields.
var ErrProviderUncredentialed = errors.New("scheduler: provider is missing required credentials")

// Collector performs the fetch-and-persist cycle for a single provider:
// resolve its enabled state and credentials, call its registered Fetcher,
// and write the result as the new latest snapshot. It is the shared core
// both the background Scheduler and the on-demand refresh HTTP endpoint
// call, so their behavior (validation, error semantics) can never diverge.
type Collector struct {
	providers   *provider.Service
	credentials *credential.Service
	snapshots   store.SnapshotRepository
	now         func() time.Time
}

// NewCollector builds a Collector backed by providers (enabled state +
// Fetcher dispatch), credentials (decrypting stored values), and snapshots
// (persisting the result).
func NewCollector(providers *provider.Service, credentials *credential.Service, snapshots store.SnapshotRepository) *Collector {
	return &Collector{providers: providers, credentials: credentials, snapshots: snapshots, now: time.Now}
}

// Collect fetches fresh usage for providerID and persists it as the new
// latest snapshot, returning the resulting snapshot.
//
// It returns store.ErrNotFound if providerID is not a known provider,
// ErrProviderDisabled if the provider is known but not enabled,
// ErrProviderUncredentialed if the provider is enabled but missing a stored
// value for one or more declared credential fields, or the error from the
// registered Fetcher (including provider.ErrFetcherNotFound if no Fetcher is
// registered for a known, enabled, credentialed provider) unwrapped from
// provider.Service.FetchUsage.
func (c *Collector) Collect(ctx context.Context, providerID string) (store.Snapshot, error) {
	p, err := c.providers.Get(ctx, providerID)
	if err != nil {
		return store.Snapshot{}, err
	}
	if !p.Enabled {
		return store.Snapshot{}, ErrProviderDisabled
	}

	creds, err := c.loadCredentials(ctx, p)
	if err != nil {
		return store.Snapshot{}, err
	}

	metrics, err := c.providers.FetchUsage(ctx, providerID, creds)
	if err != nil {
		return store.Snapshot{}, err
	}

	collectedAt := c.now().UTC()
	storeMetrics := toStoreMetrics(metrics)
	if err := c.snapshots.Replace(ctx, providerID, storeMetrics, collectedAt); err != nil {
		return store.Snapshot{}, fmt.Errorf("scheduler: replace snapshot %s: %w", providerID, err)
	}

	return store.Snapshot{ProviderID: providerID, Metrics: storeMetrics, CollectedAt: collectedAt}, nil
}

// loadCredentials decrypts every credential field p declares. A provider
// with no declared fields needs no credentials and resolves to an empty
// map. Any missing field is reported as ErrProviderUncredentialed — a
// partially-credentialed provider is treated the same as an uncredentialed
// one, since FetchUsage would receive an incomplete credential map either
// way.
func (c *Collector) loadCredentials(ctx context.Context, p provider.Provider) (map[string]string, error) {
	creds := make(map[string]string, len(p.CredentialFields))
	for _, f := range p.CredentialFields {
		v, err := c.credentials.Reveal(ctx, p.ID, f.Name)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, ErrProviderUncredentialed
			}
			return nil, fmt.Errorf("scheduler: load credential %s/%s: %w", p.ID, f.Name, err)
		}
		creds[f.Name] = v
	}
	return creds, nil
}

// toStoreMetrics converts the provider package's runtime metric shape to
// the persistence-layer store.Metric shape. The two are declared
// independently (see store.Metric's doc comment) to avoid an import cycle,
// so every caller crossing the boundary needs this conversion.
func toStoreMetrics(metrics []provider.UsageMetric) []store.Metric {
	out := make([]store.Metric, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, store.Metric{
			Name:      m.Name,
			Window:    m.Window,
			Unit:      m.Unit,
			Used:      m.Used,
			Limit:     m.Limit,
			Remaining: m.Remaining,
			ResetAt:   m.ResetAt,
		})
	}
	return out
}
