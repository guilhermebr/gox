# Track product events and read feature flags (PostHog)

```go path=main.go
package main

import (
	"net/http"
	"os"

	sdk "github.com/posthog/posthog-go"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/providers/posthog"
)

func main() {
	// SHOP_POSTHOG_PROJECT_KEY (phc_...); SHOP_POSTHOG_HOST for the EU or a
	// self-hosted instance; SHOP_POSTHOG_SECRET_KEY to evaluate flags locally.
	a := gox.MustNew("shop", gox.HTTP(), posthog.Enable())

	a.HandleFunc("POST /invoices/{id}/pay", func(w http.ResponseWriter, r *http.Request) {
		userID := "user_01" // from your session

		// Enqueue never blocks the request: events go out in batches, and the
		// queue is flushed when the app stops.
		_ = posthog.From(a).Enqueue(sdk.Capture{
			DistinctId: userID, Event: "invoice paid",
			Properties: sdk.NewProperties().Set("invoice_id", r.PathValue("id")),
		})

		newFlow, err := posthog.From(a).IsFeatureEnabled(sdk.FeatureFlagPayload{Key: "new-receipt", DistinctId: userID})
		if err != nil {
			newFlow = false // a flag lookup must never fail the request
		}
		_ = gox.JSON(w, http.StatusOK, map[string]any{"new_receipt": newFlow})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
