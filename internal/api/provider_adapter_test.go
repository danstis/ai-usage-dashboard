package api

import (
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

func TestAdapter_LiveMaps(t *testing.T) {
	t.Parallel()

	live := toAPIProvider(provider.Provider{
		Metadata: provider.Metadata{ID: "fake-provider", Name: "Fake Provider"},
		Enabled:  true,
		Live:     true,
	})
	if !live.Live {
		t.Errorf("expected toAPIProvider to map Live=true, got %+v", live)
	}

	scaffolded := toAPIProvider(provider.Provider{
		Metadata: provider.Metadata{ID: "fake-provider-2", Name: "Fake Provider 2"},
		Enabled:  false,
		Live:     false,
	})
	if scaffolded.Live {
		t.Errorf("expected toAPIProvider to map Live=false, got %+v", scaffolded)
	}
}
