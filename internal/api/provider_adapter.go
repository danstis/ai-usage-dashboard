package api

import (
	"context"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// NewProviderRepository adapts a *provider.Service (registry ↔ store) to
// the ProviderRepository seam the handlers in this package depend on,
// converting between the domain provider.Provider shape and the wire-format
// Provider type generated from the OpenAPI spec.
func NewProviderRepository(svc *provider.Service) ProviderRepository {
	return providerServiceAdapter{svc: svc}
}

type providerServiceAdapter struct {
	svc *provider.Service
}

func (a providerServiceAdapter) ListProviders(ctx context.Context) ([]Provider, error) {
	providers, err := a.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Provider, 0, len(providers))
	for _, p := range providers {
		out = append(out, toAPIProvider(p))
	}
	return out, nil
}

func (a providerServiceAdapter) GetProvider(ctx context.Context, id string) (Provider, error) {
	p, err := a.svc.Get(ctx, id)
	if err != nil {
		return Provider{}, err
	}
	return toAPIProvider(p), nil
}

func (a providerServiceAdapter) EnableProvider(ctx context.Context, id string) (Provider, error) {
	p, err := a.svc.SetEnabled(ctx, id, true)
	if err != nil {
		return Provider{}, err
	}
	return toAPIProvider(p), nil
}

func (a providerServiceAdapter) DisableProvider(ctx context.Context, id string) (Provider, error) {
	p, err := a.svc.SetEnabled(ctx, id, false)
	if err != nil {
		return Provider{}, err
	}
	return toAPIProvider(p), nil
}

func toAPIProvider(p provider.Provider) Provider {
	fields := make([]CredentialField, 0, len(p.CredentialFields))
	for _, f := range p.CredentialFields {
		fields = append(fields, CredentialField{Name: f.Name, Label: f.Label, Secret: f.Secret})
	}
	return Provider{
		Id:               p.ID,
		Name:             p.Name,
		Enabled:          p.Enabled,
		CredentialFields: fields,
	}
}
