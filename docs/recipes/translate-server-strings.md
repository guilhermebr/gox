# Translate the strings the server owns

```go path=locales/locales.go
// Package locales embeds the catalogs: one JSON file per locale.
package locales

import "embed"

// FS holds en.json, es.json and so on.
//
//go:embed *.json
var FS embed.FS
```

```json path=locales/en.json
{"receipt": {"subject": "Your receipt, {name}"}, "errors": {"not_found": "We could not find that invoice"}}
```

```json path=locales/es.json
{"receipt": {"subject": "Tu recibo, {name}"}}
```

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/i18n"

	"example.com/shop/locales"
)

func main() {
	bundle, err := i18n.Load(locales.FS, "en") // en is the fallback locale and must exist
	if err != nil {
		os.Exit(1)
	}
	// The locale comes from the "locale" cookie, then Accept-Language, and is
	// echoed in Content-Language.
	a := gox.MustNew("shop", gox.HTTP(), gox.WithMiddleware(i18n.Middleware(bundle, "locale")))

	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		// A key missing in es falls back to en; a key missing everywhere shows itself.
		gox.Error(w, r, gox.NotFound("%s", bundle.Tr(r.Context(), "errors.not_found")))
	})

	a.HandleFunc("GET /receipt-subject", func(w http.ResponseWriter, r *http.Request) {
		_ = gox.JSON(w, http.StatusOK, map[string]any{
			"subject": bundle.Tr(r.Context(), "receipt.subject", "name", "Ana"),
			// A subtree for a frontend: every key under "errors" in this locale.
			"errors": bundle.Messages(i18n.Locale(r.Context()), "errors"),
		})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Outside a request (a job mailing someone in their language) use
`bundle.T("es", key, ...)` or `i18n.WithLocale(ctx, "es")`. Placeholders are
`{name}`; `%{name}` from Ruby catalogs works unchanged. There are no plural
rules: write messages that need no count agreement.
