// Package migrations embeds the example's SQL migrations, NNNNNN_name.up.sql
// and NNNNNN_name.down.sql (golang-migrate layout), for
// postgres.WithMigrations.
package migrations

import "embed"

// FS holds every migration file next to this package.
//
//go:embed *.sql
var FS embed.FS
