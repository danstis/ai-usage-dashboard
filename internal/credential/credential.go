// Package credential wires the AES-256-GCM crypto core (internal/secret) to
// a store.CredentialRepository so callers write and read credential values
// without ever handling ciphertext directly. Reveal decrypts a stored
// value — it exists only for the future fetch/scheduler path (P2/S5) and
// must never be wired into an HTTP read path, which would violate the
// write-only guarantee the credential API exists to provide.
package credential

import (
	"context"
	"fmt"

	"github.com/danstis/ai-usage-dashboard/internal/secret"
	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// CooldownClearer lets Service clear a provider's scheduler auth-failure
// backoff after a successful credential set, without importing the
// scheduler package. Satisfied by *scheduler.AuthCooldownRegistry.
type CooldownClearer interface {
	Clear(providerID string)
}

// Service seals credential values with internal/secret before persisting
// them via a store.CredentialRepository, and is the only caller of
// secret.Open in the codebase.
type Service struct {
	repo      store.CredentialRepository
	masterKey []byte
	cooldown  CooldownClearer
}

// NewService builds a Service backed by repo, sealing/opening values with
// masterKey. masterKey must be exactly secret.KeySize bytes —
// config.masterKey is already validated to this length at boot. cooldown is
// optional (nil-safe) — pass nil in tests or callers that don't wire the
// scheduler's auth-failure backoff.
func NewService(repo store.CredentialRepository, masterKey []byte, cooldown CooldownClearer) *Service {
	return &Service{repo: repo, masterKey: masterKey, cooldown: cooldown}
}

// SetValues seals and upserts every field in values for providerID. Each
// field's ciphertext is bound to (providerID, field) via secret.AAD, so a
// stored value can never be swapped between fields or providers. On success
// it clears providerID's auth-failure cooldown (if one is wired) so the
// scheduler's next tick retries immediately instead of waiting out the
// remaining backoff window.
func (s *Service) SetValues(ctx context.Context, providerID string, values map[string]string) error {
	for field, value := range values {
		blob, err := secret.Seal(s.masterKey, []byte(value), secret.AAD(providerID, field))
		if err != nil {
			return fmt.Errorf("credential: seal %s/%s: %w", providerID, field, err)
		}
		if err := s.repo.Upsert(ctx, providerID, field, blob); err != nil {
			return fmt.Errorf("credential: upsert %s/%s: %w", providerID, field, err)
		}
	}
	if s.cooldown != nil {
		s.cooldown.Clear(providerID)
	}
	return nil
}

// Presence returns which fields currently have a stored value for
// providerID.
func (s *Service) Presence(ctx context.Context, providerID string) (map[string]bool, error) {
	presence, err := s.repo.Presence(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("credential: presence %s: %w", providerID, err)
	}
	return presence, nil
}

// Delete clears every stored credential value for providerID.
func (s *Service) Delete(ctx context.Context, providerID string) error {
	if err := s.repo.Delete(ctx, providerID); err != nil {
		return fmt.Errorf("credential: delete %s: %w", providerID, err)
	}
	return nil
}

// Reveal decrypts and returns the plaintext value of field for providerID.
// Callers on the fetch/scheduler path only (P2/S5) — never wire this into
// an HTTP read path.
func (s *Service) Reveal(ctx context.Context, providerID, field string) (string, error) {
	blob, err := s.repo.GetSecret(ctx, providerID, field)
	if err != nil {
		return "", fmt.Errorf("credential: get secret %s/%s: %w", providerID, field, err)
	}
	plaintext, err := secret.Open(s.masterKey, blob, secret.AAD(providerID, field))
	if err != nil {
		return "", fmt.Errorf("credential: open %s/%s: %w", providerID, field, err)
	}
	return string(plaintext), nil
}
