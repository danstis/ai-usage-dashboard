package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// Replace overwrites the stored snapshot for providerID with metrics
// captured at collectedAt, creating the row if it doesn't exist yet. A
// second Replace call fully discards the previous metrics — this table
// never accumulates history — and clears any previously recorded
// last_error, since a successful collection supersedes it.
func (s *sqliteStore) Replace(ctx context.Context, providerID string, metrics []store.Metric, collectedAt time.Time) error {
	if metrics == nil {
		metrics = []store.Metric{}
	}
	blob, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("sqlite: marshal metrics for %s: %w", providerID, err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO usage_snapshots (provider_id, metrics, collected_at, last_error)
		 VALUES (?, ?, ?, NULL)
		 ON CONFLICT (provider_id) DO UPDATE SET metrics = excluded.metrics, collected_at = excluded.collected_at, last_error = NULL`,
		providerID, blob, collectedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("sqlite: replace snapshot %s: %w", providerID, err)
	}
	return nil
}

// GetSnapshot returns the latest snapshot for providerID, or
// store.ErrNotFound if providerID has never been collected.
func (s *sqliteStore) GetSnapshot(ctx context.Context, providerID string) (store.Snapshot, error) {
	var blob []byte
	var collectedAt time.Time
	var lastError sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT metrics, collected_at, last_error FROM usage_snapshots WHERE provider_id = ?`, providerID,
	).Scan(&blob, &collectedAt, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Snapshot{}, store.ErrNotFound
	}
	if err != nil {
		return store.Snapshot{}, fmt.Errorf("sqlite: get snapshot %s: %w", providerID, err)
	}

	var metrics []store.Metric
	if err := json.Unmarshal(blob, &metrics); err != nil {
		return store.Snapshot{}, fmt.Errorf("sqlite: unmarshal metrics for %s: %w", providerID, err)
	}

	snap := store.Snapshot{ProviderID: providerID, Metrics: metrics, CollectedAt: collectedAt}
	if lastError.Valid {
		snap.LastError = &lastError.String
	}
	return snap, nil
}
