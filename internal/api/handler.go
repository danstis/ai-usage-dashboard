// Package api implements the /api/v1 HTTP surface: the middleware chain
// (request id, structured logging, panic recovery), the canonical error
// envelope, and the handlers for the routes defined in api/openapi.yaml.
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// ProviderRepository is the read/write seam the providers handlers depend
// on. NewProviderRepository adapts a *provider.Service (registry ↔ store)
// to it; this package only depends on the interface so handlers stay
// testable without a real store. Get/Enable/Disable return store.ErrNotFound
// (wrapped or bare) for an unknown id.
type ProviderRepository interface {
	ListProviders(ctx context.Context) ([]Provider, error)
	GetProvider(ctx context.Context, id string) (Provider, error)
	EnableProvider(ctx context.Context, id string) (Provider, error)
	DisableProvider(ctx context.Context, id string) (Provider, error)
}

// NewHandler builds the /api/v1 HTTP handler: the middleware chain (request
// id, structured logging, panic recovery) wrapped around the versioned
// routes, plus a structured 404 for anything under /api/v1 that isn't
// registered.
func NewHandler(repo ProviderRepository, credentials CredentialRepository) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/providers", handleProvidersCollection(repo))
	mux.HandleFunc("/api/v1/providers/{id}", handleProviderItem(repo))
	mux.HandleFunc("/api/v1/providers/{id}/enable", handleProviderEnable(repo))
	mux.HandleFunc("/api/v1/providers/{id}/disable", handleProviderDisable(repo))
	mux.HandleFunc("/api/v1/providers/{id}/credentials", handleProviderCredentials(credentials))
	mux.HandleFunc("/api/v1/", handleNotFound)

	return chain(withRequestID, withLogging, withRecovery)(mux)
}

// methodNotAllowed writes the canonical /api/v1 405 envelope with the given
// Allow header — used by every {collection,item,enable,disable} handler
// that dispatches on a single HTTP method.
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, ErrorErrorCodeValidationError, "method not allowed")
}

// handleProvidersCollection dispatches on method for the single
// /api/v1/providers route so a non-GET request gets the canonical structured
// 405 envelope instead of Go's default plain-text response.
func handleProvidersCollection(repo ProviderRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}

		providers, err := repo.ListProviders(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
			return
		}
		if providers == nil {
			providers = []Provider{}
		}
		writeJSON(w, http.StatusOK, providers)
	}
}

// handleProviderItem serves GET /api/v1/providers/{id}.
func handleProviderItem(repo ProviderRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		respondProvider(w, r, repo.GetProvider)
	}
}

// handleProviderEnable serves POST /api/v1/providers/{id}/enable.
func handleProviderEnable(repo ProviderRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		respondProvider(w, r, repo.EnableProvider)
	}
}

// handleProviderDisable serves POST /api/v1/providers/{id}/disable.
func handleProviderDisable(repo ProviderRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		respondProvider(w, r, repo.DisableProvider)
	}
}

// respondProvider runs fn against the {id} path value and writes the
// resulting provider as JSON, translating store.ErrNotFound into the
// canonical structured 404 and any other error into a structured 500.
func respondProvider(w http.ResponseWriter, r *http.Request, fn func(context.Context, string) (Provider, error)) {
	id := r.PathValue("id")
	p, err := fn(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func handleNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, ErrorErrorCodeNotFound, "resource not found")
}
