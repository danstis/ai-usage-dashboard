package api

import (
	"context"
	"errors"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// NewSnapshotRepository adapts a *provider.Service (provider-id validation)
// and a store.SnapshotRepository (persisted snapshots) to the
// SnapshotRepository seam the usage handler depends on.
//
// Architect decision 4 (BSOD-61): GetUsage returns 200 with a pending
// snapshot (CollectedAt nil, no metrics) for a known provider that has never
// been collected, translating store.ErrNotFound into that pending value
// rather than propagating it as a 404. Only an unknown provider id (checked
// via providers.Get first) surfaces as store.ErrNotFound to the handler,
// which the handler maps to 404.
func NewSnapshotRepository(providers *provider.Service, snapshots store.SnapshotRepository) SnapshotRepository {
	return snapshotServiceAdapter{providers: providers, snapshots: snapshots}
}

type snapshotServiceAdapter struct {
	providers *provider.Service
	snapshots store.SnapshotRepository
}

func (a snapshotServiceAdapter) GetUsage(ctx context.Context, providerID string) (UsageSnapshot, error) {
	if _, err := a.providers.Get(ctx, providerID); err != nil {
		return UsageSnapshot{}, err
	}

	snap, err := a.snapshots.GetSnapshot(ctx, providerID)
	if errors.Is(err, store.ErrNotFound) {
		return UsageSnapshot{Metrics: []UsageMetric{}}, nil
	}
	if err != nil {
		return UsageSnapshot{}, err
	}
	return toAPIUsageSnapshot(snap), nil
}

func toAPIUsageSnapshot(snap store.Snapshot) UsageSnapshot {
	collectedAt := snap.CollectedAt
	metrics := make([]UsageMetric, 0, len(snap.Metrics))
	for _, m := range snap.Metrics {
		metrics = append(metrics, UsageMetric{
			Name:      m.Name,
			Window:    m.Window,
			Unit:      m.Unit,
			Used:      m.Used,
			Limit:     m.Limit,
			Remaining: m.Remaining,
			ResetAt:   m.ResetAt,
		})
	}
	return UsageSnapshot{
		CollectedAt: &collectedAt,
		Metrics:     metrics,
		LastError:   snap.LastError,
	}
}
