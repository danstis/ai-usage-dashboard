package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/credential"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/scheduler"
	"github.com/danstis/ai-usage-dashboard/internal/store/sqlite"
)

// refreshTestFetcher is a minimal, deterministic provider.Fetcher test
// double for exercising the refresh HTTP endpoint end to end.
type refreshTestFetcher struct {
	meta    provider.Metadata
	mu      sync.Mutex
	metrics []provider.UsageMetric
	err     error
	calls   int
}

func (f *refreshTestFetcher) Metadata() provider.Metadata { return f.meta }

func (f *refreshTestFetcher) FetchUsage(_ context.Context, _ map[string]string) ([]provider.UsageMetric, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]provider.UsageMetric, len(f.metrics))
	copy(out, f.metrics)
	return out, nil
}

// refreshTestRegistry declares credential fields so the disabled/
// uncredentialed conflict paths have something to exercise.
var refreshTestRegistry = []provider.Metadata{
	{
		ID:   "openai",
		Name: "OpenAI",
		CredentialFields: []provider.CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	},
	{ID: "anthropic", Name: "Anthropic"},
}

// newRefreshTestHandler builds a handler backed by a real sqlite store and
// exposes providerSvc so tests can register a Fetcher directly, unlike
// newUsageTestHandler which has no refresh wiring.
func newRefreshTestHandler(t *testing.T) (http.Handler, *provider.Service, *credential.Service) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "aud.db")
	db, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})

	providerSvc := provider.NewService(db, refreshTestRegistry)
	if err := providerSvc.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	key := make([]byte, 32)
	credentialSvc := credential.NewService(db, key)
	collector := scheduler.NewCollector(providerSvc, credentialSvc, db)

	handler := NewHandler(
		NewProviderRepository(providerSvc),
		NewCredentialRepository(providerSvc, credentialSvc),
		NewUsageGetter(providerSvc, db),
		NewUsageRefresher(collector),
	)
	return handler, providerSvc, credentialSvc
}

func refreshUsage(t *testing.T, handler http.Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/providers/"+id+"/refresh", nil))
	return rec
}

// TestRefresh_FullAcceptanceLoop is the P2 acceptance path: register the
// fake fetcher, credential it via PUT .../credentials, enable it, trigger
// POST .../refresh, then GET .../usage and assert the fake's metrics come
// back through the full HTTP surface.
func TestRefresh_FullAcceptanceLoop(t *testing.T) {
	t.Parallel()

	handler, providerSvc, _ := newRefreshTestHandler(t)

	limit := int64(500)
	fetcher := &refreshTestFetcher{
		meta: provider.Metadata{
			ID:   "openai",
			Name: "OpenAI",
			CredentialFields: []provider.CredentialField{
				{Name: "api_key", Label: "API Key", Secret: true},
			},
		},
		metrics: []provider.UsageMetric{
			{Name: "monthly_spend", Window: "month", Unit: "usd_cents", Used: 300, Limit: &limit},
		},
	}
	providerSvc.RegisterFetcher(fetcher)

	putRec := putCredentials(t, handler, "openai", `{"values":{"api_key":"sk-test"}}`)
	assertStatus(t, putRec, http.StatusNoContent)

	enableRec := httptest.NewRecorder()
	handler.ServeHTTP(enableRec, httptest.NewRequest(http.MethodPost, "/api/v1/providers/openai/enable", nil))
	assertStatus(t, enableRec, http.StatusOK)

	refreshRec := refreshUsage(t, handler, "openai")
	assertStatus(t, refreshRec, http.StatusOK)
	var refreshed UsageSnapshot
	decodeInto(t, refreshRec, &refreshed)
	if refreshed.CollectedAt == nil {
		t.Fatal("expected refresh response to carry a non-nil CollectedAt")
	}
	if len(refreshed.Metrics) != 1 || refreshed.Metrics[0].Used != 300 {
		t.Fatalf("unexpected refresh response metrics: %+v", refreshed.Metrics)
	}

	usageRec := getUsage(t, handler, "openai")
	assertStatus(t, usageRec, http.StatusOK)
	var usage UsageSnapshot
	decodeInto(t, usageRec, &usage)
	if len(usage.Metrics) != 1 || usage.Metrics[0].Name != "monthly_spend" || usage.Metrics[0].Used != 300 {
		t.Fatalf("expected GET usage to reflect the refreshed snapshot, got %+v", usage.Metrics)
	}
	if usage.Metrics[0].Limit == nil || *usage.Metrics[0].Limit != 500 {
		t.Fatalf("expected Limit 500 to round-trip, got %+v", usage.Metrics[0].Limit)
	}

	if fetcher.calls != 1 {
		t.Fatalf("expected Fetcher to be called exactly once, got %d", fetcher.calls)
	}
}

func TestRefresh_UnknownProviderIs404(t *testing.T) {
	t.Parallel()

	handler, _, _ := newRefreshTestHandler(t)

	rec := refreshUsage(t, handler, "does-not-exist")
	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
}

func TestRefresh_DisabledProviderIs409(t *testing.T) {
	t.Parallel()

	handler, _, _ := newRefreshTestHandler(t)

	rec := refreshUsage(t, handler, "openai")
	assertJSONError(t, rec, http.StatusConflict, ErrorErrorCodeConflict, "provider is disabled")
}

func TestRefresh_UncredentialedEnabledProviderIs409(t *testing.T) {
	t.Parallel()

	handler, providerSvc, _ := newRefreshTestHandler(t)
	if _, err := providerSvc.SetEnabled(context.Background(), "openai", true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	rec := refreshUsage(t, handler, "openai")
	assertJSONError(t, rec, http.StatusConflict, ErrorErrorCodeConflict, "provider is missing required credentials")
}

func TestRefresh_NoRegisteredFetcherIs409(t *testing.T) {
	t.Parallel()

	handler, providerSvc, _ := newRefreshTestHandler(t)
	if _, err := providerSvc.SetEnabled(context.Background(), "anthropic", true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	rec := refreshUsage(t, handler, "anthropic")
	assertJSONError(t, rec, http.StatusConflict, ErrorErrorCodeConflict, "provider has no registered fetcher")
}

func TestRefresh_FetcherErrorIs500(t *testing.T) {
	t.Parallel()

	handler, providerSvc, _ := newRefreshTestHandler(t)
	if _, err := providerSvc.SetEnabled(context.Background(), "anthropic", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	fetcher := &refreshTestFetcher{meta: provider.Metadata{ID: "anthropic"}, err: context.DeadlineExceeded}
	providerSvc.RegisterFetcher(fetcher)

	rec := refreshUsage(t, handler, "anthropic")
	assertJSONError(t, rec, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
}

func TestRefresh_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler, _, _ := newRefreshTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/openai/refresh", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMethodNotAllowed)
}
