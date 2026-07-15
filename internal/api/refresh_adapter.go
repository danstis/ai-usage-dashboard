package api

import (
	"context"

	"github.com/danstis/ai-usage-dashboard/internal/scheduler"
)

// NewUsageRefresher adapts a *scheduler.Collector to the UsageRefresher
// seam the refresh handler depends on.
func NewUsageRefresher(collector *scheduler.Collector) UsageRefresher {
	return usageRefresherAdapter{collector: collector}
}

type usageRefresherAdapter struct {
	collector *scheduler.Collector
}

func (a usageRefresherAdapter) RefreshUsage(ctx context.Context, providerID string) (UsageSnapshot, error) {
	snap, err := a.collector.Collect(ctx, providerID)
	if err != nil {
		return UsageSnapshot{}, err
	}
	return toAPIUsageSnapshot(snap), nil
}
