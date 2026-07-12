// Command spec-lint validates the committed OpenAPI document, failing the
// build if the contract is malformed.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/danstis/ai-usage-dashboard/internal/specvalidate"
)

const defaultSpecPath = "api/openapi.yaml"

func main() {
	path := defaultSpecPath
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	if err := specvalidate.Validate(context.Background(), path); err != nil {
		fmt.Fprintf(os.Stderr, "spec-lint: %s: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Printf("spec-lint: %s: OK\n", path)
}
