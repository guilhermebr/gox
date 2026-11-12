# Push updates to browsers with server-sent events

Topics work like Mercure's: a client subscribes to the topics it cares
about, the service publishes to a topic, and every subscriber of a matching
topic gets the event. With more than one replica, updates fan out through
Postgres LISTEN/NOTIFY.

```go path=main.go
package main

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jwt"
	"github.com/guilhermebr/gox/postgres"
)

func main() {
	a := gox.MustNew("shop",
		postgres.Enable(),
		jwt.Enable(),
		gox.HTTP(gox.WithSSE(gox.SSEConfig{
			// Fan updates out to every replica.
			Transport: postgres.SSETransport,
			// Private updates on /users/{id} reach only that user. The
			// request carries what the chain put there; who may subscribe
			// at all is the chain's business (gox.WithAuth).
			Authorize: func(r *http.Request, topic string) bool {
				c, ok := jwt.ClaimsFromContext(r.Context())
				return ok && topic == "/users/"+c.UserID
			},
		})),
	)

	a.HandleFunc("POST /orders/{id}/pay", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		// ... mark the order paid ...
		data, _ := json.Marshal(map[string]string{"id": id, "status": "paid"})
		if _, err := a.SSE().Publish(r.Context(), gox.SSEUpdate{
			Topics: []string{"/orders/" + id},
			Event:  "order.paid",
			Data:   string(data),
		}); err != nil {
			gox.Error(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

```js path=static/orders.js
// Every order, or one: ?topic=/orders/42. Templates match one segment
// ({id}) or the rest of the topic ({path...}); * matches everything.
const events = new EventSource("/events?topic=" + encodeURIComponent("/orders/{id}"), { withCredentials: true });
events.addEventListener("order.paid", (e) => console.log(JSON.parse(e.data)));
// On reconnect the browser sends Last-Event-ID and the hub replays what
// was missed, as long as it is still in SSEConfig.History; otherwise it
// sends a "resync" event (gox.SSEResyncEvent) and the page should refetch.
events.addEventListener("resync", () => location.reload());
```

`EventSource` cannot send an `Authorization` header: authenticate the
subscription with a cookie (gox/web sessions), or use a fetch-based SSE
client to send a bearer token. Only the subscription route is exempt from
`HTTP_REQUEST_TIMEOUT` and the server's read and write timeouts; every other
route keeps them. Streams end after `SSEConfig.MaxAge` (15m) so the browser
reconnects through the middleware chain and an expired token stops them.
NOTIFY carries about 7.9 KB, so publish an ID or a URL for anything bigger
(`postgres.ErrSSETooLarge`), and point `POSTGRES_URL` at the database or a
session-mode pooler: LISTEN gets nothing through a transaction-mode one.
