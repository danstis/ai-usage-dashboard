package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/credential"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/store/sqlite"
)

// credentialTestRegistry declares credential fields, unlike persistence_test.go's
// testRegistry, so the write-only credential endpoints have something to
// validate against.
var credentialTestRegistry = []provider.Metadata{
	{
		ID:   "openai",
		Name: "OpenAI",
		CredentialFields: []provider.CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
			{Name: "org_id", Label: "Organization ID", Secret: false},
		},
	},
	{ID: "anthropic", Name: "Anthropic"},
}

func newCredentialTestHandler(t *testing.T) http.Handler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "aud.db")
	db, err := sqlite.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})

	providerSvc := provider.NewService(db, credentialTestRegistry)
	if err := providerSvc.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	key := make([]byte, 32)
	credentialSvc := credential.NewService(db, key, nil)

	return NewHandler(NewProviderRepository(providerSvc), NewCredentialRepository(providerSvc, credentialSvc), NewUsageGetter(providerSvc, db), stubUsageRefresher{})
}

func putCredentials(t *testing.T, handler http.Handler, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/providers/"+id+"/credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestCredentials_PutThenGet_ConfiguredTrueNoSecretLeak(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)
	secretValue := "sk-super-secret-value-12345"

	rec := putCredentials(t, handler, "openai", `{"values":{"api_key":"`+secretValue+`","org_id":"org-123"}}`)
	assertStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body on 204, got %q", rec.Body.String())
	}

	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/v1/providers/openai/credentials", nil))
	assertStatus(t, getRec, http.StatusOK)

	if bytes.Contains(getRec.Body.Bytes(), []byte(secretValue)) {
		t.Fatalf("GET response leaked the raw secret value: %s", getRec.Body.String())
	}

	var got CredentialPresenceList
	decodeInto(t, getRec, &got)
	want := map[string]bool{"api_key": true, "org_id": true}
	if len(got.Fields) != len(want) {
		t.Fatalf("expected %d fields, got %+v", len(want), got.Fields)
	}
	for _, f := range got.Fields {
		if !f.Configured {
			t.Errorf("expected field %q configured, got %+v", f.Name, f)
		}
		delete(want, f.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing expected fields in response: %+v, got %+v", want, got.Fields)
	}
}

func TestCredentials_Get_BeforeAnyPutShowsUnconfigured(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/providers/openai/credentials", nil))
	assertStatus(t, rec, http.StatusOK)

	var got CredentialPresenceList
	decodeInto(t, rec, &got)
	if len(got.Fields) != 2 {
		t.Fatalf("expected 2 declared fields, got %+v", got.Fields)
	}
	for _, f := range got.Fields {
		if f.Configured {
			t.Errorf("expected field %q unconfigured before any PUT, got %+v", f.Name, f)
		}
	}
}

func TestCredentials_Put_MissingFieldIsValidationError(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	rec := putCredentials(t, handler, "openai", `{"values":{"api_key":"only-one-field"}}`)
	body := assertJSONError(t, rec, http.StatusBadRequest, ErrorErrorCodeValidationError, "invalid credential fields")
	if body.Error.Details == nil {
		t.Fatal("expected details on validation error")
	}
	missing, ok := (*body.Error.Details)["missing"]
	if !ok {
		t.Fatalf("expected 'missing' detail, got %+v", *body.Error.Details)
	}
	if !strings.Contains(toJSONString(missing), "org_id") {
		t.Fatalf("expected missing field org_id, got %v", missing)
	}
}

func TestCredentials_Put_UnknownFieldIsValidationError(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	rec := putCredentials(t, handler, "openai", `{"values":{"api_key":"v","org_id":"v","bogus_field":"v"}}`)
	body := assertJSONError(t, rec, http.StatusBadRequest, ErrorErrorCodeValidationError, "invalid credential fields")
	if body.Error.Details == nil {
		t.Fatal("expected details on validation error")
	}
	unknown, ok := (*body.Error.Details)["unknown"]
	if !ok {
		t.Fatalf("expected 'unknown' detail, got %+v", *body.Error.Details)
	}
	if !strings.Contains(toJSONString(unknown), "bogus_field") {
		t.Fatalf("expected unknown field bogus_field, got %v", unknown)
	}
}

func TestCredentials_Put_UnknownProviderIs404(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	rec := putCredentials(t, handler, "does-not-exist", `{"values":{}}`)
	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
}

func TestCredentials_Get_UnknownProviderIs404(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/providers/does-not-exist/credentials", nil))
	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
}

func TestCredentials_Delete_UnknownProviderIs404(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/providers/does-not-exist/credentials", nil))
	assertJSONError(t, rec, http.StatusNotFound, ErrorErrorCodeNotFound, "provider not found")
}

func TestCredentials_Put_WrongContentTypeIs415(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/providers/openai/credentials", strings.NewReader(`{"values":{}}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertJSONError(t, rec, http.StatusUnsupportedMediaType, ErrorErrorCodeUnsupportedMediaType, "Content-Type must be application/json")
}

func TestCredentials_Put_MissingContentTypeIs415(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/providers/openai/credentials", strings.NewReader(`{"values":{}}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType)
}

func TestCredentials_Put_MalformedBodyIsValidationError(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	rec := putCredentials(t, handler, "openai", `{not-json`)
	assertJSONError(t, rec, http.StatusBadRequest, ErrorErrorCodeValidationError, "invalid request body")
}

func TestCredentials_DeleteClearsPresence(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	rec := putCredentials(t, handler, "openai", `{"values":{"api_key":"v","org_id":"v"}}`)
	assertStatus(t, rec, http.StatusNoContent)

	delRec := httptest.NewRecorder()
	handler.ServeHTTP(delRec, httptest.NewRequest(http.MethodDelete, "/api/v1/providers/openai/credentials", nil))
	assertStatus(t, delRec, http.StatusNoContent)

	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/v1/providers/openai/credentials", nil))
	assertStatus(t, getRec, http.StatusOK)
	var got CredentialPresenceList
	decodeInto(t, getRec, &got)
	for _, f := range got.Fields {
		if f.Configured {
			t.Errorf("expected field %q unconfigured after DELETE, got %+v", f.Name, f)
		}
	}
}

func TestCredentials_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler := newCredentialTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/openai/credentials", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMethodNotAllowed)
}

// toJSONString renders v (an any decoded from the Error.Details map) as a
// string for a simple substring assertion, regardless of whether it
// unmarshalled as []string or []interface{}.
func toJSONString(v any) string {
	switch vv := v.(type) {
	case []string:
		return strconv.Quote(strings.Join(vv, ","))
	case []any:
		parts := make([]string, 0, len(vv))
		for _, e := range vv {
			parts = append(parts, e.(string))
		}
		return strconv.Quote(strings.Join(parts, ","))
	default:
		return ""
	}
}
