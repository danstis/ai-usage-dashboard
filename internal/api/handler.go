// Package api implements the /api/v1 HTTP surface: the middleware chain
// (request id, structured logging, panic recovery), the canonical error
// envelope, and the handlers for the routes defined in api/openapi.yaml.
package api

import (
	"context"
	"net/http"
)

// ProviderLister is the read seam the providers handler depends on. S3
// supplies a SQLite-backed implementation (internal/store); S4 wires the
// seeded registry through it. This package only depends on the interface so
// the HTTP contract can be exercised before persistence lands.
type ProviderLister interface {
	ListProviders(ctx context.Context) ([]Provider, error)
}

// NewHandler builds the /api/v1 HTTP handler: the middleware chain (request
// id, structured logging, panic recovery) wrapped around the versioned
// routes, plus a structured 404 for anything under /api/v1 that isn't
// registered.
func NewHandler(repo ProviderLister) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/providers", handleProviders(repo))
	mux.HandleFunc("/api/v1/", handleNotFound)

	return chain(withRequestID, withLogging, withRecovery)(mux)
}

// handleProviders dispatches on method for the single /api/v1/providers
// route registered above so a non-GET request gets the canonical structured
// 405 envelope instead of Go's default plain-text response.
func handleProviders(repo ProviderLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, http.StatusMethodNotAllowed, ErrorErrorCodeValidationError, "method not allowed")
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

func handleNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, ErrorErrorCodeNotFound, "resource not found")
}

// inMemoryProviderLister is a placeholder ProviderLister that always reports
// zero providers. It exists so the /providers contract can be exercised
// before S3's persistent store lands; S4 replaces it with the seeded
// registry backed by that store.
type inMemoryProviderLister struct{}

// NewInMemoryProviderLister returns the placeholder ProviderLister used by
// server.New until S3/S4 land.
func NewInMemoryProviderLister() ProviderLister {
	return inMemoryProviderLister{}
}

func (inMemoryProviderLister) ListProviders(_ context.Context) ([]Provider, error) {
	return []Provider{}, nil
}
