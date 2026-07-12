package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/store/sqlite"
)

// testRegistry is a small fixture registry so this test doesn't depend on
// provider.Registry's exact production contents.
var testRegistry = []provider.Metadata{
	{ID: "openai", Name: "OpenAI"},
	{ID: "anthropic", Name: "Anthropic"},
}

// TestPersistence_EnabledStateSurvivesRestart is the P1 acceptance test: it
// enables a provider through one handler/store instance, closes and
// re-opens the same on-disk SQLite file behind a brand new store, service,
// and handler set, and asserts the enabled state is still there — exactly
// what a process restart against the same AUD_DB_PATH does.
func TestPersistence_EnabledStateSurvivesRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "aud.db")
	ctx := context.Background()

	db1, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	svc1 := provider.NewService(db1, testRegistry)
	if err := svc1.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	handler1 := NewHandler(NewProviderRepository(svc1))

	// All providers start disabled by default.
	rec := httptest.NewRecorder()
	handler1.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil))
	for _, p := range decodeProviders(t, rec) {
		if p.Enabled {
			t.Fatalf("expected all providers disabled by default, got %+v", p)
		}
	}

	// Enable one via the API.
	rec = httptest.NewRecorder()
	handler1.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/providers/openai/enable", nil))
	assertStatus(t, rec, http.StatusOK)
	var enabled Provider
	decodeInto(t, rec, &enabled)
	if !enabled.Enabled {
		t.Fatalf("expected openai enabled immediately after POST, got %+v", enabled)
	}

	if err := db1.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	// Simulate a process restart: reopen the same file behind a fresh
	// store, service, and handler set.
	db2, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() {
		if err := db2.Close(); err != nil {
			t.Errorf("close second store: %v", err)
		}
	})
	svc2 := provider.NewService(db2, testRegistry)
	if err := svc2.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile after restart: %v", err)
	}
	handler2 := NewHandler(NewProviderRepository(svc2))

	rec = httptest.NewRecorder()
	handler2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/providers/openai", nil))
	assertStatus(t, rec, http.StatusOK)
	var got Provider
	decodeInto(t, rec, &got)
	if !got.Enabled {
		t.Fatalf("expected openai to still be enabled after restart, got %+v", got)
	}
}
