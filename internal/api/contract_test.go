package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

// specPath is relative to this package's directory, which is where `go
// test` runs with the working directory set.
const specPath = "../../api/openapi.yaml"

// loadSpecRouter loads and validates the committed OpenAPI document and
// builds a router from it so tests can look up the routers.Route for a
// given request and validate a recorded response against it.
func loadSpecRouter(t *testing.T) routers.Router {
	t.Helper()

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("validate spec: %v", err)
	}
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatalf("build router from spec: %v", err)
	}
	return router
}

// assertConformsToSpec asserts that rec (the recorded response for req)
// matches the response shape (status/headers/body schema) the spec declares
// for req's operation.
func assertConformsToSpec(t *testing.T, router routers.Router, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()

	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("FindRoute(%s %s): %v", req.Method, req.URL.Path, err)
	}

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status: rec.Code,
		Header: rec.Header(),
	}
	input.SetBodyBytes(rec.Body.Bytes())

	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		t.Fatalf("response for %s %s does not conform to spec: %v", req.Method, req.URL.Path, err)
	}
}

func TestContract_ProviderEndpointsConformToSpec(t *testing.T) {
	t.Parallel()

	router := loadSpecRouter(t)
	handler := NewHandler(newStubProviderRepository(
		Provider{
			Id:      "openai",
			Name:    "OpenAI",
			Enabled: false,
			CredentialFields: []CredentialField{
				{Name: "api_key", Label: "API Key", Secret: true},
			},
		},
	))

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list providers", http.MethodGet, "/api/v1/providers"},
		{"get provider", http.MethodGet, "/api/v1/providers/openai"},
		{"get unknown provider is 404", http.MethodGet, "/api/v1/providers/does-not-exist"},
		{"enable provider", http.MethodPost, "/api/v1/providers/openai/enable"},
		{"disable provider", http.MethodPost, "/api/v1/providers/openai/disable"},
		{"enable unknown provider is 404", http.MethodPost, "/api/v1/providers/does-not-exist/enable"},
		{"disable unknown provider is 404", http.MethodPost, "/api/v1/providers/does-not-exist/disable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			assertConformsToSpec(t, router, httptest.NewRequest(tc.method, tc.path, nil), rec)
		})
	}
}
