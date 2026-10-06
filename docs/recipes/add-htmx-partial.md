# Add an htmx partial

```templ path=internal/search/views/search.templ
package views

// Search renders the page; the results list is a partial swapped by htmx.
templ Search() {
	<h1>Search</h1>
	<input
		name="q"
		hx-get="/search/results"
		hx-trigger="keyup changed delay:300ms"
		hx-target="#results"
		hx-swap="innerHTML"
		placeholder="Type to search"
	/>
	<div id="results">@Results(nil)</div>
}

// Results is the partial: same package, typed params, no layout.
templ Results(items []string) {
	if len(items) == 0 {
		<p>No results.</p>
	} else {
		<ul>
			for _, it := range items {
				<li>{ it }</li>
			}
		</ul>
	}
}
```

```go path=main.go
package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"

	"example.com/shop/internal/search/views"
)

var catalog = []string{"apple", "apricot", "banana"}

func main() {
	// web.Enable() alone uses the default layout, which loads no htmx: keep the
	// web.Enable options of add-html-page.md (what gox new -web generates).
	a := gox.MustNew("shop", gox.HTTP(), web.Enable())

	a.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		web.PageFrom(r).Title = "Search"
		_ = web.Render(w, r, views.Search()) // in the layout; alone for an htmx fragment request (not hx-boost)
	})

	a.HandleFunc("GET /search/results", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("q"))
		var items []string
		for _, it := range catalog {
			if q != "" && strings.Contains(it, q) {
				items = append(items, it)
			}
		}
		_ = web.Partial(w, r, views.Results(items)) // never wraps the layout
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

htmx comes from the scaffold's layout (`web/layout/layout.templ`, shown in
add-html-page.md): it loads `static/js/htmx.min.js`, which `make assets`
vendors once, and sends the CSRF token as `X-CSRF-Token` on every htmx request.
