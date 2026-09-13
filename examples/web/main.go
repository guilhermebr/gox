// Command web is the plan's server-rendered example: a layout, two pages,
// a form with validation errors, static assets and a 404 page. Two
// imports plus templ.
//
//	go run ./examples/web
//	open http://localhost:8080/
//
// Set WEB_EXAMPLE_WEB_SESSION_SECRET (32+ bytes) outside development; in
// development an ephemeral secret is generated and logged as a warning.
package main

import (
	"embed"
	"net/http"
	"os"
	"sync"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"

	"github.com/guilhermebr/gox/examples/web/views"
)

//go:embed static
var static embed.FS

// guestbook is the example's whole "domain": names people signed with.
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
	a := gox.MustNew("web-example",
		gox.HTTP(),
		web.Enable(
			web.WithStatic(mustSub(static, "static")),
			web.WithSessions(),
			web.WithLayout(views.Layout),
			web.WithErrorPage(views.ErrorPage),
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
			web.Error(w, r, err)
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
