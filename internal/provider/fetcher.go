// Package provider — runtime Fetcher contract (P2/S1).
//
// This file defines the executable side of the provider catalog: a transport-
// neutral Fetcher interface, the UsageMetric shape it returns, and a
// parallel runtime registry keyed by metadata id. The metadata-only
// Registry (in provider.go) remains the source of truth for the P1 API
// (List/Get/SetEnabled/Reconcile). A metadata provider without a registered
// Fetcher is intentionally not pollable — S5 (scheduler) skips it and the
// future POST /refresh endpoint returns conflict.
//
// Transport neutrality (Architect decision 5 on BSOD-61): the boundary
// passes only context.Context, map[string]string credentials, and
// []UsageMetric of scalar fields. No channels, funcs, or interfaces cross
// the boundary so a future hashicorp/go-plugin gRPC provider can satisfy
// this same contract without a breaking change.
package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrFetcherNotFound is returned when a caller asks Service.FetchUsage for
// a provider that has no registered Fetcher — either because the metadata
// id is unknown to the registry, or because the metadata is known but no
// Fetcher has been registered for it (a metadata-only provider).
var ErrFetcherNotFound = errors.New("provider: no Fetcher registered")

// UsageMetric is one usage data point returned by a Fetcher.
//
// All required fields are non-empty strings or non-negative integers; no
// floats are used so the type is deterministic and gRPC-friendly. Fractional
// quantities (e.g. spend) are expressed in the smallest integer unit named
// by Unit — "usd_cents", "tokens", etc.
//
// Limit, Remaining, and ResetAt are optional. Limit == nil means the
// provider does not disclose a cap (unlimited or unknown). Remaining is
// provider-supplied; callers MAY derive Limit-Used when Remaining is nil.
// ResetAt == nil means the provider did not return a reset time.
type UsageMetric struct {
	Name      string     // required, non-empty
	Window    string     // required, non-empty (e.g. "month", "day")
	Unit      string     // required, non-empty (smallest integer unit, e.g. "usd_cents")
	Used      int64      // required
	Limit     *int64     // nil = unlimited / unknown
	Remaining *int64     // nil = unknown; provider-supplied
	ResetAt   *time.Time // nil = none
}

// Fetcher is the runtime contract every executable provider satisfies.
//
// It is transport-neutral by design (Architect decision 5): the only types
// crossing the boundary are context.Context, map[string]string credentials,
// and []UsageMetric of scalar fields. An in-tree compiled-in provider and a
// future hashicorp/go-plugin gRPC provider satisfy the same interface.
//
// Metadata() must return the matching Metadata from the static registry.
// FetchUsage must be safe to call concurrently for different provider ids;
// implementations are responsible for any per-provider synchronization.
type Fetcher interface {
	Metadata() Metadata
	FetchUsage(ctx context.Context, creds map[string]string) ([]UsageMetric, error)
}

// runtimeRegistry holds the set of Fetchers keyed by Metadata.ID. It is
// intentionally separate from the metadata Registry so adding a runtime
// fetcher never requires touching the static catalog — and so a metadata
// entry without a fetcher is observable as "not pollable" rather than as a
// silent missing-method error.
type runtimeRegistry struct {
	mu    sync.RWMutex
	fetch map[string]Fetcher
}

// newRuntimeRegistry returns an empty runtime registry. Tests and main
// each construct their own so registration in tests cannot leak into
// production and vice versa.
func newRuntimeRegistry() *runtimeRegistry {
	return &runtimeRegistry{fetch: map[string]Fetcher{}}
}

// register adds fetcher to the registry under its Metadata().ID. It panics
// if id is already registered — duplicate registration is a programmer
// error (two Fetchers claiming the same provider id) that must fail loud
// at startup, not silently shadow an existing fetcher at runtime.
func (r *runtimeRegistry) register(id string, fetcher Fetcher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.fetch[id]; exists {
		panic(fmt.Sprintf("provider: Fetcher already registered for id %q", id))
	}
	r.fetch[id] = fetcher
}

// lookup returns the Fetcher registered under id, or ErrFetcherNotFound.
// A nil fetcher in the map is treated as "not registered" defensively even
// though register never inserts nil.
func (r *runtimeRegistry) lookup(id string) (Fetcher, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.fetch[id]
	if !ok || f == nil {
		return nil, ErrFetcherNotFound
	}
	return f, nil
}

// RegisterFetcher adds f to the Service's runtime registry, keyed by
// f.Metadata().ID. It panics on duplicate registration so misconfiguration
// is caught at boot rather than silently shadowing an existing fetcher. It
// also panics if f.Metadata().ID is not present in the Service's metadata
// registry: without this check a mistyped id would register successfully
// but FetchUsage gates on s.metadata(id) first, so the fetcher would be
// silently unreachable — the same failure class Reconcile already guards
// against for the opposite direction (registry entries with no store row).
func (s *Service) RegisterFetcher(f Fetcher) {
	id := f.Metadata().ID
	if _, ok := s.metadata(id); !ok {
		panic(fmt.Sprintf("provider: RegisterFetcher: id %q is not present in the metadata registry", id))
	}
	s.fetchers.register(id, f)
}

// FetchUsage invokes the registered Fetcher for id with the given
// credentials. It returns ErrFetcherNotFound if id is not in the metadata
// registry (mirrors Get/SetEnabled semantics) or if no Fetcher has been
// registered for a known metadata id — both are "not pollable" and must be
// treated identically by callers (scheduler skip / refresh conflict).
//
// Any error returned by the Fetcher is wrapped and propagated unchanged
// (errors.Is/As work).
func (s *Service) FetchUsage(ctx context.Context, id string, creds map[string]string) ([]UsageMetric, error) {
	if _, ok := s.metadata(id); !ok {
		return nil, ErrFetcherNotFound
	}
	f, err := s.fetchers.lookup(id)
	if err != nil {
		return nil, err
	}
	metrics, err := f.FetchUsage(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("provider: fetch %s: %w", id, err)
	}
	return metrics, nil
}
