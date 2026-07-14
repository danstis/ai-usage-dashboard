// Package store defines the persistence-layer interfaces used by the AI
// Usage Dashboard. Concrete implementations (e.g. internal/store/sqlite)
// satisfy these interfaces so the backing database engine can be swapped
// later without changing callers.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a lookup by id matches no row.
var ErrNotFound = errors.New("store: not found")

// Provider is a persisted provider-registry row.
type Provider struct {
	ID        string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProviderRepository manages persisted provider-registry state: whether a
// known provider is enabled.
type ProviderRepository interface {
	// List returns every provider row, ordered by id.
	List(ctx context.Context) ([]Provider, error)
	// Get returns the provider row for id, or ErrNotFound if no such row
	// exists.
	Get(ctx context.Context, id string) (Provider, error)
	// Create inserts a new provider row with the given initial enabled
	// state.
	Create(ctx context.Context, id string, enabled bool) (Provider, error)
	// SetEnabled updates the enabled state for id. It returns ErrNotFound
	// if no such row exists.
	SetEnabled(ctx context.Context, id string, enabled bool) error
}

// CredentialRepository manages persisted, encrypted credential field
// values. It never exposes a decryption path itself: GetSecret returns the
// raw ciphertext blob exactly as stored, and only
// internal/credential.Service.Reveal (consumed by the future fetch/
// scheduler path, P2/S5) decrypts it via internal/secret. No HTTP read path
// may call GetSecret.
type CredentialRepository interface {
	// Upsert stores ciphertext for (providerID, field), creating the row if
	// it doesn't exist yet or replacing the value if it does.
	Upsert(ctx context.Context, providerID, field string, ciphertext []byte) error
	// Presence returns which fields currently have a stored value for
	// providerID, keyed by field name. A field absent from the map has no
	// stored value.
	Presence(ctx context.Context, providerID string) (map[string]bool, error)
	// GetSecret returns the raw ciphertext blob stored for (providerID,
	// field), or ErrNotFound if no value is stored for that field.
	GetSecret(ctx context.Context, providerID, field string) ([]byte, error)
	// Delete clears every stored value for providerID. It is idempotent —
	// deleting a provider with no stored credentials succeeds.
	Delete(ctx context.Context, providerID string) error
}

// Metric is one persisted usage data point captured in a Snapshot. It
// mirrors provider.UsageMetric's shape field-for-field, but is declared here
// independently rather than imported: internal/provider already imports
// internal/store (for store.ProviderRepository), so store depending on
// provider back would be an import cycle. Adapters at the internal/api and
// future scheduler (P2/S5) layers convert between the two.
type Metric struct {
	Name      string
	Window    string
	Unit      string
	Used      int64
	Limit     *int64     // nil = unlimited / unknown
	Remaining *int64     // nil = unknown
	ResetAt   *time.Time // nil = none
}

// Snapshot is the latest-only usage state persisted for one provider: the
// metrics captured at CollectedAt, plus the error from the most recent
// failed collection attempt, if any (set by the future scheduler, P2/S5;
// always nil for a snapshot written by Replace).
type Snapshot struct {
	ProviderID  string
	Metrics     []Metric
	CollectedAt time.Time
	LastError   *string
}

// SnapshotRepository manages the latest-only usage snapshot per provider —
// no history/time-series retention. Each provider has at most one row.
type SnapshotRepository interface {
	// Replace overwrites the stored snapshot for providerID with metrics
	// captured at collectedAt, creating the row if it doesn't exist yet.
	// There is no accumulation: a second Replace call discards the
	// previous metrics entirely.
	Replace(ctx context.Context, providerID string, metrics []Metric, collectedAt time.Time) error
	// GetSnapshot returns the latest snapshot for providerID, or ErrNotFound
	// if providerID has never been collected. Named GetSnapshot rather than
	// Get because Store also embeds ProviderRepository.Get, which has a
	// different signature — Go forbids embedding two interfaces whose
	// method sets share a name with different signatures.
	GetSnapshot(ctx context.Context, providerID string) (Snapshot, error)
}

// Store is a lifecycle-managed database handle providing every repository
// this service persists to. Callers must Close it when done.
type Store interface {
	ProviderRepository
	CredentialRepository
	SnapshotRepository
	Close() error
}
