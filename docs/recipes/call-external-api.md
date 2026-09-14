# Call an external API

```go path=main.go
package main

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
)

type rate struct {
	Currency string  `json:"currency"`
	Rate     float64 `json:"rate"`
}

func main() {
	// gox.HTTPClient(): timeouts and pool limits from BILLING_HTTP_CLIENT_*,
	// User-Agent billing/<version>, request id and trace context propagated.
	a := gox.MustNew("billing", gox.HTTP(), gox.HTTPClient())
	client := a.HTTPClient()

	a.HandleFunc("GET /rates/{currency}", func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet,
			"https://rates.example.com/v1/"+r.PathValue("currency"), nil)
		resp, err := client.Do(req)
		if err != nil {
			gox.Error(w, r, gox.WrapError(err, gox.CodeUnavailable, "rates provider unavailable"))
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			gox.Error(w, r, gox.NotFound("currency %s", r.PathValue("currency")))
			return
		}
		var out rate
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			gox.Error(w, r, gox.WrapError(err, gox.CodeUnavailable, "rates provider returned an unexpected body"))
			return
		}
		_ = gox.JSON(w, http.StatusOK, out)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Retries are opt-in and only for idempotent requests: build a second client with `httpclient.New(cfg, httpclient.WithRetry(3, 200*time.Millisecond))` from `github.com/guilhermebr/gox/pkg/httpclient`.
