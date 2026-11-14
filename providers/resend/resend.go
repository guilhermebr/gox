package resend

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sdk "github.com/resend/resend-go/v4"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/mail"
)

// HeaderIdempotencyKey is the mail.Message.Headers entry that carries the
// idempotency key. The sender moves it to the Idempotency-Key request
// header and does not put it in the email, so a message sent twice with the
// same key within 24 hours is delivered once.
const HeaderIdempotencyKey = "Idempotency-Key"

// ErrNotConfigured is returned by the Sender when RESEND_API_KEY is empty.
var ErrNotConfigured = errors.New("resend: RESEND_API_KEY is empty")

const (
	attempts      = 3
	firstBackoff  = 500 * time.Millisecond
	maxBackoff    = 5 * time.Second
	maxRetryAfter = 15 * time.Second // a longer Retry-After is not worth waiting for in a request
	httpTimeout   = 15 * time.Second
	defaultBase   = "https://api.resend.com"
)

// Config is the RESEND config section: BILLING_RESEND_* under a service
// prefixed BILLING.
type Config struct {
	APIKey  string `conf:"mask,help:API key (re_...); empty turns sending off: Configured reports false and Send fails"`
	BaseURL string `conf:"help:API base URL; empty for https://api.resend.com"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	if c.BaseURL == "" {
		return nil
	}
	if u, err := url.Parse(c.BaseURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("RESEND_BASE_URL must be an absolute http(s) URL, got %q", c.BaseURL)
	}
	return nil
}

type (
	clientKey struct{}
	senderKey struct{}
)

// Enable registers the RESEND config section and builds the API client.
// There is no lifecycle component: the client is a value. The key is
// optional so a service can run without mail in development: ask Configured
// before relying on the Sender.
func Enable() gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("RESEND", cfg, "resend.Enable()")
		b.Setup(func(a *gox.App) error {
			hc := &http.Client{Timeout: httpTimeout}
			if a.HasHTTPClient() {
				hc = a.HTTPClient()
			}
			key := strings.TrimSpace(cfg.APIKey)
			client := sdk.NewCustomClient(hc, key)
			base, err := url.Parse(strings.TrimRight(cmp.Or(cfg.BaseURL, defaultBase), "/") + "/")
			if err != nil {
				return fmt.Errorf("resend: RESEND_BASE_URL: %w", err)
			}
			client.BaseURL = base
			b.Set(clientKey{}, client)
			b.Set(senderKey{}, &sender{emails: client.Emails, on: key != "", backoff: firstBackoff, sleep: sleepCtx})
			return nil
		})
		return nil
	}
}

// From returns the Resend API client. It panics if Enable was not passed to
// gox.New.
func From(a *gox.App) *sdk.Client {
	return gox.MustValue[*sdk.Client](a, clientKey{}, "resend.From", "resend.Enable()")
}

// Configured reports whether RESEND_API_KEY is set, so the service can log
// a link or skip the mail instead of failing when it is not.
func Configured(a *gox.App) bool {
	return gox.MustValue[*sender](a, senderKey{}, "resend.Configured", "resend.Enable()").on
}

// Sender returns the mail.Sender that delivers through the API. Wrap it
// with mail.Restrict outside production.
//
// Each Send makes up to three attempts with exponential backoff, only for
// network errors, 5xx and 429 (a Retry-After of up to 15 seconds is
// respected). Other 4xx answers are final. Set HeaderIdempotencyKey on the
// message so a retry, here or by the caller, cannot deliver it twice.
func Sender(a *gox.App) mail.Sender {
	return gox.MustValue[*sender](a, senderKey{}, "resend.Sender", "resend.Enable()")
}

type emailsAPI interface {
	SendWithOptions(ctx context.Context, params *sdk.SendEmailRequest, options *sdk.SendEmailOptions) (*sdk.SendEmailResponse, error)
}

type sender struct {
	emails  emailsAPI
	on      bool
	backoff time.Duration
	sleep   func(ctx context.Context, d time.Duration) error
}

func (s *sender) Send(ctx context.Context, m mail.Message) (string, error) {
	if !s.on {
		return "", ErrNotConfigured
	}
	if err := m.Validate(); err != nil {
		return "", err
	}
	req := &sdk.SendEmailRequest{
		From: m.From, To: m.To, Cc: m.Cc, Bcc: m.Bcc, ReplyTo: m.ReplyTo,
		Subject: m.Subject, Html: m.HTML, Text: m.Text,
	}
	var key string
	for k, v := range m.Headers {
		if strings.EqualFold(k, HeaderIdempotencyKey) {
			key = v
			continue
		}
		if req.Headers == nil {
			req.Headers = map[string]string{}
		}
		req.Headers[k] = v
	}
	for _, t := range m.Tags {
		req.Tags = append(req.Tags, sdk.Tag{Name: "tag", Value: t})
	}
	for _, a := range m.Attachments {
		req.Attachments = append(req.Attachments, &sdk.Attachment{Filename: a.Filename, ContentType: a.ContentType, Content: a.Data})
	}
	opts := &sdk.SendEmailOptions{IdempotencyKey: key}

	var err error
	for attempt := 1; ; attempt++ {
		var resp *sdk.SendEmailResponse
		resp, err = s.emails.SendWithOptions(ctx, req, opts)
		if err == nil {
			return resp.Id, nil
		}
		wait, retry := s.retryable(ctx, err, attempt)
		if !retry {
			break
		}
		if serr := s.sleep(ctx, wait); serr != nil {
			err = serr
			break
		}
	}
	return "", fmt.Errorf("resend: send: %w", err)
}

// retryable decides whether another attempt follows err and how long to wait.
func (s *sender) retryable(ctx context.Context, err error, attempt int) (time.Duration, bool) {
	if attempt >= attempts || ctx.Err() != nil {
		return 0, false
	}
	delay := min(s.backoff<<(attempt-1), maxBackoff)
	delay += time.Duration(rand.Int64N(int64(delay)/4 + 1)) //nolint:gosec // jitter, not a secret
	var rl *sdk.RateLimitError
	var api *sdk.ResendError
	var netErr *url.Error // what http.Client.Do returns for connection, TLS and timeout failures
	switch {
	case errors.As(err, &rl):
		if ra, ok := parseRetryAfter(rl.RetryAfter); ok {
			if ra > maxRetryAfter {
				return 0, false
			}
			delay = max(delay, ra)
		}
		return delay, true
	case errors.As(err, &api):
		return delay, api.StatusCode >= 500
	case errors.As(err, &netErr):
		return delay, true
	}
	return 0, false
}

// parseRetryAfter reads the header as seconds or an HTTP date.
func parseRetryAfter(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 {
		return time.Duration(n * float64(time.Second)), true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(0, time.Until(t)), true
	}
	return 0, false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
