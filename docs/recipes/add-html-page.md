# Add an HTML page (templ)

```templ path=views/layout.templ
package views

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

```templ path=views/invoices.templ
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
	"embed"
	"io/fs"
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"

	"example.com/shop/views"
)

//go:embed static
var static embed.FS

func main() {
	assets, _ := fs.Sub(static, "static")
	a := gox.MustNew("shop",
		gox.HTTP(),
		web.Enable(web.WithStatic(assets), web.WithSessions(), web.WithLayout(views.Layout)),
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

```css path=static/css/app.css
body { font-family: system-ui, sans-serif; max-width: 40rem; margin: 2rem auto; }
```
