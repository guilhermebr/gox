# Canonical mains (copy these)

The Go and templ blocks below are files from `examples/`, which CI builds, with the example's own packages imported as `example.com/<service>/...`.

JSON API, one import (`examples/http`):

```go
package main

import (
	"net/http"
	"os"
	"sync"

	"github.com/guilhermebr/gox"
)

type invoice struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Amount   int    `json:"amount"`
}

// store is the example's in-memory database. Handlers run concurrently, so
// every access holds the lock.
type store struct {
	mu       sync.Mutex
	invoices map[string]invoice
}

func (s *store) get(id string) (invoice, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invoices[id]
	return inv, ok
}

func (s *store) put(inv invoice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoices[inv.ID] = inv
}

func main() {
	a := gox.MustNew("billing", gox.HTTP())
	db := &store{invoices: map[string]invoice{
		"inv_1": {ID: "inv_1", Customer: "ana", Amount: 1250},
	}}

	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		inv, ok := db.get(r.PathValue("id"))
		if !ok {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		_ = gox.JSON(w, http.StatusOK, inv)
	})

	a.HandleFunc("POST /invoices", func(w http.ResponseWriter, r *http.Request) {
		var in invoice
		if err := gox.Decode(r, &in); err != nil {
			gox.Error(w, r, err)
			return
		}
		if in.Amount <= 0 {
			gox.Error(w, r, gox.InvalidArgument("amount must be positive").WithDetail("field", "amount"))
			return
		}
		in.ID = "inv_" + in.Customer
		db.put(in)
		_ = gox.JSON(w, http.StatusCreated, in)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

JSON API with Postgres and one migration (`examples/postgres`), two gox imports plus pgx. A duplicate id is a 409, other database errors a 500:

```go
package main

import (
	"errors"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"

	"example.com/billing/migrations" // package migrations: //go:embed *.sql; var FS embed.FS
)

type invoice struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Amount   int    `json:"amount"`
}

func main() {
	a := gox.MustNew("billing",
		gox.HTTP(),
		postgres.Enable(postgres.WithMigrations(migrations.FS)),
	)
	db := postgres.From(a)

	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		var inv invoice
		err := db.QueryRow(r.Context(), "SELECT id, customer, amount FROM invoices WHERE id = $1", r.PathValue("id")).
			Scan(&inv.ID, &inv.Customer, &inv.Amount)
		if errors.Is(err, pgx.ErrNoRows) {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusOK, inv)
	})

	a.HandleFunc("POST /invoices", func(w http.ResponseWriter, r *http.Request) {
		var inv invoice
		if err := gox.Decode(r, &inv); err != nil {
			gox.Error(w, r, err)
			return
		}
		if inv.Amount <= 0 {
			gox.Error(w, r, gox.InvalidArgument("amount must be positive").WithDetail("field", "amount"))
			return
		}
		err := postgres.Tx(r.Context(), db, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(),
				"INSERT INTO invoices (id, customer, amount) VALUES ($1, $2, $3)", inv.ID, inv.Customer, inv.Amount)
			return err
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			gox.Error(w, r, gox.AlreadyExists("invoice %s", inv.ID))
			return
		}
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusCreated, inv)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Server-rendered HTML app on templ (`examples/web`), two gox imports plus templ. The form input struct lives in the views package so both `main` and the templ component can use it. `go mod tidy` adds `github.com/a-h/templ` at `gox/web`'s version; do not `go get` it on its own, which upgrades it past the version `make generate` runs. After every `.templ` edit, regenerate the `*_templ.go` files with `make generate` (in a `gox new -web` service it runs the templ CLI through `go run`, pinned to `gox/web`'s templ version); never install templ globally or edit `*_templ.go` by hand. Set `<PREFIX>_WEB_SESSION_SECRET` (32+ bytes) in production:

```go
package main

import (
	"net/http"
	"os"
	"sync"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"

	"example.com/shop/internal/guestbook/views" // templ components: views.Home, views.SignForm, views.SignInput
	"example.com/shop/static"                   // package static: //go:embed all:css; var FS embed.FS
	"example.com/shop/web/layout"               // templ layout: layout.Layout, layout.ErrorPage
)

// guestbook is the example's whole "domain": names people signed with.
// Handlers run concurrently, so every access holds the lock.
type guestbook struct {
	mu    sync.Mutex
	names []string
}

func (g *guestbook) add(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.names = append(g.names, name)
}

func (g *guestbook) all() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.names...)
}

func main() {
	a := gox.MustNew("shop",
		gox.HTTP(),
		web.Enable(
			web.WithStatic(static.FS),
			web.WithSessions(),
			web.WithLayout(layout.Layout),
			web.WithErrorPage(layout.ErrorPage),
		),
	)
	book := &guestbook{}

	a.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		web.PageFrom(r).Title = "Guestbook"
		_ = web.Render(w, r, views.Home(book.all()))
	})

	a.HandleFunc("GET /sign", func(w http.ResponseWriter, r *http.Request) {
		web.PageFrom(r).Title = "Sign the guestbook"
		_ = web.Render(w, r, views.SignForm(web.PageFrom(r), views.SignInput{}, nil))
	})

	// The one form pattern: decode, re-render with 422 on field errors,
	// flash and redirect on success.
	a.HandleFunc("POST /sign", func(w http.ResponseWriter, r *http.Request) {
		in, ferrs, err := web.Form[views.SignInput](r)
		if err != nil {
			gox.Error(w, r, err) // the error page for a browser, the JSON envelope otherwise
			return
		}
		if ferrs != nil {
			web.PageFrom(r).Title = "Sign the guestbook"
			_ = web.RenderStatus(w, r, http.StatusUnprocessableEntity, views.SignForm(web.PageFrom(r), in, ferrs))
			return
		}
		book.add(in.Name)
		web.AddFlash(w, r, "success", "Thanks for signing, "+in.Name+"!")
		web.Redirect(w, r, "/")
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

In `.templ` files, `if`, `for` and `switch` end their line with `{` and the body goes on the lines below; a one-line block does not parse.

The layout (`web/layout/layout.templ`) receives the page context and the body. The error page passed with `web.WithErrorPage` is a body fragment that gox wraps in the layout; it sets the title in Go, since the layout prints it before the body renders:

```templ
// Package layout is the page shell every page renders in.
package layout

import (
	"fmt"

	"github.com/guilhermebr/gox/web"
)

// Layout is the one shell every page renders in. It takes the request's
// Page and the body; pages never build <html> themselves.
func Layout(page *web.Page, body templ.Component) templ.Component {
	return shell(page, body)
}

templ shell(page *web.Page, body templ.Component) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>{ page.Title }</title>
			@web.CSRFMeta(page)
			<link rel="stylesheet" href={ page.Asset("css/app.css") }/>
		</head>
		<body>
			<header>
				<a href="/">Guestbook</a>
				<nav><a href="/sign">Sign</a></nav>
			</header>
			for _, f := range page.Flashes {
				<div class={ "flash", "flash-" + f.Kind } role="status">{ f.Message }</div>
			}
			<main>
				@body
			</main>
		</body>
	</html>
}

// ErrorPage renders framework and handler errors for browser requests. It
// returns a body fragment; gox wraps it in Layout.
func ErrorPage(page *web.Page, status int, code, message string) templ.Component {
	page.Title = fmt.Sprint(status)
	return errorBody(status, message)
}

templ errorBody(status int, message string) {
	<section class="error">
		<h1>{ fmt.Sprint(status) }</h1>
		<p>{ message }</p>
		<p><a href="/">Back to the guestbook</a></p>
	</section>
}
```

This layout loads no htmx. With htmx and `web.WithSessions()`, load htmx in the head and, at the end of `<body>`, add an `htmx:configRequest` listener that sets `e.detail.headers['X-CSRF-Token']` from `meta[name=csrf-token]`, as the `gox new -web` layout does; otherwise an `hx-post` outside a form with `web.CSRFField` gets a 403.

A form component (`internal/guestbook/views/sign.templ`) declares the input struct and takes the page (for the CSRF field), the input and the field errors keyed by form field name:

```templ
package views

import "github.com/guilhermebr/gox/web"

// SignInput is what the form decodes into; the tags drive validation.
type SignInput struct {
	Name  string `form:"name,required,min=2,max=40"`
	Agree bool   `form:"agree,required"`
}

// SignForm renders the form, with errors keyed by field when re-rendered
// after a failed POST.
templ SignForm(page *web.Page, in SignInput, errs web.FieldErrors) {
	<h1>Sign the guestbook</h1>
	<form method="post" action="/sign">
		@web.CSRFField(page)
		<label>
			Your name
			<input type="text" name="name" value={ in.Name } autofocus/>
			if msg, ok := errs["name"]; ok {
				<span class="field-error">{ msg }</span>
			}
		</label>
		<label>
			<input type="checkbox" name="agree" checked?={ in.Agree }/>
			I am a real person
			if msg, ok := errs["agree"]; ok {
				<span class="field-error">{ msg }</span>
			}
		</label>
		<button type="submit">Sign</button>
	</form>
}
```
