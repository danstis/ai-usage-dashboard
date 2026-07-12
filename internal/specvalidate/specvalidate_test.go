package specvalidate_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/specvalidate"
)

func TestValidate_CommittedSpec(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "api", "openapi.yaml")
	if err := specvalidate.Validate(context.Background(), path); err != nil {
		t.Fatalf("expected committed spec %q to validate cleanly, got: %v", path, err)
	}
}

func TestValidate_BrokenSpec(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "broken.yaml")
	if err := specvalidate.Validate(context.Background(), path); err == nil {
		t.Fatalf("expected broken spec %q to fail validation", path)
	}
}
