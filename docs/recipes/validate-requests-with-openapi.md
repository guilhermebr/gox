# Enforce an OpenAPI contract on incoming requests

```go path=api/api.go
// Package api embeds the contract clients are generated from.
package api

import _ "embed"

// Spec is the OpenAPI document.
//
//go:embed openapi.yaml
var Spec []byte
```

```yaml path=api/openapi.yaml
openapi: 3.1.0
info: {title: Shop API, version: 1.0.0}
paths:
  /api/v1/invoices:
    post:
      operationId: createInvoice
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [customer, amountCents]
              unevaluatedProperties: false
              properties:
                customer: {type: string, minLength: 1}
                amountCents: {type: integer, minimum: 1}
      responses: {"201": {description: created}}
```

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/openapi"

	"example.com/shop/api"
)

type invoice struct {
	Customer    string `json:"customer"`
	AmountCents int    `json:"amountCents"`
}

func main() {
	// Requests the document describes are validated (parameters, body,
	// formats) before the handler runs; a violation is a 400 whose "errors"
	// detail lists {code, message, pointer}. Everything else (health, pages,
	// webhooks) goes to the mux untouched. Pass several documents for several
	// APIs: openapi.Enable(api.Public, api.Admin).
	a := gox.MustNew("shop", gox.HTTP(), openapi.Enable(api.Spec))

	a.HandleFunc("POST /api/v1/invoices", func(w http.ResponseWriter, r *http.Request) {
		var in invoice
		if err := gox.Decode(r, &in); err != nil { // already valid; the body is still readable
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusCreated, in)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Typed handlers from the same document: `oapi-codegen` with the `std-http-server`
target emits an interface and a registration function for `net/http`'s mux,
which is what `a.Mux()` is:

```
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -generate types,std-http-server -package api -o api/api.gen.go api/openapi.yaml
```

then `api.HandlerFromMux(server, a.Mux())`. Pair it with
`gox.WithErrorRenderer(gox.ProblemJSON)` when the contract declares problem
details for errors.
