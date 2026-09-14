// Package migrations embeds the SQL migration files. postgres.WithMigrations(FS)
// runs them at boot on a dedicated connection; files are NNNN_name.up.sql
// and NNNN_name.down.sql (golang-migrate layout).
package migrations

import "embed"

// FS holds every migration file next to this package.
//
//go:embed *.sql
var FS embed.FS
