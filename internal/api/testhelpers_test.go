package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubCredentialRepository is a no-op test double for CredentialRepository,
// sufficient for handler tests that don't exercise the credentials
// endpoints.
type stubCredentialRepository struct{}

func (stubCredentialRepository) SetCredentials(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

func (stubCredentialRepository) GetCredentialPresence(_ context.Context, _ string) ([]CredentialPresence, error) {
	return nil, nil
}

func (stubCredentialRepository) DeleteCredentials(_ context.Context, _ string) error {
	return nil
}

// newHandler builds a handler with a no-op CredentialRepository stub, for
// tests that only exercise the provider endpoints.
func newHandler(providers ProviderRepository) http.Handler {
	return NewHandler(providers, stubCredentialRepository{})
}

// assertStatus fails the test if rec.Code does not equal want.
func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("expected status %d, got %d", want, rec.Code)
	}
}

// assertJSONHeader fails the test if rec's Content-Type is not
// "application/json".
func assertJSONHeader(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
}

// assertJSONError asserts that rec carries the canonical /api/v1 error
// envelope — status, Content-Type, and inner code/message — and returns the
// decoded body so callers can perform additional checks (e.g. on Details).
// Use it from every test that exercises the structured error responder so the
// boilerplate stays in one place.
func assertJSONError(t *testing.T, rec *httptest.ResponseRecorder, status int, code ErrorErrorCode, message string) Error {
	t.Helper()
	assertStatus(t, rec, status)
	assertJSONHeader(t, rec)
	body := decodeError(t, rec)
	if body.Error.Code != code {
		t.Fatalf("expected code %q, got %q", code, body.Error.Code)
	}
	if body.Error.Message != message {
		t.Fatalf("expected message %q, got %q", message, body.Error.Message)
	}
	return body
}

// decodeError decodes rec's body as the canonical /api/v1 error envelope.
// Tests that have already verified status/Content-Type (or that need to make
// additional assertions on the inner code) use this directly.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) Error {
	t.Helper()
	var body Error
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

// decodeProviders decodes rec's body as a providers slice.
func decodeProviders(t *testing.T, rec *httptest.ResponseRecorder) []Provider {
	t.Helper()
	var providers []Provider
	if err := json.NewDecoder(rec.Body).Decode(&providers); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return providers
}

// decodeInto decodes rec's body into v.
func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}
