# Add a form with validation (decode → 422 re-render → flash + redirect)

```templ path=internal/signup/views/sign.templ
package views

import "github.com/guilhermebr/gox/web"

// SignInput is decoded from the form; tags drive validation.
type SignInput struct {
	Name  string `form:"name,required,min=2,max=40"`
	Email string `form:"email,required"`
	Agree bool   `form:"agree,required"`
}

templ SignForm(page *web.Page, in SignInput, errs web.FieldErrors) {
	<h1>Sign up</h1>
	<form method="post" action="/signup">
		@web.CSRFField(page)
		<label>Name <input name="name" value={ in.Name }/></label>
		if msg, ok := errs["name"]; ok {
			<span class="field-error">{ msg }</span>
		}
		<label>Email <input name="email" type="email" value={ in.Email }/></label>
		if msg, ok := errs["email"]; ok {
			<span class="field-error">{ msg }</span>
		}
		<label><input type="checkbox" name="agree" checked?={ in.Agree }/> I agree</label>
		if msg, ok := errs["agree"]; ok {
			<span class="field-error">{ msg }</span>
		}
		<button type="submit">Sign up</button>
	</form>
}
```

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/web"

	"example.com/shop/internal/signup/views"
)

func main() {
	a := gox.MustNew("shop", gox.HTTP(), web.Enable(web.WithSessions()))

	a.HandleFunc("GET /signup", func(w http.ResponseWriter, r *http.Request) {
		web.PageFrom(r).Title = "Sign up"
		_ = web.Render(w, r, views.SignForm(web.PageFrom(r), views.SignInput{}, nil))
	})

	a.HandleFunc("POST /signup", func(w http.ResponseWriter, r *http.Request) {
		in, ferrs, err := web.Form[views.SignInput](r)
		if err != nil {
			web.Error(w, r, err) // not a form body
			return
		}
		if ferrs != nil {
			web.PageFrom(r).Title = "Sign up"
			_ = web.RenderStatus(w, r, http.StatusUnprocessableEntity, views.SignForm(web.PageFrom(r), in, ferrs))
			return
		}
		web.AddFlash(w, r, "success", "Welcome, "+in.Name)
		web.Redirect(w, r, "/") // 303, or HX-Redirect for htmx
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
