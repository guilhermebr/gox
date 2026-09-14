# Project layout (the only supported one)

```
cmd/<name>/main.go            # gox.MustNew + <feature>.Register(a) calls + a.Run()
internal/<feature>/handler.go # net/http handlers for one feature; exposes Register(a *gox.App)
internal/<feature>/views/     # templ components for that feature (HTML apps)
web/layout/*.templ            # layout(s) (HTML apps)
web/components/*.templ        # shared components (HTML apps)
migrations/migrations.go      # package migrations: //go:embed *.sql; var FS embed.FS
migrations/NNNN_name.up.sql   # golang-migrate files (and .down.sql)
static/static.go              # package static: //go:embed css js img; var FS embed.FS (HTML apps)
.env.example                  # every <PREFIX>_* variable with its default
Makefile                      # build, test, lint, generate (templ), run
```

`//go:embed` cannot reach outside its own directory, so embedded files live in a package of their own and `main` imports it: `postgres.WithMigrations(migrations.FS)`, `web.WithStatic(static.FS)`.

```go
// migrations/migrations.go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

Start order is fixed by stage, not by option order: datastores (StageDatastore) → clients (StageClient) → your components (StageUser) → public HTTP server (StageServer) → admin server. Stop is the reverse. `/readyz` turns green only after every component started; it turns red first on shutdown.
