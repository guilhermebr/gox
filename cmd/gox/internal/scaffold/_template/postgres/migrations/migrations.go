// Package migrations embeds the SQL migration files. postgres.WithMigrations(FS)
// runs them at boot on a dedicated connection; files are NNNNNN_name.up.sql
// (golang-migrate layout). Only up migrations run; a NNNNNN_name.down.sql is
// for resetting a local database by hand.
package migrations

import "embed"

// FS holds every migration file next to this package.
//
//go:embed *.sql
var FS embed.FS
