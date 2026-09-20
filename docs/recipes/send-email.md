# Send email (Mailgun in production, SMTP in development)

```go path=main.go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/mail"
	"github.com/guilhermebr/gox/providers/mailgun"
)

// Config picks the delivery path next to the framework's settings.
type Config struct {
	gox.BaseConfig
	SMTPAddr         string   `conf:"help:host:port of a mail catcher such as localhost:1025; when set mail goes there instead of Mailgun"`
	MailAllowDomains []string `conf:"help:when set only recipients at these domains get mail; set it on staging"`
}

func main() {
	// SHOP_MAILGUN_API_KEY, SHOP_MAILGUN_DOMAIN; EU accounts also
	// SHOP_MAILGUN_BASE_URL=https://api.eu.mailgun.net. Webhooks need
	// SHOP_MAILGUN_WEBHOOK_SIGNING_KEY.
	var cfg Config
	a := gox.MustNew("shop", gox.WithConfig(&cfg), gox.HTTP(), mailgun.Enable())

	// Handlers depend on mail.Sender, never on a provider.
	var sender mail.Sender = mailgun.Sender(a)
	if cfg.SMTPAddr != "" {
		sender = mail.SMTP(cfg.SMTPAddr, "", "")
	}
	if len(cfg.MailAllowDomains) > 0 {
		sender = mail.Restrict(sender, cfg.MailAllowDomains...) // staging never writes to real people
	}

	a.HandleFunc("POST /invoices/{id}/receipt", func(w http.ResponseWriter, r *http.Request) {
		id, err := sender.Send(r.Context(), mail.Message{
			From: "Shop <no-reply@shop.example>", To: []string{"ana@example.com"}, ReplyTo: "help@shop.example",
			Subject: "Your receipt", Text: "Thanks for your payment.", HTML: "<p>Thanks for your payment.</p>",
			Tags: []string{"receipt"}, Headers: map[string]string{"X-Entity-Ref": r.PathValue("id")},
		})
		if err != nil {
			gox.Error(w, r, gox.WrapError(err, gox.CodeUnavailable, "the receipt could not be sent"))
			return
		}
		_ = gox.JSON(w, http.StatusAccepted, map[string]string{"message_id": id}) // store it to match delivery events
	})

	// Delivery events: delivered, failed (temporary or permanent), complained.
	a.HandleFunc("POST /webhooks/mailgun", func(w http.ResponseWriter, r *http.Request) {
		ev, err := mailgun.VerifyWebhook(a, r)
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		a.Log().InfoContext(r.Context(), "mail event", "type", ev.Type, "severity", ev.Severity, "recipient", ev.Recipient, "message_id", ev.MessageID)
		w.WriteHeader(http.StatusNoContent)
	})

	// Inbound mail: a Mailgun route forwarding to this URL.
	a.HandleFunc("POST /webhooks/mailgun/inbound", func(w http.ResponseWriter, r *http.Request) {
		in, err := mailgun.ParseInbound(a, r)
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		a.Log().InfoContext(r.Context(), "mail received", "from", in.Sender, "to", in.Recipient, "subject", in.Subject, "attachments", len(in.Attachments))
		w.WriteHeader(http.StatusNoContent) // any 2xx tells Mailgun not to retry
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

In tests pass a `*mail.Recorder` where the code takes a `mail.Sender` and
assert on `Messages()`. Send from a background job (`gox/jobs`) when a
request should not wait for the provider. Inbound messages with
attachments need a larger `SHOP_HTTP_MAX_BODY_BYTES`.
