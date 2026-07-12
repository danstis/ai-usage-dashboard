// Package server builds the HTTP handler for AI Usage Dashboard.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// New returns the application HTTP handler.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)

	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		slog.Error("write health response", "error", err)
	}
}
