# Project layout (the only supported one)

```
cmd/<name>/main.go            # gox.MustNew + route registration + a.Run()
internal/<feature>/handler.go # net/http handlers for one feature
internal/<feature>/views/     # templ components for that feature (HTML apps)
web/layout/*.templ            # layout(s) (HTML apps)
web/components/*.templ        # shared components (HTML apps)
migrations/NNNN_name.up.sql   # golang-migrate files, embedded with //go:embed
static/                       # embedded assets (HTML apps): css/, js/, img/
.env.example                  # every <PREFIX>_* variable with its default
Makefile                      # build, test, lint, generate (templ), run
```

Start order is fixed by stage, not by option order: datastores (StageDatastore) → clients (StageClient) → your components (StageUser) → public HTTP server (StageServer) → admin server. Stop is the reverse. `/readyz` turns green only after every component started; it turns red first on shutdown.
