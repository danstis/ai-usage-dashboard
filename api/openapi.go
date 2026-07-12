// Package openapi embeds the committed OpenAPI document (openapi.yaml) so it
// can be served at runtime — e.g. by the Swagger UI docs route in
// internal/docs — without depending on a filesystem path that may differ
// between local runs and the container image. api/openapi.yaml remains the
// single source of truth validated by make spec-lint and used by make
// generate; this file only exposes its bytes to the rest of the module.
package openapi

import _ "embed"

// Spec holds the raw bytes of the committed api/openapi.yaml document.
//
//go:embed openapi.yaml
var Spec []byte
