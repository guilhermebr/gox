# Canonical mains (copy these)

JSON API, one import:

```go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
)

func main() {
	a := gox.MustNew("hello", gox.HTTP())
	a.HandleFunc("GET /hello/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = gox.JSON(w, http.StatusOK, map[string]string{"hello": r.PathValue("name")})
	})
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

JSON API with Postgres and one migration, two imports:

```go
package main

import (
	"embed"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Config struct {
	gox.BaseConfig
	InvoiceTTL time.Duration `conf:"default:24h"`
}

func main() {
	var cfg Config
	a := gox.MustNew("billing",
		gox.WithConfig(&cfg),
		gox.HTTP(),
		postgres.Enable(postgres.WithMigrations(migrations)),
	)
	db := postgres.From(a)

	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		var id string
		err := db.QueryRow(r.Context(), "SELECT id FROM invoices WHERE id = $1", r.PathValue("id")).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusOK, map[string]string{"id": id})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Server-rendered HTML app (templ), two imports plus templ:

```go
package main

import (
	"embed"
	"io/fs"
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"

	"example.com/shop/views" // templ components: views.Layout, views.Home, views.SignForm
)

//go:embed static
var static embed.FS

type SignInput struct {
	Name string `form:"name,required,min=2,max=40"`
}

func main() {
	assets, _ := fs.Sub(static, "static")
	a := gox.MustNew("shop",
		gox.HTTP(),
		web.Enable(web.WithStatic(assets), web.WithSessions(), web.WithLayout(views.Layout)),
	)
	a.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		web.PageFrom(r).Title = "Home"
		_ = web.Render(w, r, views.Home())
	})
	a.HandleFunc("GET /sign", func(w http.ResponseWriter, r *http.Request) {
		_ = web.Render(w, r, views.SignForm(web.PageFrom(r), SignInput{}, nil))
	})
	a.HandleFunc("POST /sign", func(w http.ResponseWriter, r *http.Request) {
		in, ferrs, err := web.Form[SignInput](r)
		if err != nil {
			web.Error(w, r, err)
			return
		}
		if ferrs != nil {
			_ = web.RenderStatus(w, r, http.StatusUnprocessableEntity, views.SignForm(web.PageFrom(r), in, ferrs))
			return
		}
		web.AddFlash(w, r, "success", "Thanks, "+in.Name)
		web.Redirect(w, r, "/")
	})
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

A templ layout receives the page context and the body:

```templ
package views

import "github.com/guilhermebr/gox/web"

func Layout(page *web.Page, body templ.Component) templ.Component { return layout(page, body) }

templ layout(page *web.Page, body templ.Component) {
	<!DOCTYPE html>
	<html lang="en">
		<head><meta charset="utf-8"/><title>{ page.Title }</title>@web.CSRFMeta(page)<link rel="stylesheet" href={ page.Asset("css/app.css") }/></head>
		<body>
			for _, f := range page.Flashes { <div class={ "flash", "flash-" + f.Kind }>{ f.Message }</div> }
			<main>@body</main>
		</body>
	</html>
}
```

A form component takes the page (for the CSRF field), the input and the field errors:

```templ
templ SignForm(page *web.Page, in SignInput, errs web.FieldErrors) {
	<form method="post" action="/sign">
		@web.CSRFField(page)
		<input name="name" value={ in.Name }/>
		if msg, ok := errs["name"]; ok { <span class="field-error">{ msg }</span> }
		<button type="submit">Sign</button>
	</form>
}
```
