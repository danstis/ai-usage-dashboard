package api

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// CredentialRepository is the read/write seam the credential handlers
// depend on. Implementations validate providerID against the provider
// registry (unknown id ⇒ store.ErrNotFound) and, for SetCredentials,
// validate the submitted field set against the provider's declared
// CredentialFields (⇒ *ErrInvalidCredentialFields on a mismatch) before
// touching the credential store. No implementation may ever return a
// stored secret value — GetCredentialPresence is presence-only by
// construction.
type CredentialRepository interface {
	SetCredentials(ctx context.Context, providerID string, values map[string]string) error
	GetCredentialPresence(ctx context.Context, providerID string) ([]CredentialPresence, error)
	DeleteCredentials(ctx context.Context, providerID string) error
}

// ErrInvalidCredentialFields is returned when a PUT body's field set does
// not exactly match the provider's declared credential fields.
type ErrInvalidCredentialFields struct {
	Missing []string
	Unknown []string
}

func (e *ErrInvalidCredentialFields) Error() string {
	return "credentials: submitted fields do not match the provider's declared credential fields"
}

// handleProviderCredentials serves PUT/GET/DELETE
// /api/v1/providers/{id}/credentials.
func handleProviderCredentials(repo CredentialRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			handleSetCredentials(repo, w, r)
		case http.MethodGet:
			handleGetCredentials(repo, w, r)
		case http.MethodDelete:
			handleDeleteCredentials(repo, w, r)
		default:
			methodNotAllowed(w, "PUT, GET, DELETE")
		}
	}
}

func handleSetCredentials(repo CredentialRepository, w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, ErrorErrorCodeUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	var body CredentialValues
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, ErrorErrorCodeValidationError, "invalid request body")
		return
	}

	id := r.PathValue("id")
	if err := repo.SetCredentials(r.Context(), id, body.Values); err != nil {
		writeCredentialError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleGetCredentials(repo CredentialRepository, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	fields, err := repo.GetCredentialPresence(r.Context(), id)
	if err != nil {
		writeCredentialError(w, err)
		return
	}
	if fields == nil {
		fields = []CredentialPresence{}
	}
	writeJSON(w, http.StatusOK, CredentialPresenceList{Fields: fields})
}

func handleDeleteCredentials(repo CredentialRepository, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := repo.DeleteCredentials(r.Context(), id); err != nil {
		writeCredentialError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeCredentialError translates the errors a CredentialRepository may
// return into the canonical /api/v1 envelope: *ErrInvalidCredentialFields
// ⇒ 400 validation_error (with missing/unknown field names as details),
// store.ErrNotFound ⇒ 404 not_found, anything else ⇒ a generic 500 (the
// underlying error is never sent to the client).
func writeCredentialError(w http.ResponseWriter, err error) {
	var fieldErr *ErrInvalidCredentialFields
	if errors.As(err, &fieldErr) {
		details := map[string]any{}
		if len(fieldErr.Missing) > 0 {
			details["missing"] = fieldErr.Missing
		}
		if len(fieldErr.Unknown) > 0 {
			details["unknown"] = fieldErr.Unknown
		}
		writeErrorWithDetails(w, http.StatusBadRequest, ErrorErrorCodeValidationError, "invalid credential fields", details)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
		return
	}
	writeError(w, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
}
