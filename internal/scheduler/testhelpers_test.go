package scheduler

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/credential"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/store"
	"github.com/danstis/ai-usage-dashboard/internal/store/sqlite"
)

// testRegistry is a small fixture registry, independent of the production
// provider.Registry, covering both a credentialed and an uncredentialed
// provider.
var testRegistry = []provider.Metadata{
	{
		ID:   "fake-provider",
		Name: "Fake Provider",
		CredentialFields: []provider.CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	},
	{ID: "no-creds-provider", Name: "No Creds Provider"},
}

// testStack bundles the real, sqlite-backed services a Collector/Scheduler
// needs so tests exercise the same wiring as production.
type testStack struct {
	db          store.Store
	providers   *provider.Service
	credentials *credential.Service
}

// newTestStack opens a temp sqlite store, reconciles testRegistry into it,
// and returns the services layered on top. t.Cleanup closes the store.
func newTestStack(t *testing.T) testStack {
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

	providerSvc := provider.NewService(db, testRegistry)
	if err := providerSvc.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	key := make([]byte, 32)
	credentialSvc := credential.NewService(db, key)

	return testStack{db: db, providers: providerSvc, credentials: credentialSvc}
}
