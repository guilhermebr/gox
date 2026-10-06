// Package gox is an opinionated base for Go services. A plain HTTP service
// imports only this package and gets config from environment variables,
// structured logs, traces and metrics, /healthz and /readyz, an admin server
// and graceful shutdown.
//
// main declares what the service uses, registers plain net/http handlers
// with Go 1.22 patterns, then runs:
//
//	func main() {
//		a := gox.MustNew("billing", gox.HTTP())
//		a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
//			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
//		})
//		if err := a.Run(); err != nil {
//			os.Exit(1)
//		}
//	}
//
// Everything heavier is a module of its own under github.com/guilhermebr/gox/,
// opted into by importing it and passing its Enable option to New:
//
//	postgres          pgx pool, embedded migrations, transactions
//	jobs              durable background jobs on Postgres (River)
//	jwt               bearer tokens: HS256, RS256 or an identity provider's JWKS
//	openapi           request validation against OpenAPI 3 documents
//	web               server-rendered HTML: templ, sessions, CSRF, forms
//	providers/<name>  third-party clients: mailgun, posthog, s3, stripe, supabase, temporal, workos
//
// monetary and osrelease are plain libraries with no framework dependency.
//
// Look things up instead of guessing; these match the version in go.mod:
//
//	go doc github.com/guilhermebr/gox.Periodic   # one symbol
//	go doc github.com/guilhermebr/gox/postgres   # one module
//	go run ./cmd/<name> --help                   # every variable the service reads
//	go list -m -f '{{.Dir}}' github.com/guilhermebr/gox
//
// The last prints this module's directory (empty until go mod download
// github.com/guilhermebr/gox); its docs/recipes/README.md indexes the task
// recipes.
package gox
