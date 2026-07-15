// Package plugintest provides shared test doubles for in-tree provider
// plugins under internal/plugins/<provider>/, so each plugin's FetchUsage
// test doesn't hand-roll its own provider.Service / credential.Service
// wiring. See docs/providers.md for the plugin package convention this
// harness supports.
package plugintest

import (
	"context"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/credential"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// Stack bundles a provider.Service and credential.Service backed by an
// in-memory store seeded with exactly one fixture provider (meta), enabled
// by default. A plugin test builds a Stack, registers its Fetcher via
// Stack.Providers.RegisterFetcher, resolves credentials via Stack.Reveal,
// and drives FetchUsage through the same production wiring end to end —
// no real database, no duplicated store fakes per plugin.
type Stack struct {
	Providers   *provider.Service
	Credentials *credential.Service
}

// NewStack builds a Stack whose registry contains exactly meta, already
// enabled — a plugin's FetchUsage test needs no further setup to become
// "live" once it registers a Fetcher for meta.ID.
func NewStack(meta provider.Metadata) Stack {
	repo := newMemRepo()
	svc := provider.NewService(repo, []provider.Metadata{meta})
	if err := svc.Reconcile(context.Background()); err != nil {
		panic("plugintest: reconcile: " + err.Error()) // in-memory repo never errors.
	}
	if _, err := svc.SetEnabled(context.Background(), meta.ID, true); err != nil {
		panic("plugintest: enable: " + err.Error())
	}

	key := make([]byte, 32) // deterministic all-zero key: a test double, never used in production.
	return Stack{Providers: svc, Credentials: credential.NewService(repo, key, nil)}
}

// Reveal seals values as providerID's stored credentials, then reveals and
// returns them as the map[string]string a Fetcher.FetchUsage receives at
// runtime (see internal/scheduler.Collector.loadCredentials) — building the
// creds map the same way production resolves it, rather than a plugin test
// hand-assembling one.
func (s Stack) Reveal(ctx context.Context, providerID string, values map[string]string) (map[string]string, error) {
	if err := s.Credentials.SetValues(ctx, providerID, values); err != nil {
		return nil, err
	}
	creds := make(map[string]string, len(values))
	for field := range values {
		v, err := s.Credentials.Reveal(ctx, providerID, field)
		if err != nil {
			return nil, err
		}
		creds[field] = v
	}
	return creds, nil
}

// memRepo is an in-memory store.ProviderRepository + store.CredentialRepository
// double, just enough to back a Stack without a real database.
type memRepo struct {
	providers map[string]store.Provider
	creds     map[string]map[string][]byte
}

func newMemRepo() *memRepo {
	return &memRepo{providers: map[string]store.Provider{}, creds: map[string]map[string][]byte{}}
}

func (r *memRepo) List(_ context.Context) ([]store.Provider, error) {
	out := make([]store.Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	return out, nil
}

func (r *memRepo) Get(_ context.Context, id string) (store.Provider, error) {
	p, ok := r.providers[id]
	if !ok {
		return store.Provider{}, store.ErrNotFound
	}
	return p, nil
}

func (r *memRepo) Create(_ context.Context, id string, enabled bool) (store.Provider, error) {
	now := time.Now().UTC()
	p := store.Provider{ID: id, Enabled: enabled, CreatedAt: now, UpdatedAt: now}
	r.providers[id] = p
	return p, nil
}

func (r *memRepo) SetEnabled(_ context.Context, id string, enabled bool) error {
	p, ok := r.providers[id]
	if !ok {
		return store.ErrNotFound
	}
	p.Enabled = enabled
	r.providers[id] = p
	return nil
}

func (r *memRepo) Upsert(_ context.Context, providerID, field string, ciphertext []byte) error {
	if r.creds[providerID] == nil {
		r.creds[providerID] = map[string][]byte{}
	}
	r.creds[providerID][field] = ciphertext
	return nil
}

func (r *memRepo) Presence(_ context.Context, providerID string) (map[string]bool, error) {
	presence := map[string]bool{}
	for field := range r.creds[providerID] {
		presence[field] = true
	}
	return presence, nil
}

func (r *memRepo) GetSecret(_ context.Context, providerID, field string) ([]byte, error) {
	blob, ok := r.creds[providerID][field]
	if !ok {
		return nil, store.ErrNotFound
	}
	return blob, nil
}

func (r *memRepo) Delete(_ context.Context, providerID string) error {
	delete(r.creds, providerID)
	return nil
}
