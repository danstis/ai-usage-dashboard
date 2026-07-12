// Command spec-lint validates the committed OpenAPI document, failing the
// build if the contract is malformed.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/danstis/ai-usage-dashboard/internal/specvalidate"
)

const defaultSpecPath = "api/openapi.yaml"

func main() {
	if err := run(os.Args, os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}

// run is the testable body of main(): it picks the spec path from args,
// validates the spec, and writes a status line to stdout/stderr. It returns
// the validation error (if any) so tests can assert on it without invoking
// main()'s os.Exit branch.
func run(args []string, stdout, stderr io.Writer) error {
	path := defaultSpecPath
	if len(args) > 1 {
		path = args[1]
	}

	if err := specvalidate.Validate(context.Background(), path); err != nil {
		_, _ = fmt.Fprintf(stderr, "spec-lint: %s: %v\n", path, err)
		return err
	}

	_, _ = fmt.Fprintf(stdout, "spec-lint: %s: OK\n", path)
	return nil
}
