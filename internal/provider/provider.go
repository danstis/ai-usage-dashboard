// Package provider defines the compiled-in provider registry (Model A: a
// static, build-time list of known providers) and the Service that merges
// that registry with the persisted enabled/disabled state owned by
// internal/store.
package provider

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// CredentialField describes one credential field a provider requires (e.g.
// an API key). FetchUsage itself is out of scope for P1 — this is metadata
// only, used to render a credential form in a later phase.
type CredentialField struct {
	Name   string
	Label  string
	Secret bool
}

// Metadata is the static, compiled-in description of a provider: what it's
// called and what credentials configuring it requires. It carries no
// runtime state — enabled/disabled is persisted separately in the store.
type Metadata struct {
	ID               string
	Name             string
	CredentialFields []CredentialField
}

// Provider is registry Metadata merged with its persisted enabled state —
// the shape Service returns to callers.
type Provider struct {
	Metadata
	Enabled bool
}

// Registry is the compiled-in, ordered list of providers this build knows
// about. Growing the catalog means adding an entry here and shipping a new
// build (Model A) — there is no dynamic/plugin registration in P1.
var Registry = []Metadata{
	{
		ID:   "openai",
		Name: "OpenAI",
		CredentialFields: []CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	},
	{
		ID:   "anthropic",
		Name: "Anthropic",
		CredentialFields: []CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	},
}

// Service merges a compiled-in provider registry with persisted enabled
// state from a store.ProviderRepository. It is the single place that knows
// how to reconcile the two and answer provider queries.
type Service struct {
	registry []Metadata
	repo     store.ProviderRepository
}

// NewService builds a Service backed by repo, serving the given registry.
// Production callers pass Registry; tests pass a small fixture registry.
func NewService(repo store.ProviderRepository, registry []Metadata) *Service {
	return &Service{repo: repo, registry: registry}
}

func (s *Service) metadata(id string) (Metadata, bool) {
	for _, m := range s.registry {
		if m.ID == id {
			return m, true
		}
	}
	return Metadata{}, false
}

// List returns every registered provider merged with its persisted enabled
// state, ordered as the registry declares them.
func (s *Service) List(ctx context.Context) ([]Provider, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("provider: list: %w", err)
	}
	enabled := make(map[string]bool, len(rows))
	for _, row := range rows {
		enabled[row.ID] = row.Enabled
	}

	providers := make([]Provider, 0, len(s.registry))
	for _, m := range s.registry {
		providers = append(providers, Provider{Metadata: m, Enabled: enabled[m.ID]})
	}
	return providers, nil
}

// Get returns the single provider identified by id. It returns
// store.ErrNotFound if id is not in the registry, or if the registry knows
// about id but no persisted row exists for it yet.
func (s *Service) Get(ctx context.Context, id string) (Provider, error) {
	m, ok := s.metadata(id)
	if !ok {
		return Provider{}, store.ErrNotFound
	}
	row, err := s.repo.Get(ctx, id)
	if err != nil {
		return Provider{}, err
	}
	return Provider{Metadata: m, Enabled: row.Enabled}, nil
}

// SetEnabled sets the persisted enabled state for id and returns the
// resulting provider. It is idempotent: setting an already-matching state
// succeeds and returns the current provider. It returns store.ErrNotFound if
// id is not in the registry, or if the registry knows about id but no
// persisted row exists for it yet.
func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) (Provider, error) {
	m, ok := s.metadata(id)
	if !ok {
		return Provider{}, store.ErrNotFound
	}
	if err := s.repo.SetEnabled(ctx, id, enabled); err != nil {
		return Provider{}, err
	}
	return Provider{Metadata: m, Enabled: enabled}, nil
}

// Reconcile ensures every provider in the registry has a persisted row,
// creating one (default disabled) for any that are missing. It never
// deletes or otherwise touches rows whose id is no longer present in the
// registry — a provider dropped from a future build must not silently
// destroy a user's persisted state — it only logs them so the gap is
// visible.
func (s *Service) Reconcile(ctx context.Context) error {
	existing, err := s.repo.List(ctx)
	if err != nil {
		return fmt.Errorf("provider: reconcile: list existing: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, row := range existing {
		have[row.ID] = true
	}

	known := make(map[string]bool, len(s.registry))
	for _, m := range s.registry {
		known[m.ID] = true
		if have[m.ID] {
			continue
		}
		if _, err := s.repo.Create(ctx, m.ID, false); err != nil {
			return fmt.Errorf("provider: reconcile: create %s: %w", m.ID, err)
		}
	}

	for _, row := range existing {
		if !known[row.ID] {
			slog.Warn("provider: persisted row has no matching registry entry, leaving it in place", "id", row.ID)
		}
	}

	return nil
}
