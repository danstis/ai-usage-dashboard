package scheduler

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/credential"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/providertest"
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

func setupNoCredsProviderFetcher(t *testing.T, stack testStack, metrics []provider.UsageMetric) *providertest.Fetcher {
	t.Helper()

	if _, err := stack.providers.SetEnabled(context.Background(), "no-creds-provider", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	fetcher := providertest.NewFetcher(provider.Metadata{ID: "no-creds-provider"}, metrics)
	stack.providers.RegisterFetcher(fetcher)
	return fetcher
}

func setupTwoProviderScenario(t *testing.T, stack testStack, noCredsFetcher, fakeProviderFetcher *providertest.Fetcher) {
	t.Helper()

	ctx := context.Background()
	if _, err := stack.providers.SetEnabled(ctx, "no-creds-provider", true); err != nil {
		t.Fatalf("enable no-creds-provider: %v", err)
	}
	if _, err := stack.providers.SetEnabled(ctx, "fake-provider", true); err != nil {
		t.Fatalf("enable fake-provider: %v", err)
	}
	if err := stack.credentials.SetValues(ctx, "fake-provider", map[string]string{"api_key": "k"}); err != nil {
		t.Fatalf("set credentials: %v", err)
	}
	if noCredsFetcher != nil {
		stack.providers.RegisterFetcher(noCredsFetcher)
	}
	if fakeProviderFetcher != nil {
		stack.providers.RegisterFetcher(fakeProviderFetcher)
	}
}
