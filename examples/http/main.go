// Command http is a plain JSON API on gox: one import, two routes.
//
// Everything else is provided: request ids in every log line and response,
// traces, metrics on :9090/metrics, /healthz and /readyz on both ports,
// pprof, a per-request timeout and body limit, and one error envelope for
// handler errors, panics, 404s and 405s alike.
//
//	go run ./examples/http
//	curl -i localhost:8080/invoices/inv_1
//	curl -i localhost:8080/invoices/nope
//	curl -i -X POST localhost:8080/invoices -d '{"customer":"ana","amount":1250}'
package main

import (
	"net/http"
	"os"
	"sync"

	"github.com/guilhermebr/gox"
)

type invoice struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Amount   int    `json:"amount"`
}

// store is the example's in-memory database. Handlers run concurrently, so
// every access holds the lock.
type store struct {
	mu       sync.Mutex
	invoices map[string]invoice
}

func (s *store) get(id string) (invoice, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invoices[id]
	return inv, ok
}

func (s *store) put(inv invoice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoices[inv.ID] = inv
}

func main() {
	a := gox.MustNew("billing", gox.HTTP())
	db := &store{invoices: map[string]invoice{
		"inv_1": {ID: "inv_1", Customer: "ana", Amount: 1250},
	}}

	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		inv, ok := db.get(r.PathValue("id"))
		if !ok {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		_ = gox.JSON(w, http.StatusOK, inv)
	})

	a.HandleFunc("POST /invoices", func(w http.ResponseWriter, r *http.Request) {
		var in invoice
		if err := gox.Decode(r, &in); err != nil {
			gox.Error(w, r, err)
			return
		}
		if in.Amount <= 0 {
			gox.Error(w, r, gox.InvalidArgument("amount must be positive").WithDetail("field", "amount"))
			return
		}
		in.ID = "inv_" + in.Customer
		db.put(in)
		_ = gox.JSON(w, http.StatusCreated, in)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
