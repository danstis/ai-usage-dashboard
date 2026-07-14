package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// stubProviderRepository is a test double for ProviderRepository, backed by
// an in-memory map keyed by provider id.
type stubProviderRepository struct {
	providers map[string]Provider
	listErr   error
}

func newStubProviderRepository(providers ...Provider) *stubProviderRepository {
	byID := make(map[string]Provider, len(providers))
	for _, p := range providers {
		byID[p.Id] = p
	}
	return &stubProviderRepository{providers: byID}
}

func (s *stubProviderRepository) ListProviders(_ context.Context) ([]Provider, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]Provider, 0, len(s.providers))
	for _, p := range s.providers {
		out = append(out, p)
	}
	return out, nil
}

func (s *stubProviderRepository) GetProvider(_ context.Context, id string) (Provider, error) {
	p, ok := s.providers[id]
	if !ok {
		return Provider{}, store.ErrNotFound
	}
	return p, nil
}

func (s *stubProviderRepository) EnableProvider(_ context.Context, id string) (Provider, error) {
	p, ok := s.providers[id]
	if !ok {
		return Provider{}, store.ErrNotFound
	}
	p.Enabled = true
	s.providers[id] = p
	return p, nil
}

func (s *stubProviderRepository) DisableProvider(_ context.Context, id string) (Provider, error) {
	p, ok := s.providers[id]
	if !ok {
		return Provider{}, store.ErrNotFound
	}
	p.Enabled = false
	s.providers[id] = p
	return p, nil
}

func TestNewHandler_ListProvidersEmpty(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository())

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

func TestNewHandler_ListProvidersReturnsSeeded(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository(
		Provider{Id: "openai", Name: "OpenAI", Enabled: false},
		Provider{Id: "anthropic", Name: "Anthropic", Enabled: true},
	))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	providers := decodeProviders(t, rec)
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %v", providers)
	}
}

func TestNewHandler_ListProvidersRepositoryError(t *testing.T) {
	t.Parallel()

	repo := newStubProviderRepository()
	repo.listErr = errors.New("boom")
	handler := newHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
}

func TestNewHandler_UnknownRouteIsStructured404(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "resource not found")
}

func TestNewHandler_MethodNotAllowedIsStructured405(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository())

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

	handler := newHandler(panickingRepository{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	rec := httptest.NewRecorder()

	// Should not panic even though the repository panics.
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
}

type panickingRepository struct{}

func (panickingRepository) ListProviders(_ context.Context) ([]Provider, error) { panic("boom") }
func (panickingRepository) GetProvider(_ context.Context, _ string) (Provider, error) {
	panic("boom")
}
func (panickingRepository) EnableProvider(_ context.Context, _ string) (Provider, error) {
	panic("boom")
}
func (panickingRepository) DisableProvider(_ context.Context, _ string) (Provider, error) {
	panic("boom")
}

func TestNewHandler_GetProvider(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository(
		Provider{Id: "openai", Name: "OpenAI", Enabled: true},
	))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/openai", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	var got Provider
	decodeInto(t, rec, &got)
	if got.Id != "openai" || !got.Enabled {
		t.Fatalf("unexpected provider: %+v", got)
	}
}

func TestNewHandler_GetProvider_UnknownID(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/does-not-exist", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
}

func TestNewHandler_GetProvider_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository(Provider{Id: "openai"}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/openai", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("expected Allow header %q, got %q", http.MethodGet, allow)
	}
}

func TestNewHandler_EnableProvider(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository(
		Provider{Id: "openai", Name: "OpenAI", Enabled: false},
	))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/openai/enable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	var got Provider
	decodeInto(t, rec, &got)
	if !got.Enabled {
		t.Fatalf("expected provider enabled, got %+v", got)
	}
}

func TestNewHandler_EnableProvider_IsIdempotent(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository(
		Provider{Id: "openai", Name: "OpenAI", Enabled: true},
	))

	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/openai/enable", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assertStatus(t, rec, http.StatusOK)
		var got Provider
		decodeInto(t, rec, &got)
		if !got.Enabled {
			t.Fatalf("call %d: expected provider enabled, got %+v", i, got)
		}
	}
}

func TestNewHandler_EnableProvider_UnknownID(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/does-not-exist/enable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
}

func TestNewHandler_EnableProvider_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository(Provider{Id: "openai"}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/openai/enable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("expected Allow header %q, got %q", http.MethodPost, allow)
	}
}

func TestNewHandler_DisableProvider(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository(
		Provider{Id: "openai", Name: "OpenAI", Enabled: true},
	))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/openai/disable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	var got Provider
	decodeInto(t, rec, &got)
	if got.Enabled {
		t.Fatalf("expected provider disabled, got %+v", got)
	}
}

func TestNewHandler_DisableProvider_IsIdempotent(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository(
		Provider{Id: "openai", Name: "OpenAI", Enabled: false},
	))

	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/openai/disable", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assertStatus(t, rec, http.StatusOK)
		var got Provider
		decodeInto(t, rec, &got)
		if got.Enabled {
			t.Fatalf("call %d: expected provider disabled, got %+v", i, got)
		}
	}
}

func TestNewHandler_DisableProvider_UnknownID(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/does-not-exist/disable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
}

func TestNewHandler_DisableProvider_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler := newHandler(newStubProviderRepository(Provider{Id: "openai"}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/openai/disable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("expected Allow header %q, got %q", http.MethodPost, allow)
	}
}
