# Return RFC 9457 problem details instead of the envelope

```go path=main.go
package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/guilhermebr/gox"
)

func main() {
	a := gox.MustNew("shop",
		gox.HTTP(),
		// Every error response, from handlers and from the framework (404,
		// 405, panics, timeouts, body limits), becomes application/problem+json.
		gox.WithErrorRenderer(func(w http.ResponseWriter, r *http.Request, status int, env gox.Envelope) bool {
			if strings.HasPrefix(r.URL.Path, "/legacy/") {
				return false // decline: these clients keep the envelope
			}
			return gox.ProblemJSON(w, r, status, env)
		}),
	)

	a.HandleFunc("POST /invoices", func(w http.ResponseWriter, r *http.Request) {
		// Details become top-level members: {"title":"Bad Request","status":400,
		// "code":"invalid_argument","detail":"...","errors":[...],"request_id":"..."}
		gox.Error(w, r, gox.InvalidArgument("the invoice is not valid").WithDetail("errors", []map[string]string{
			{"code": "blank", "message": "is required", "pointer": "/amountCents"},
		}))
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Pass `gox.ProblemJSON` directly when every route uses it:
`gox.WithErrorRenderer(gox.ProblemJSON)`.
