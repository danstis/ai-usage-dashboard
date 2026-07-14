package api

import (
	"context"
	"sort"

	"github.com/danstis/ai-usage-dashboard/internal/credential"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// NewCredentialRepository adapts a *provider.Service (id/field validation
// against the declared registry) and a *credential.Service (sealing,
// persisting, and clearing values) to the CredentialRepository seam the
// credential handlers depend on.
func NewCredentialRepository(providers *provider.Service, credentials *credential.Service) CredentialRepository {
	return credentialServiceAdapter{providers: providers, credentials: credentials}
}

type credentialServiceAdapter struct {
	providers   *provider.Service
	credentials *credential.Service
}

func (a credentialServiceAdapter) SetCredentials(ctx context.Context, providerID string, values map[string]string) error {
	p, err := a.providers.Get(ctx, providerID)
	if err != nil {
		return err
	}
	if err := validateCredentialFields(p.CredentialFields, values); err != nil {
		return err
	}
	return a.credentials.SetValues(ctx, providerID, values)
}

func (a credentialServiceAdapter) GetCredentialPresence(ctx context.Context, providerID string) ([]CredentialPresence, error) {
	p, err := a.providers.Get(ctx, providerID)
	if err != nil {
		return nil, err
	}
	presence, err := a.credentials.Presence(ctx, providerID)
	if err != nil {
		return nil, err
	}
	out := make([]CredentialPresence, 0, len(p.CredentialFields))
	for _, f := range p.CredentialFields {
		out = append(out, CredentialPresence{Name: f.Name, Configured: presence[f.Name]})
	}
	return out, nil
}

func (a credentialServiceAdapter) DeleteCredentials(ctx context.Context, providerID string) error {
	if _, err := a.providers.Get(ctx, providerID); err != nil {
		return err
	}
	return a.credentials.Delete(ctx, providerID)
}

// validateCredentialFields enforces the write-only API's full-replace
// contract: values must supply exactly the provider's declared credential
// fields — no more, no fewer.
func validateCredentialFields(declared []provider.CredentialField, values map[string]string) error {
	declaredNames := make(map[string]bool, len(declared))
	for _, f := range declared {
		declaredNames[f.Name] = true
	}

	var missing, unknown []string
	for name := range declaredNames {
		if _, ok := values[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range values {
		if !declaredNames[name] {
			unknown = append(unknown, name)
		}
	}
	if len(missing) == 0 && len(unknown) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	return &ErrInvalidCredentialFields{Missing: missing, Unknown: unknown}
}
