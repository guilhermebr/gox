# Add a migration

```sql path=migrations/000001_create_invoices.up.sql
CREATE TABLE IF NOT EXISTS invoices (
    id     TEXT PRIMARY KEY,
    amount INTEGER NOT NULL CHECK (amount > 0)
);
```

```sql path=migrations/000001_create_invoices.down.sql
DROP TABLE IF EXISTS invoices;
```

```sql path=migrations/000002_add_status.up.sql
ALTER TABLE invoices ADD COLUMN status TEXT NOT NULL DEFAULT 'open';
```

```sql path=migrations/000002_add_status.down.sql
ALTER TABLE invoices DROP COLUMN status;
```

```go path=migrations/migrations.go
// Package migrations embeds the SQL files; //go:embed cannot reach outside
// its own directory, so the files live next to this file.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

```go path=cmd/billing/main.go
package main

import (
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"

	"example.com/shop/migrations"
)

func main() {
	// Migrations run at boot, after the pool pings and before /readyz is green.
	// Set BILLING_POSTGRES_MIGRATE=false to skip them and run
	// postgres.Migrate(ctx, url, migrations.FS) from a deploy step instead.
	a := gox.MustNew("billing", gox.HTTP(), postgres.Enable(postgres.WithMigrations(migrations.FS)))
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Moving a service off another framework that already owns `schema_migrations`
(Rails, Django, Laravel): set `BILLING_POSTGRES_MIGRATIONS_TABLE=service_migrations`
and start from a baseline migration dumped from the existing schema.
