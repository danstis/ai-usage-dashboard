package docs_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/docs"
)

func TestHandleUI(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/swaggerui", nil)
	rec := httptest.NewRecorder()

	docs.HandleUI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected Content-Type text/html, got %q", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "SwaggerUIBundle") {
		t.Fatalf("expected body to reference SwaggerUIBundle, got: %s", body)
	}
	if !strings.Contains(body, "/swaggerui/openapi.yaml") {
		t.Fatalf("expected body to point at /swaggerui/openapi.yaml, got: %s", body)
	}

	// The CDN-loaded swagger-ui-dist assets must carry Subresource Integrity
	// attributes (SonarCloud Web:S5725) so a compromised or MITM'd CDN
	// response is rejected by the browser instead of executed.
	if strings.Count(body, `integrity="sha384-`) != 2 {
		t.Fatalf("expected both CDN tags to carry an SRI integrity attribute, got: %s", body)
	}
	if strings.Count(body, `crossorigin="anonymous"`) != 2 {
		t.Fatalf("expected both CDN tags to carry crossorigin=\"anonymous\", got: %s", body)
	}
}

func TestHandleSpec(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/swaggerui/openapi.yaml", nil)
	rec := httptest.NewRecorder()

	docs.HandleSpec(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Fatalf("expected Content-Type application/yaml, got %q", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "openapi: 3.0.3") {
		t.Fatalf("expected embedded spec to contain the openapi version header, got: %s", body)
	}
	if !strings.Contains(body, "AI Usage Dashboard API") {
		t.Fatalf("expected embedded spec to contain the API title, got: %s", body)
	}
}
