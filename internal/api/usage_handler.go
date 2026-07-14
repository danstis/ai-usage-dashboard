package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// SnapshotRepository is the read seam the usage handler depends on.
// GetUsage returns store.ErrNotFound only for an unknown provider id — a
// known provider that has never been collected is a pending snapshot
// (CollectedAt nil), not an error; see NewSnapshotRepository.
type SnapshotRepository interface {
	GetUsage(ctx context.Context, providerID string) (UsageSnapshot, error)
}

// handleProviderUsage serves GET /api/v1/providers/{id}/usage.
func handleProviderUsage(repo SnapshotRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}

		id := r.PathValue("id")
		snap, err := repo.GetUsage(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
				return
			}
			writeError(w, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, snap)
	}
}
