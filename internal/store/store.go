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

// ProviderStore is a ProviderRepository bound to a lifecycle-managed
// database handle. Callers must Close it when done.
type ProviderStore interface {
	ProviderRepository
	Close() error
}
