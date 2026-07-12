package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		code    ErrorErrorCode
		message string
	}{
		{"not found", http.StatusNotFound, ErrorErrorCodeNotFound, "resource not found"},
		{"internal error", http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error"},
		{"validation error", http.StatusBadRequest, ErrorErrorCodeValidationError, "invalid request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			writeError(rec, tt.status, tt.code, tt.message)

			if rec.Code != tt.status {
				t.Fatalf("expected status %d, got %d", tt.status, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("expected Content-Type application/json, got %q", ct)
			}

			var body Error
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error.Code != tt.code {
				t.Fatalf("expected code %q, got %q", tt.code, body.Error.Code)
			}
			if body.Error.Message != tt.message {
				t.Fatalf("expected message %q, got %q", tt.message, body.Error.Message)
			}
			if body.Error.Details != nil {
				t.Fatalf("expected no details, got %v", *body.Error.Details)
			}
		})
	}
}

// errWriter fails on the first Write so writeError's json.Encode call
// observes a non-nil error and exercises its slog.Error log branch, without
// panicking the caller.
type errWriter struct {
	header http.Header
}

func (w *errWriter) Header() http.Header       { return w.header }
func (w *errWriter) WriteHeader(int)           {}
func (w *errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestWriteError_EncodeErrorDoesNotPanic(t *testing.T) {
	t.Parallel()

	w := &errWriter{header: http.Header{}}

	writeError(w, http.StatusInternalServerError, ErrorErrorCodeInternalError, "internal server error")

	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected Content-Type to be set, got %q", w.Header().Get("Content-Type"))
	}
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, []Provider{})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Fatalf("expected empty array payload, got %q", got)
	}
}
