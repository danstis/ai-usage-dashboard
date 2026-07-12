// Package server builds the HTTP handler for the AI Usage Dashboard service.
package server

import (
	"encoding/json"
	"net/http"
)

// New constructs the top-level HTTP handler for the service.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	return mux
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
