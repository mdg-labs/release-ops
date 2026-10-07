// Package migrations embeds the sqlite-migrate files generated from db/schema.sql.
package migrations

import "embed"

// FS holds the generated migration files at its root.
//
//go:embed *.sql
var FS embed.FS
