package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/credential"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/store"
	"github.com/danstis/ai-usage-dashboard/internal/store/sqlite"
)

// usageTestRegistry is a small fixture registry, independent of the
// production provider.Registry, so this test doesn't depend on its exact
// contents.
var usageTestRegistry = []provider.Metadata{
	{ID: "openai", Name: "OpenAI"},
	{ID: "anthropic", Name: "Anthropic"},
}

// newUsageTestHandler builds a handler backed by a real sqlite store so
// GetSnapshot/Replace round-trip through the actual repository, returning
// the handler and the store.Store so tests can seed snapshots directly.
func newUsageTestHandler(t *testing.T) (http.Handler, store.Store) {
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

	providerSvc := provider.NewService(db, usageTestRegistry)
	if err := providerSvc.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	key := make([]byte, 32)
	credentialSvc := credential.NewService(db, key)

	handler := NewHandler(
		NewProviderRepository(providerSvc),
		NewCredentialRepository(providerSvc, credentialSvc),
		NewUsageGetter(providerSvc, db),
	)
	return handler, db
}

func getUsage(t *testing.T, handler http.Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/providers/"+id+"/usage", nil))
	return rec
}

func TestUsage_NeverCollectedReturns200PendingSnapshot(t *testing.T) {
	t.Parallel()

	handler, _ := newUsageTestHandler(t)

	rec := getUsage(t, handler, "openai")
	assertStatus(t, rec, http.StatusOK)

	var got UsageSnapshot
	decodeInto(t, rec, &got)
	if got.CollectedAt != nil {
		t.Fatalf("expected nil CollectedAt for a never-collected provider, got %v", *got.CollectedAt)
	}
	if len(got.Metrics) != 0 {
		t.Fatalf("expected no metrics for a never-collected provider, got %+v", got.Metrics)
	}
}

func TestUsage_UnknownProviderIs404(t *testing.T) {
	t.Parallel()

	handler, _ := newUsageTestHandler(t)

	rec := getUsage(t, handler, "does-not-exist")
	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
}

func TestUsage_AfterReplaceReturnsLatestSnapshot(t *testing.T) {
	t.Parallel()

	handler, db := newUsageTestHandler(t)

	collectedAt := time.Date(2026, 7, 14, 9, 30, 0, 0, time.UTC)
	limit := int64(100)
	metrics := []store.Metric{
		{Name: "requests", Window: "day", Unit: "count", Used: 42, Limit: &limit},
	}
	if err := db.Replace(context.Background(), "openai", metrics, collectedAt); err != nil {
		t.Fatalf("Replace() returned error: %v", err)
	}

	rec := getUsage(t, handler, "openai")
	assertStatus(t, rec, http.StatusOK)

	var got UsageSnapshot
	decodeInto(t, rec, &got)
	if got.CollectedAt == nil || !got.CollectedAt.Equal(collectedAt) {
		t.Fatalf("expected CollectedAt %v, got %+v", collectedAt, got.CollectedAt)
	}
	if len(got.Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %+v", got.Metrics)
	}
	m := got.Metrics[0]
	if m.Name != "requests" || m.Used != 42 || m.Limit == nil || *m.Limit != 100 {
		t.Fatalf("unexpected metric: %+v", m)
	}
}

func TestUsage_SecondReplaceOverwritesPreviousSnapshot(t *testing.T) {
	t.Parallel()

	handler, db := newUsageTestHandler(t)
	ctx := context.Background()

	first := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	second := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	if err := db.Replace(ctx, "openai", []store.Metric{{Name: "requests", Window: "day", Unit: "count", Used: 1}}, first); err != nil {
		t.Fatalf("first Replace() returned error: %v", err)
	}
	if err := db.Replace(ctx, "openai", []store.Metric{{Name: "requests", Window: "day", Unit: "count", Used: 2}}, second); err != nil {
		t.Fatalf("second Replace() returned error: %v", err)
	}

	rec := getUsage(t, handler, "openai")
	assertStatus(t, rec, http.StatusOK)

	var got UsageSnapshot
	decodeInto(t, rec, &got)
	if len(got.Metrics) != 1 || got.Metrics[0].Used != 2 {
		t.Fatalf("expected replace-on-write with latest Used=2, got %+v", got.Metrics)
	}
	if got.CollectedAt == nil || !got.CollectedAt.Equal(second) {
		t.Fatalf("expected CollectedAt %v, got %+v", second, got.CollectedAt)
	}
}

func TestUsage_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler, _ := newUsageTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/openai/usage", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMethodNotAllowed)
}
