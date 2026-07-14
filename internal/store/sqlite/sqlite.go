// Package sqlite provides a pure-Go, CGO-free SQLite implementation of the
// repository interfaces defined in internal/store. The concrete type is
// unexported; callers only ever see the store.Store interface returned by
// New, so no sqlite-specific type leaks out of this package.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" database/sql driver

	"github.com/danstis/ai-usage-dashboard/internal/store"
	"github.com/danstis/ai-usage-dashboard/internal/store/migrations"
)

const driverName = "sqlite"

// sqliteStore is the sqlite-backed implementation of store.Store, backing
// every repository (providers, credentials, ...) behind one connection.
type sqliteStore struct {
	db *sql.DB
}

var _ store.Store = (*sqliteStore)(nil)

// New opens (creating if necessary) the SQLite database at dbPath, applies
// any pending migrations, and returns a ready-to-use store.Store. The
// parent directory of dbPath is created if it does not already exist.
// Callers must Close the returned store when done with it.
func New(ctx context.Context, dbPath string) (store.Store, error) {
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("sqlite: create db directory %s: %w", dir, err)
		}
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_time_format=sqlite",
		dbPath,
	)
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", dbPath, err)
	}
	// modernc.org/sqlite serializes writers; a single pooled connection
	// avoids spurious "database is locked" errors under concurrent access.
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: open %s: %w", dbPath, err)
	}

	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &sqliteStore{db: db}, nil
}

// migrate applies every pending goose migration embedded in the migrations
// package. It is idempotent: goose tracks applied versions in the database
// itself, so re-running it against an already-migrated file is a no-op.
func migrate(ctx context.Context, db *sql.DB) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("sqlite: init migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("sqlite: apply migrations: %w", err)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("sqlite: close: %w", err)
	}
	return nil
}

func (s *sqliteStore) List(ctx context.Context) ([]store.Provider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, enabled, created_at, updated_at FROM providers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list providers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var providers []store.Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: list providers: scan row: %w", err)
		}
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list providers: %w", err)
	}
	return providers, nil
}

func (s *sqliteStore) Get(ctx context.Context, id string) (store.Provider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, enabled, created_at, updated_at FROM providers WHERE id = ?`, id)
	p, err := scanProvider(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Provider{}, store.ErrNotFound
	}
	if err != nil {
		return store.Provider{}, fmt.Errorf("sqlite: get provider %s: %w", id, err)
	}
	return p, nil
}

func (s *sqliteStore) Create(ctx context.Context, id string, enabled bool) (store.Provider, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO providers (id, enabled, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		id, enabled, now, now,
	)
	if err != nil {
		return store.Provider{}, fmt.Errorf("sqlite: create provider %s: %w", id, err)
	}
	return store.Provider{ID: id, Enabled: enabled, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *sqliteStore) SetEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE providers SET enabled = ?, updated_at = ? WHERE id = ?`,
		enabled, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: set enabled for %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: set enabled for %s: %w", id, err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting
// scanProvider serve Get (single row) and List (row iteration) alike.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanProvider(row rowScanner) (store.Provider, error) {
	var p store.Provider
	if err := row.Scan(&p.ID, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return store.Provider{}, err
	}
	return p, nil
}
