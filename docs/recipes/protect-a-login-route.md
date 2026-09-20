# Rate-limit a route and know the real client IP

```go path=main.go
package main

import (
	"net/http"
	"os"
	"time"

	"github.com/guilhermebr/gox"
)

func main() {
	// Behind a load balancer set SHOP_HTTP_TRUSTED_PROXIES to its CIDRs
	// (10.0.0.0/8). X-Forwarded-For is believed only from those addresses and
	// read right to left, so a client cannot pick its own IP.
	a := gox.MustNew("shop", gox.HTTP())

	login := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.Log().InfoContext(r.Context(), "login attempt", "client_ip", gox.ClientIP(r))
		w.WriteHeader(http.StatusNoContent)
	})
	// Ten attempts a minute per client IP, then 429 with Retry-After. The
	// count is per process: with three replicas the ceiling is thirty.
	a.Mux().Handle("POST /login", gox.RateLimit(10, time.Minute)(login))

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

The access log carries `client_ip` on every line.
