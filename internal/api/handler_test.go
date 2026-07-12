package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubProviderLister is a test double for ProviderLister.
type stubProviderLister struct {
	providers []Provider
	err       error
}

func (s stubProviderLister) ListProviders(_ context.Context) ([]Provider, error) {
	return s.providers, s.err
}

func TestNewHandler_ListProvidersEmpty(t *testing.T) {
	t.Parallel()

	handler := NewHandler(stubProviderLister{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	assertJSONHeader(t, rec)
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id header to be set")
	}

	providers := decodeProviders(t, rec)
	if len(providers) != 0 {
		t.Fatalf("expected empty providers list, got %v", providers)
	}
}

func TestNewHandler_ListProvidersRepositoryError(t *testing.T) {
	t.Parallel()

	handler := NewHandler(stubProviderLister{err: errors.New("boom")})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
}

func TestNewHandler_UnknownRouteIsStructured404(t *testing.T) {
	t.Parallel()

	handler := NewHandler(stubProviderLister{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "resource not found")
}

func TestNewHandler_MethodNotAllowedIsStructured405(t *testing.T) {
	t.Parallel()

	handler := NewHandler(stubProviderLister{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	assertJSONHeader(t, rec)
	if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("expected Allow header %q, got %q", http.MethodGet, allow)
	}

	body := decodeError(t, rec)
	if body.Error.Code == "" {
		t.Fatal("expected a non-empty error code")
	}
}

func TestNewHandler_PanicRecoveredAsStructured500(t *testing.T) {
	t.Parallel()

	handler := NewHandler(panickingLister{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()

	// Should not panic even though the repository panics.
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
}

type panickingLister struct{}

func (panickingLister) ListProviders(_ context.Context) ([]Provider, error) {
	panic("boom")
}

func TestNewInMemoryProviderLister_ListsEmpty(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryProviderLister()

	providers, err := repo.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("expected empty providers list, got %v", providers)
	}
}
