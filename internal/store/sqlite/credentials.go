package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// Upsert stores ciphertext for (providerID, field), creating the row if it
// doesn't exist yet or replacing the value (and updated_at) if it does.
func (s *sqliteStore) Upsert(ctx context.Context, providerID, field string, ciphertext []byte) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO credentials (provider_id, field, ciphertext, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (provider_id, field) DO UPDATE SET ciphertext = excluded.ciphertext, updated_at = excluded.updated_at`,
		providerID, field, ciphertext, now, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: upsert credential %s/%s: %w", providerID, field, err)
	}
	return nil
}

// Presence returns which fields currently have a stored value for
// providerID, keyed by field name.
func (s *sqliteStore) Presence(ctx context.Context, providerID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT field FROM credentials WHERE provider_id = ?`, providerID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: presence %s: %w", providerID, err)
	}
	defer func() { _ = rows.Close() }()

	presence := map[string]bool{}
	for rows.Next() {
		var field string
		if err := rows.Scan(&field); err != nil {
			return nil, fmt.Errorf("sqlite: presence %s: scan row: %w", providerID, err)
		}
		presence[field] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: presence %s: %w", providerID, err)
	}
	return presence, nil
}

// GetSecret returns the raw ciphertext blob stored for (providerID, field),
// or store.ErrNotFound if no value is stored for that field.
func (s *sqliteStore) GetSecret(ctx context.Context, providerID, field string) ([]byte, error) {
	var ciphertext []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT ciphertext FROM credentials WHERE provider_id = ? AND field = ?`, providerID, field,
	).Scan(&ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get secret %s/%s: %w", providerID, field, err)
	}
	return ciphertext, nil
}

// Delete clears every stored value for providerID. It is idempotent —
// deleting a provider with no stored credentials succeeds.
func (s *sqliteStore) Delete(ctx context.Context, providerID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE provider_id = ?`, providerID); err != nil {
		return fmt.Errorf("sqlite: delete credentials %s: %w", providerID, err)
	}
	return nil
}
