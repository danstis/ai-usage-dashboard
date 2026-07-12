package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// writeError writes the canonical S1 error envelope ({error:{code,message}})
// with the given HTTP status. Callers on 5xx paths must pass a generic
// message — internal detail is never sent to the client, only logged.
func writeError(w http.ResponseWriter, status int, code ErrorErrorCode, message string) {
	var body Error
	body.Error.Code = code
	body.Error.Message = message

	writeJSON(w, status, body)
}

// writeJSON writes v as the JSON response body with the given status,
// setting the canonical Content-Type for the /api/v1 surface.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "error", err)
	}
}
