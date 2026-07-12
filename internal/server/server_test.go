package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/api"
	"github.com/danstis/ai-usage-dashboard/internal/server"
)

// stubProviderRepository is a no-op api.ProviderRepository, sufficient for
// exercising routes (like /healthz) that don't depend on provider state.
type stubProviderRepository struct{}

func (stubProviderRepository) ListProviders(_ context.Context) ([]api.Provider, error) {
	return nil, nil
}

func (stubProviderRepository) GetProvider(_ context.Context, _ string) (api.Provider, error) {
	return api.Provider{}, nil
}

func (stubProviderRepository) EnableProvider(_ context.Context, _ string) (api.Provider, error) {
	return api.Provider{}, nil
}

func (stubProviderRepository) DisableProvider(_ context.Context, _ string) (api.Provider, error) {
	return api.Provider{}, nil
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	handler := server.New(stubProviderRepository{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type %q, got %q", "application/json", ct)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body.Status != "ok" {
		t.Fatalf("expected status %q, got %q", "ok", body.Status)
	}
}

// errWriter fails on the first Write so the json.Encode call inside
// handleHealthz observes a non-nil error and exercises its slog.Error log
// branch.
type errWriter struct {
	header http.Header
}

func (w *errWriter) Header() http.Header { return w.header }
func (w *errWriter) WriteHeader(int)     {}
func (w *errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestHealthz_EncodeErrorDoesNotPanic(t *testing.T) {
	t.Parallel()

	handler := server.New(stubProviderRepository{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := &errWriter{header: http.Header{}}

	// Should not panic even though the underlying writer returns an error.
	handler.ServeHTTP(w, req)

	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected Content-Type to be set, got %q", w.Header().Get("Content-Type"))
	}
}
