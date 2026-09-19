# Serve a single-page app from the API's origin

```go path=dist/dist.go
// Package dist embeds the built frontend (the bundler's output directory).
package dist

import "embed"

// FS holds index.html and assets/.
//
//go:embed index.html all:assets
var FS embed.FS
```

```html path=dist/index.html
<!doctype html>
<html><head><title>shop</title></head><body><div id="root"></div><script type="module" src="/assets/app.js"></script></body></html>
```

```js path=dist/assets/app.js
document.getElementById("root").textContent = "shop";
```

```go path=main.go
package main

import (
	"html"
	"net/http"
	"os"

	"github.com/guilhermebr/gox"

	"example.com/shop/dist"
)

func main() {
	a := gox.MustNew("shop", gox.HTTP(gox.WithSPA(gox.SPAConfig{
		FS: dist.FS,
		// Unmatched requests under these are 404 errors, never the shell.
		ServerPrefixes: []string{"/api/", "/webhooks/"},
		// Per-request tags before </head>: locale, public config, a token.
		Head: func(r *http.Request) string {
			return `<meta name="locale" content="` + html.EscapeString(r.Header.Get("Accept-Language")) + `">`
		},
	})))

	a.HandleFunc("GET /api/invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = gox.JSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id")})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Files under `/assets/` are cached forever (bundlers content-hash them; change
`SPAConfig.Immutable` otherwise); everything else, the shell included,
revalidates. Same-origin apps need no CORS and no CSRF token: cross-origin
writes are rejected by default.
