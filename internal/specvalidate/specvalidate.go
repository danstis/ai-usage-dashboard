// Package specvalidate validates the committed OpenAPI document so a broken
// contract fails the build instead of silently drifting from the API.
package specvalidate

import (
	"context"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
)

// Validate loads the OpenAPI 3 document at path and validates it, including
// schema and reference resolution.
func Validate(ctx context.Context, path string) error {
	loader := openapi3.NewLoader()

	doc, err := loader.LoadFromFile(path)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	if err := doc.Validate(ctx); err != nil {
		return fmt.Errorf("validate spec: %w", err)
	}

	return nil
}
