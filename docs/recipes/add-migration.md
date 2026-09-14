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

```go path=main.go
package main

import (
	"embed"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	// Migrations run at boot, after the pool pings and before /readyz is green.
	// Set BILLING_POSTGRES_MIGRATE=false to skip them and run
	// postgres.Migrate(ctx, url, migrations) from a deploy step instead.
	a := gox.MustNew("billing", gox.HTTP(), postgres.Enable(postgres.WithMigrations(migrations)))
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
