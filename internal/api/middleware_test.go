package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithRequestID_SetsHeaderAndContext(t *testing.T) {
	t.Parallel()

	var sawID string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sawID = requestIDFromContext(r.Context())
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)

	withRequestID(next).ServeHTTP(rec, req)

	header := rec.Header().Get("X-Request-Id")
	if header == "" {
		t.Fatal("expected X-Request-Id header to be set")
	}
	if sawID != header {
		t.Fatalf("expected context request id %q to match header %q", sawID, header)
	}
}

func TestWithLogging_LogsStructuredLine(t *testing.T) {
	var buf bytes.Buffer
	restore := swapDefaultLogger(&buf)
	defer restore()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)
	req = req.WithContext(contextWithRequestID(req.Context(), "test-id"))

	withLogging(next).ServeHTTP(rec, req)

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("expected one structured JSON log line, got %q: %v", buf.String(), err)
	}

	for _, key := range []string{"method", "path", "status", "duration", "requestId"} {
		if _, ok := line[key]; !ok {
			t.Fatalf("expected log line to contain %q, got %v", key, line)
		}
	}
	if line["method"] != http.MethodGet {
		t.Fatalf("expected method %q, got %v", http.MethodGet, line["method"])
	}
	if line["path"] != "/api/v1/providers" {
		t.Fatalf("expected path %q, got %v", "/api/v1/providers", line["path"])
	}
	if line["status"] != float64(http.StatusTeapot) {
		t.Fatalf("expected status %v, got %v", http.StatusTeapot, line["status"])
	}
	if line["requestId"] != "test-id" {
		t.Fatalf("expected requestId %q, got %v", "test-id", line["requestId"])
	}
}

func TestWithLogging_DefaultsStatusToOK(t *testing.T) {
	var buf bytes.Buffer
	restore := swapDefaultLogger(&buf)
	defer restore()

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)

	withLogging(next).ServeHTTP(rec, req)

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	if line["status"] != float64(http.StatusOK) {
		t.Fatalf("expected default status %v, got %v", http.StatusOK, line["status"])
	}
}

func TestWithRecovery_RecoversPanicAsStructured500(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)

	withRecovery(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var body Error
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != ErrorErrorCodeInternalError {
		t.Fatalf("expected code %q, got %q", ErrorErrorCodeInternalError, body.Error.Code)
	}
	if strings.Contains(body.Error.Message, "boom") {
		t.Fatalf("expected panic detail not to leak into message, got %q", body.Error.Message)
	}
}

func TestWithRecovery_NoPanicPassesThrough(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil)

	withRecovery(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestChain_AppliesOutermostFirst(t *testing.T) {
	t.Parallel()

	var order []string
	mw := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	handler := chain(mw("a"), mw("b"), mw("c"))(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		order = append(order, "handler")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	want := []string{"a", "b", "c", "handler"}
	if len(order) != len(want) {
		t.Fatalf("expected order %v, got %v", want, order)
	}
	for i, name := range want {
		if order[i] != name {
			t.Fatalf("expected order %v, got %v", want, order)
		}
	}
}

// swapDefaultLogger replaces the slog default logger with one writing JSON
// to buf, returning a func that restores the previous default.
func swapDefaultLogger(buf *bytes.Buffer) func() {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	return func() { slog.SetDefault(prev) }
}
