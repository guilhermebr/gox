# Add an HTTP route

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
)

type invoice struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

var invoices = map[string]invoice{"inv_1": {ID: "inv_1", Amount: 1250}}

func main() {
	a := gox.MustNew("billing", gox.HTTP())

	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		inv, ok := invoices[r.PathValue("id")]
		if !ok {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		_ = gox.JSON(w, http.StatusOK, inv)
	})

	a.HandleFunc("POST /invoices", func(w http.ResponseWriter, r *http.Request) {
		var in invoice
		if err := gox.Decode(r, &in); err != nil {
			gox.Error(w, r, err) // 400 with the JSON problem, or 413 over HTTP_MAX_BODY_BYTES
			return
		}
		if in.Amount <= 0 {
			gox.Error(w, r, gox.InvalidArgument("amount must be positive").WithDetail("field", "amount"))
			return
		}
		invoices[in.ID] = in
		_ = gox.JSON(w, http.StatusCreated, in)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
