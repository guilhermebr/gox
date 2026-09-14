// Package hello is the example feature: replace it with your own. Each
// feature package exposes Register, which mounts its routes on the app.
package hello

import (
	"net/http"

	"github.com/guilhermebr/gox"
)

type handler struct {
	greeting string
}

// Register mounts the feature's routes.
func Register(a *gox.App, greeting string) {
	h := &handler{greeting: greeting}
	a.HandleFunc("GET /hello/{name}", h.greet)
}

// Index answers the root with a pointer to the API.
func Index(w http.ResponseWriter, _ *http.Request) {
	_ = gox.JSON(w, http.StatusOK, map[string]string{"try": "/hello/world"})
}

func (h *handler) greet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if len(name) > 40 {
		gox.Error(w, r, gox.InvalidArgument("name must be at most 40 characters").WithDetail("field", "name"))
		return
	}
	_ = gox.JSON(w, http.StatusOK, map[string]string{"greeting": h.greeting + ", " + name})
}
