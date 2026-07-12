package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
)

type contextKey int

const requestIDKey contextKey = iota

// contextWithRequestID returns a copy of ctx carrying id as the request id.
func contextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// requestIDFromContext returns the request id injected by withRequestID, or
// "" if none is present.
func requestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// newRequestID returns a random hex-encoded request id.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// withRequestID injects a per-request id into the request context and
// echoes it back to the client as X-Request-Id.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(contextWithRequestID(r.Context(), id)))
	})
}

// statusRecorder captures the status code written to the underlying
// ResponseWriter so middleware can log it after the handler runs.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// withLogging logs one structured line per request via log/slog: method,
// path, status, duration, and request id.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		// The method/path are logged after stripping CR/LF inline (not via a
		// helper) so gosec's log-injection taint analysis recognizes the
		// strings.ReplaceAll calls at the sink and clears the taint.
		slog.Info("http request",
			"method", strings.ReplaceAll(strings.ReplaceAll(r.Method, "\r", ""), "\n", ""),
			"path", strings.ReplaceAll(strings.ReplaceAll(r.URL.Path, "\r", ""), "\n", ""),
			"status", rec.status,
			"duration", time.Since(start),
			"requestId", requestIDFromContext(r.Context()),
		)
	})
}

// withRecovery recovers a panic anywhere downstream in the handler chain
// and responds with a structured 500 instead of crashing the connection.
// The panic value is logged server-side only; it never reaches the client.
func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "error", rec, "path", strings.ReplaceAll(strings.ReplaceAll(r.URL.Path, "\r", ""), "\n", ""))
				writeError(w, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// chain composes middleware so that chain(a, b, c)(h) applies a first, then
// b, then c, then h — i.e. a is outermost.
func chain(mw ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		for _, m := range slices.Backward(mw) {
			h = m(h)
		}
		return h
	}
}
