// Package docs serves an embedded Swagger UI at /docs so developers can
// exercise the /api/v1 HTTP surface directly from a browser, backed by the
// same api/openapi.yaml contract used for codegen (make generate) and
// spec-lint (make spec-lint).
package docs

import (
	_ "embed"
	"net/http"

	openapi "github.com/danstis/ai-usage-dashboard/api"
)

//go:embed ui.html
var uiHTML []byte

// HandleUI serves the Swagger UI page at GET /docs. The page loads the
// swagger-ui-dist assets from a pinned CDN version and points them at
// HandleSpec (GET /docs/openapi.yaml) to render the document.
func HandleUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(uiHTML)
}

// HandleSpec serves the raw OpenAPI document backing the Swagger UI, at
// GET /docs/openapi.yaml, embedded from api/openapi.yaml at build time.
func HandleSpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(openapi.Spec)
}
