# Add an HTML page (templ)

A service made by `gox new -web` already has the layout, `static/` and the
`web.Enable(...)` options below: add only the view and the handler. The layout
and `static/static.go` here are the files that scaffold generates.

```templ path=web/layout/layout.templ
// Package layout is the page shell every page renders in.
package layout

import (
	"fmt"

	"github.com/guilhermebr/gox/web"
)

// Layout wraps a page body; web.Render passes the request's Page.
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
			<script src={ page.Asset("js/htmx.min.js") }></script>
			<script defer src={ page.Asset("js/alpine.min.js") }></script>
		</head>
		<body>
			<header>
				<a href="/">Home</a>
			</header>
			for _, f := range page.Flashes {
				<div class={ "flash", "flash-" + f.Kind } role="status">{ f.Message }</div>
			}
			<main>
				@body
			</main>
			<script>
				// Send the CSRF token with every htmx request.
				document.body.addEventListener('htmx:configRequest', function (e) {
					var m = document.querySelector('meta[name=csrf-token]');
					if (m) { e.detail.headers['X-CSRF-Token'] = m.content; }
				});
			</script>
		</body>
	</html>
}

// ErrorPage renders framework and handler errors for browser requests. It
// returns a body fragment; gox wraps it in Layout.
func ErrorPage(page *web.Page, status int, code, message string) templ.Component {
	page.Title = fmt.Sprintf("%d", status)
	return errorBody(status, message)
}

templ errorBody(status int, message string) {
	<section class="error">
		<h1>{ fmt.Sprint(status) }</h1>
		<p>{ message }</p>
		<p><a href="/">Back home</a></p>
	</section>
}
```

```templ path=internal/invoices/views/invoices.templ
package views

// Pages take explicit typed parameters.
templ Invoices(ids []string) {
	<h1>Invoices</h1>
	<ul>
		for _, id := range ids {
			<li><a href={ templ.SafeURL("/invoices/" + id) }>{ id }</a></li>
		}
	</ul>
}
```

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"

	"example.com/shop/internal/invoices/views"
	"example.com/shop/static"
	"example.com/shop/web/layout"
)

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

	a.HandleFunc("GET /invoices", func(w http.ResponseWriter, r *http.Request) {
		web.PageFrom(r).Title = "Invoices"
		_ = web.Render(w, r, views.Invoices([]string{"inv_1", "inv_2"}))
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

```go path=static/static.go
// Package static embeds the site's assets. web.WithStatic(FS) serves them
// under /static/ with content-hashed URLs in production; templates resolve
// names with page.Asset("css/app.css").
package static

import "embed"

// FS holds css/ and js/ (run `make assets` to vendor htmx and Alpine.js). A
// new directory (img/) is served only once it is added to the line below.
//
//go:embed all:css all:js
var FS embed.FS
```

```text path=static/js/README.md
# Run `make assets` to vendor htmx and Alpine.js here; the layout references both.
```

```css path=static/css/app.css
body { font-family: system-ui, sans-serif; max-width: 40rem; margin: 2rem auto; }
```
