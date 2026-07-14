// Package server builds the HTTP handler for the AI Usage Dashboard service.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/danstis/ai-usage-dashboard/internal/api"
	"github.com/danstis/ai-usage-dashboard/internal/docs"
)

// New constructs the top-level HTTP handler for the service, serving the
// provider registry endpoints from providers, the credential endpoints from
// credentials, and the usage snapshot endpoint from snapshots.
func New(providers api.ProviderRepository, credentials api.CredentialRepository, snapshots api.SnapshotRepository) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /swaggerui", docs.HandleUI)
	mux.HandleFunc("GET /swaggerui/openapi.yaml", docs.HandleSpec)
	mux.Handle("/api/v1/", api.NewHandler(providers, credentials, snapshots))
	return mux
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		slog.Error("encode healthz response", "error", err)
	}
}
