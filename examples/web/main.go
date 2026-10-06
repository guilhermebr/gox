// Command web is a server-rendered app on templ: a layout, two pages, a form
// with validation errors, flash messages, static assets and an error page.
// Two gox imports plus templ.
//
//	go run ./examples/web
//	open http://localhost:8080/
//
// Set SHOP_WEB_SESSION_SECRET (32+ bytes) in production; elsewhere an
// ephemeral secret is generated and a warning names the variable.
package main

import (
	"net/http"
	"os"
	"sync"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"

	"github.com/guilhermebr/gox/examples/web/internal/guestbook/views"
	"github.com/guilhermebr/gox/examples/web/static"
	"github.com/guilhermebr/gox/examples/web/web/layout"
)

// guestbook is the example's whole "domain": names people signed with.
// Handlers run concurrently, so every access holds the lock.
type guestbook struct {
	mu    sync.Mutex
	names []string
}

func (g *guestbook) add(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.names = append(g.names, name)
}

func (g *guestbook) all() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.names...)
}

func main() {
	a := gox.MustNew("shop",
		gox.HTTP(),
		web.Enable(
			web.WithStatic(static.FS),
			web.WithSessions(),
			web.WithLayout(layout.Layout),
			web.WithErrorPage(layout.ErrorPage),
		),
	)
	book := &guestbook{}

	a.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		web.PageFrom(r).Title = "Guestbook"
		_ = web.Render(w, r, views.Home(book.all()))
	})

	a.HandleFunc("GET /sign", func(w http.ResponseWriter, r *http.Request) {
		web.PageFrom(r).Title = "Sign the guestbook"
		_ = web.Render(w, r, views.SignForm(web.PageFrom(r), views.SignInput{}, nil))
	})

	// The one form pattern: decode, re-render with 422 on field errors,
	// flash and redirect on success.
	a.HandleFunc("POST /sign", func(w http.ResponseWriter, r *http.Request) {
		in, ferrs, err := web.Form[views.SignInput](r)
		if err != nil {
			gox.Error(w, r, err) // the error page for a browser, the JSON envelope otherwise
			return
		}
		if ferrs != nil {
			web.PageFrom(r).Title = "Sign the guestbook"
			_ = web.RenderStatus(w, r, http.StatusUnprocessableEntity, views.SignForm(web.PageFrom(r), in, ferrs))
			return
		}
		book.add(in.Name)
		web.AddFlash(w, r, "success", "Thanks for signing, "+in.Name+"!")
		web.Redirect(w, r, "/")
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
