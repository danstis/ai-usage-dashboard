// Package migrations embeds the SQL migration files applied to the store's
// SQLite database on boot.
package migrations

import "embed"

// FS embeds every migration file in this directory for use with goose.
//
//go:embed *.sql
var FS embed.FS
