package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubProviderRepository is a test double for ProviderRepository.
type stubProviderRepository struct {
	providers []Provider
	err       error
}

func (s stubProviderRepository) ListProviders(_ context.Context) ([]Provider, error) {
	return s.providers, s.err
}

func TestNewHandler_ListProvidersEmpty(t *testing.T) {
	t.Parallel()

	handler := NewHandler(stubProviderRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id header to be set")
	}

	var providers []Provider
	if err := json.NewDecoder(rec.Body).Decode(&providers); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("expected empty providers list, got %v", providers)
	}
}

func TestNewHandler_ListProvidersRepositoryError(t *testing.T) {
	t.Parallel()

	handler := NewHandler(stubProviderRepository{err: errors.New("boom")})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var body Error
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != ErrorErrorCodeInternalError {
		t.Fatalf("expected code %q, got %q", ErrorErrorCodeInternalError, body.Error.Code)
	}
}

func TestNewHandler_UnknownRouteIsStructured404(t *testing.T) {
	t.Parallel()

	handler := NewHandler(stubProviderRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var body Error
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != ErrorErrorCodeNotFound {
		t.Fatalf("expected code %q, got %q", ErrorErrorCodeNotFound, body.Error.Code)
	}
}

func TestNewHandler_MethodNotAllowedIsStructured405(t *testing.T) {
	t.Parallel()

	handler := NewHandler(stubProviderRepository{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("expected Allow header %q, got %q", http.MethodGet, allow)
	}

	var body Error
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code == "" {
		t.Fatal("expected a non-empty error code")
	}
}

func TestNewHandler_PanicRecoveredAsStructured500(t *testing.T) {
	t.Parallel()

	handler := NewHandler(panickingRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()

	// Should not panic even though the repository panics.
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var body Error
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != ErrorErrorCodeInternalError {
		t.Fatalf("expected code %q, got %q", ErrorErrorCodeInternalError, body.Error.Code)
	}
}

type panickingRepository struct{}

func (panickingRepository) ListProviders(_ context.Context) ([]Provider, error) {
	panic("boom")
}

func TestNewInMemoryProviderRepository_ListsEmpty(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryProviderRepository()

	providers, err := repo.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("expected empty providers list, got %v", providers)
	}
}
