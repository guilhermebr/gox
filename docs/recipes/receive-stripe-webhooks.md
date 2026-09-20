# Call Stripe and receive its webhooks

```go path=main.go
package main

import (
	"net/http"
	"os"

	sdk "github.com/stripe/stripe-go/v82"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/providers/stripe"
)

func main() {
	// SHOP_STRIPE_API_KEY, and SHOP_STRIPE_WEBHOOK_SECRET (whsec_...) for the
	// endpoint below. gox.HTTPClient() gives the SDK the app's outbound client:
	// timeouts, tracing, request-id propagation.
	a := gox.MustNew("shop", gox.HTTP(), gox.HTTPClient(), stripe.Enable())

	a.HandleFunc("POST /customers", func(w http.ResponseWriter, r *http.Request) {
		cus, err := stripe.From(a).V1Customers.Create(r.Context(), &sdk.CustomerCreateParams{Email: sdk.String("ana@example.com")})
		if err != nil {
			gox.Error(w, r, gox.WrapError(err, gox.CodeUnavailable, "the customer could not be created"))
			return
		}
		_ = gox.JSON(w, http.StatusCreated, map[string]string{"id": cus.ID})
	})

	// Cross-origin protection lets this through: Stripe's servers send no
	// browser headers. The signature is the authentication.
	a.HandleFunc("POST /webhooks/stripe", func(w http.ResponseWriter, r *http.Request) {
		event, err := stripe.VerifyWebhook(a, r)
		if err != nil {
			gox.Error(w, r, err) // 401 for a bad signature
			return
		}
		switch event.Type {
		case "invoice.paid":
			a.Log().InfoContext(r.Context(), "invoice paid", "event", event.ID)
		}
		// Handle each event id once: Stripe retries until it gets a 2xx.
		w.WriteHeader(http.StatusNoContent)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Every provider with webhooks follows this shape: `workos.VerifyWebhook`,
`mailgun.VerifyWebhook`, `mailgun.ParseInbound`. Do slow work in a
`gox/jobs` worker and answer the webhook quickly.
