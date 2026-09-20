package mailgun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdk "github.com/mailgun/mailgun-go/v5"
	"github.com/mailgun/mailgun-go/v5/mtypes"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/mail"
)

// Config is the MAILGUN config section: BILLING_MAILGUN_* under a service
// prefixed BILLING.
type Config struct {
	APIKey            string `conf:"required,mask,help:sending API key"`
	Domain            string `conf:"required,help:sending domain such as mg.example.com"`
	BaseURL           string `conf:"help:API base without a version; empty for the US region and https://api.eu.mailgun.net for the EU"`
	WebhookSigningKey string `conf:"mask,help:HTTP webhook signing key; required by VerifyWebhook and ParseInbound"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	if c.BaseURL == "" {
		return nil
	}
	if u, err := url.Parse(c.BaseURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("MAILGUN_BASE_URL must be an absolute http(s) URL, got %q", c.BaseURL)
	}
	return nil
}

type (
	clientKey  struct{}
	featureKey struct{}
)

type feature struct {
	cfg    *Config
	client *sdk.Client
}

// Enable registers the MAILGUN config section and builds the API client.
// There is no lifecycle component: the client is a value.
func Enable() gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("MAILGUN", cfg, "mailgun.Enable()")
		b.Setup(func(a *gox.App) error {
			client := sdk.NewMailgun(cfg.APIKey)
			if cfg.BaseURL != "" {
				if err := client.SetAPIBase(strings.TrimRight(cfg.BaseURL, "/")); err != nil {
					return fmt.Errorf("mailgun: MAILGUN_BASE_URL: %w", err)
				}
			}
			if cfg.WebhookSigningKey != "" {
				client.SetWebhookSigningKey(cfg.WebhookSigningKey)
			}
			if a.HasHTTPClient() {
				client.SetHTTPClient(a.HTTPClient())
			}
			b.Set(clientKey{}, client)
			b.Set(featureKey{}, &feature{cfg: cfg, client: client})
			return nil
		})
		return nil
	}
}

// From returns the Mailgun API client. It panics if Enable was not passed
// to gox.New.
func From(a *gox.App) *sdk.Client {
	return gox.MustValue[*sdk.Client](a, clientKey{}, "mailgun.From", "mailgun.Enable()")
}

// Sender returns the mail.Sender that delivers through the API. Wrap it
// with mail.Restrict outside production.
func Sender(a *gox.App) mail.Sender {
	return &sender{f: gox.MustValue[*feature](a, featureKey{}, "mailgun.Sender", "mailgun.Enable()")}
}

type sender struct{ f *feature }

func (s *sender) Send(ctx context.Context, m mail.Message) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	msg := sdk.NewMessage(s.f.cfg.Domain, m.From, m.Subject, m.Text, m.To...)
	if m.HTML != "" {
		msg.SetHTML(m.HTML)
	}
	for _, r := range m.Cc {
		msg.AddCC(r)
	}
	for _, r := range m.Bcc {
		msg.AddBCC(r)
	}
	if m.ReplyTo != "" {
		msg.SetReplyTo(m.ReplyTo)
	}
	for k, v := range m.Headers {
		msg.AddHeader(k, v)
	}
	if len(m.Tags) > 0 {
		if err := msg.AddTag(m.Tags...); err != nil {
			return "", fmt.Errorf("mailgun: tags: %w", err)
		}
	}
	for _, a := range m.Attachments {
		msg.AddBufferAttachment(a.Filename, a.Data)
	}
	resp, err := s.f.client.Send(ctx, msg)
	if err != nil {
		return "", fmt.Errorf("mailgun: send: %w", err)
	}
	return resp.ID, nil
}

// Event is a delivery event Mailgun posts to a webhook.
type Event struct {
	ID        string
	Type      string // accepted, delivered, failed, opened, clicked, unsubscribed, complained
	Severity  string // for failed: temporary or permanent
	Recipient string
	MessageID string // the id Send returned, without angle brackets
	Time      time.Time
	Raw       json.RawMessage // the whole event-data object
}

// VerifyWebhook reads a delivery-event webhook, checks its signature with
// MAILGUN_WEBHOOK_SIGNING_KEY and returns the event. A bad signature is an
// unauthenticated error.
func VerifyWebhook(a *gox.App, r *http.Request) (*Event, error) {
	f := gox.MustValue[*feature](a, featureKey{}, "mailgun.VerifyWebhook", "mailgun.Enable()")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, gox.WrapError(err, gox.CodeInvalidArgument, "could not read the webhook body")
	}
	var payload struct {
		Signature mtypes.Signature `json:"signature"`
		Data      json.RawMessage  `json:"event-data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, gox.WrapError(err, gox.CodeInvalidArgument, "the webhook body is not JSON")
	}
	if err := f.verify(payload.Signature); err != nil {
		return nil, err
	}
	var data struct {
		ID        string  `json:"id"`
		Event     string  `json:"event"`
		Severity  string  `json:"severity"`
		Recipient string  `json:"recipient"`
		Timestamp float64 `json:"timestamp"`
		Message   struct {
			Headers struct {
				MessageID string `json:"message-id"`
			} `json:"headers"`
		} `json:"message"`
	}
	if err := json.Unmarshal(payload.Data, &data); err != nil {
		return nil, gox.WrapError(err, gox.CodeInvalidArgument, "the event data is not valid")
	}
	sec := int64(data.Timestamp)
	return &Event{
		ID: data.ID, Type: data.Event, Severity: data.Severity, Recipient: data.Recipient,
		MessageID: data.Message.Headers.MessageID, Raw: payload.Data,
		Time: time.Unix(sec, int64((data.Timestamp-float64(sec))*1e9)).UTC(),
	}, nil
}

// Inbound is a message Mailgun received and forwarded through a route.
type Inbound struct {
	Sender       string // envelope sender
	Recipient    string // the address at your domain that received it
	From         string // From header
	Subject      string
	Text         string
	StrippedText string // Text without quoted replies and signatures
	HTML         string
	MessageID    string
	InReplyTo    string
	MIME         string // the raw message, when the route URL ends in /mime
	Attachments  []mail.Attachment
}

// ParseInbound reads a message forwarded by a Mailgun route (a multipart or
// urlencoded form), checks its signature and returns it. The request body
// limit of the service applies: raise HTTP_MAX_BODY_BYTES to accept
// attachments.
func ParseInbound(a *gox.App, r *http.Request) (*Inbound, error) {
	f := gox.MustValue[*feature](a, featureKey{}, "mailgun.ParseInbound", "mailgun.Enable()")
	if err := r.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return nil, gox.WrapError(err, gox.CodeInvalidArgument, "could not read the inbound message")
	}
	if err := r.ParseForm(); err != nil {
		return nil, gox.WrapError(err, gox.CodeInvalidArgument, "could not read the inbound message")
	}
	v := r.FormValue
	if err := f.verify(mtypes.Signature{TimeStamp: v("timestamp"), Token: v("token"), Signature: v("signature")}); err != nil {
		return nil, err
	}
	in := &Inbound{
		Sender: v("sender"), Recipient: v("recipient"), From: v("from"), Subject: v("subject"),
		Text: v("body-plain"), StrippedText: v("stripped-text"), HTML: v("body-html"),
		MessageID: v("Message-Id"), InReplyTo: v("In-Reply-To"), MIME: v("body-mime"),
	}
	if r.MultipartForm != nil {
		for _, headers := range r.MultipartForm.File {
			for _, fh := range headers {
				file, err := fh.Open()
				if err != nil {
					return nil, gox.WrapError(err, gox.CodeInvalidArgument, "could not read an attachment")
				}
				data, err := io.ReadAll(file)
				_ = file.Close()
				if err != nil {
					return nil, gox.WrapError(err, gox.CodeInvalidArgument, "could not read an attachment")
				}
				in.Attachments = append(in.Attachments, mail.Attachment{Filename: fh.Filename, ContentType: fh.Header.Get("Content-Type"), Data: data})
			}
		}
	}
	return in, nil
}

func (f *feature) verify(sig mtypes.Signature) error {
	if f.cfg.WebhookSigningKey == "" {
		return gox.Internal("MAILGUN_WEBHOOK_SIGNING_KEY is not configured")
	}
	ok, err := f.client.VerifyWebhookSignature(sig)
	if err != nil || !ok {
		return gox.Unauthenticated("the Mailgun signature is not valid")
	}
	return nil
}
