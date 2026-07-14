package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/scheduler"
	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// UsageRefresher is the read/write seam the refresh handler depends on.
// RefreshUsage synchronously fetches fresh usage for providerID and
// persists it as the new latest snapshot, returning the result.
type UsageRefresher interface {
	RefreshUsage(ctx context.Context, providerID string) (UsageSnapshot, error)
}

// handleProviderRefresh serves POST /api/v1/providers/{id}/refresh.
func handleProviderRefresh(refresher UsageRefresher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}

		id := r.PathValue("id")
		snap, err := refresher.RefreshUsage(r.Context(), id)
		if err != nil {
			writeRefreshError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snap)
	}
}

// writeRefreshError translates the errors a UsageRefresher may return into
// the canonical /api/v1 envelope: store.ErrNotFound ⇒ 404 not_found,
// scheduler.ErrProviderDisabled / scheduler.ErrProviderUncredentialed /
// provider.ErrFetcherNotFound (surfaced via the scheduler seam — the
// provider is known but cannot be fetched right now) ⇒ 409 conflict,
// anything else ⇒ a generic 500 (the underlying error is never sent to the
// client).
func writeRefreshError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
		return
	}
	if errors.Is(err, scheduler.ErrProviderDisabled) {
		writeError(w, http.StatusConflict, ErrorErrorCodeConflict, "provider is disabled")
		return
	}
	if errors.Is(err, scheduler.ErrProviderUncredentialed) {
		writeError(w, http.StatusConflict, ErrorErrorCodeConflict, "provider is missing required credentials")
		return
	}
	if errors.Is(err, provider.ErrFetcherNotFound) {
		writeError(w, http.StatusConflict, ErrorErrorCodeConflict, "provider has no registered fetcher")
		return
	}
	writeError(w, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
}
