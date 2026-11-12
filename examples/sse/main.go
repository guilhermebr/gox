// Command sse pushes server-sent events with topics, in the spirit of
// Mercure: browsers subscribe to topics, the service publishes to them.
//
// Updates fan out in memory, which serves the subscribers of one process.
// With more than one replica, add postgres.Enable() and set
// SSEConfig.Transport to postgres.SSETransport.
//
//	go run ./examples/sse
//	open http://localhost:8080/                       # or: curl -N 'localhost:8080/events?topic=/orders/{id}'
//	curl -i -X POST localhost:8080/orders/42/pay       # every /orders/{id} subscriber gets order.paid
//	curl -i -X POST localhost:8080/users/ana/notify    # private: only subscribers signed in as ana
//	curl -i -X POST localhost:8080/users/jose/notify   # private to jose: the page (signed in as ana) won't update
package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
)

//go:embed index.html
var index []byte

func main() {
	a := gox.MustNew("sse-example", gox.HTTP(gox.WithSSE(gox.SSEConfig{
		// Private updates on /users/{name} reach only that user. The demo
		// trusts a "user" cookie; a real service reads its session or
		// JWT claims from r here.
		Authorize: func(r *http.Request, topic string) bool {
			c, err := r.Cookie("user")
			return err == nil && topic == "/users/"+c.Value
		},
	})))
	hub := a.SSE()

	a.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "user", Value: "ana", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})

	a.HandleFunc("POST /orders/{id}/pay", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		data, _ := json.Marshal(map[string]string{"id": id, "status": "paid"})
		eventID, err := hub.Publish(r.Context(), gox.SSEUpdate{
			Topics: []string{"/orders/" + id},
			Event:  "order.paid",
			Data:   string(data),
		})
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusAccepted, map[string]string{"event_id": eventID})
	})

	a.HandleFunc("POST /users/{name}/notify", func(w http.ResponseWriter, r *http.Request) {
		eventID, err := hub.Publish(r.Context(), gox.SSEUpdate{
			Topics:  []string{"/users/" + r.PathValue("name")},
			Event:   "notice",
			Data:    `{"text":"your export is ready"}`,
			Private: true,
		})
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusAccepted, map[string]string{"event_id": eventID})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
