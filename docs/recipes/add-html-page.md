# Add an HTML page (templ)

```templ path=web/layout/layout.templ
package layout

import "github.com/guilhermebr/gox/web"

// Layout is the one shell; pages never write <html> themselves.
func Layout(page *web.Page, body templ.Component) templ.Component { return layout(page, body) }

templ layout(page *web.Page, body templ.Component) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<title>{ page.Title }</title>
			@web.CSRFMeta(page)
			<link rel="stylesheet" href={ page.Asset("css/app.css") }/>
		</head>
		<body>
			for _, f := range page.Flashes {
				<div class={ "flash", "flash-" + f.Kind }>{ f.Message }</div>
			}
			<main>@body</main>
		</body>
	</html>
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
		web.Enable(web.WithStatic(static.FS), web.WithSessions(), web.WithLayout(layout.Layout)),
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
// Package static embeds the assets; web.WithStatic(FS) serves them under /static/.
package static

import "embed"

//go:embed all:css
var FS embed.FS
```

```css path=static/css/app.css
body { font-family: system-ui, sans-serif; max-width: 40rem; margin: 2rem auto; }
```
