package credential

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// fakeRepository is an in-memory store.CredentialRepository test double,
// keyed by providerID then field.
type fakeRepository struct {
	values map[string]map[string][]byte
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{values: map[string]map[string][]byte{}}
}

func (f *fakeRepository) Upsert(_ context.Context, providerID, field string, ciphertext []byte) error {
	if f.values[providerID] == nil {
		f.values[providerID] = map[string][]byte{}
	}
	f.values[providerID][field] = ciphertext
	return nil
}

func (f *fakeRepository) Presence(_ context.Context, providerID string) (map[string]bool, error) {
	presence := map[string]bool{}
	for field := range f.values[providerID] {
		presence[field] = true
	}
	return presence, nil
}

func (f *fakeRepository) GetSecret(_ context.Context, providerID, field string) ([]byte, error) {
	blob, ok := f.values[providerID][field]
	if !ok {
		return nil, store.ErrNotFound
	}
	return blob, nil
}

func (f *fakeRepository) Delete(_ context.Context, providerID string) error {
	delete(f.values, providerID)
	return nil
}

func validKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestService_SetValuesThenReveal_RoundTrips(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	svc := NewService(repo, validKey())

	if err := svc.SetValues(ctx, "openai", map[string]string{"api_key": "sk-super-secret"}); err != nil {
		t.Fatalf("SetValues() returned error: %v", err)
	}

	got, err := svc.Reveal(ctx, "openai", "api_key")
	if err != nil {
		t.Fatalf("Reveal() returned error: %v", err)
	}
	if got != "sk-super-secret" {
		t.Fatalf("Reveal() = %q, want %q", got, "sk-super-secret")
	}
}

func TestService_SetValues_StoredCiphertextNeverContainsPlaintext(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	svc := NewService(repo, validKey())

	plaintext := "sk-super-secret-plaintext-value"
	if err := svc.SetValues(ctx, "openai", map[string]string{"api_key": plaintext}); err != nil {
		t.Fatalf("SetValues() returned error: %v", err)
	}

	blob, err := repo.GetSecret(ctx, "openai", "api_key")
	if err != nil {
		t.Fatalf("GetSecret() returned error: %v", err)
	}
	if bytes.Contains(blob, []byte(plaintext)) {
		t.Fatalf("stored ciphertext contains the raw plaintext value: %x", blob)
	}
}

func TestService_SetValues_TwoFieldsAreIndependentlySealed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	svc := NewService(repo, validKey())

	if err := svc.SetValues(ctx, "openai", map[string]string{
		"api_key": "key-value",
		"org_id":  "org-value",
	}); err != nil {
		t.Fatalf("SetValues() returned error: %v", err)
	}

	gotKey, err := svc.Reveal(ctx, "openai", "api_key")
	if err != nil {
		t.Fatalf("Reveal(api_key) returned error: %v", err)
	}
	if gotKey != "key-value" {
		t.Fatalf("Reveal(api_key) = %q, want %q", gotKey, "key-value")
	}

	gotOrg, err := svc.Reveal(ctx, "openai", "org_id")
	if err != nil {
		t.Fatalf("Reveal(org_id) returned error: %v", err)
	}
	if gotOrg != "org-value" {
		t.Fatalf("Reveal(org_id) = %q, want %q", gotOrg, "org-value")
	}
}

func TestService_Reveal_WrongProviderFailsAADBinding(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	svc := NewService(repo, validKey())

	if err := svc.SetValues(ctx, "openai", map[string]string{"api_key": "secret"}); err != nil {
		t.Fatalf("SetValues() returned error: %v", err)
	}

	// Copy the openai/api_key ciphertext under a different provider id to
	// simulate a row swapped between providers; the AAD binding must reject
	// decrypting it under the wrong id.
	blob, err := repo.GetSecret(ctx, "openai", "api_key")
	if err != nil {
		t.Fatalf("GetSecret() returned error: %v", err)
	}
	if err := repo.Upsert(ctx, "anthropic", "api_key", blob); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}

	if _, err := svc.Reveal(ctx, "anthropic", "api_key"); err == nil {
		t.Fatal("Reveal() expected error for a ciphertext sealed under a different provider id, got nil")
	}
}

func TestService_Presence(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	svc := NewService(repo, validKey())

	if err := svc.SetValues(ctx, "openai", map[string]string{"api_key": "v"}); err != nil {
		t.Fatalf("SetValues() returned error: %v", err)
	}

	presence, err := svc.Presence(ctx, "openai")
	if err != nil {
		t.Fatalf("Presence() returned error: %v", err)
	}
	if !presence["api_key"] {
		t.Fatalf("expected api_key present, got: %+v", presence)
	}
}

func TestService_Delete(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	svc := NewService(repo, validKey())

	if err := svc.SetValues(ctx, "openai", map[string]string{"api_key": "v"}); err != nil {
		t.Fatalf("SetValues() returned error: %v", err)
	}
	if err := svc.Delete(ctx, "openai"); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	if _, err := svc.Reveal(ctx, "openai", "api_key"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got: %v", err)
	}
}
