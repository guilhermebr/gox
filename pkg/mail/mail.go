// Package mail is the provider-neutral side of sending email: the Message
// every mail provider accepts (providers/mailgun), a Sender interface, an
// SMTP sender for development and self-hosted relays, a guard that keeps a
// staging environment from mailing real people, and a recorder for tests.
package mail

import (
	"context"
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
	"sync"
)

// Message is one email. Addresses are RFC 5322: "ana@example.com" or
// "Ana Souza <ana@example.com>".
type Message struct {
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	ReplyTo     string
	Subject     string
	Text        string            // plain-text body; send it with HTML so every client can read the mail
	HTML        string            // HTML body
	Headers     map[string]string // extra headers such as X-Entity-Ref
	Tags        []string          // provider-side labels for analytics; ignored by SMTP
	Attachments []Attachment
}

// Attachment is a file sent with a Message.
type Attachment struct {
	Filename    string
	ContentType string // default application/octet-stream
	Data        []byte
}

// Sender delivers messages. The id is the provider's message id, for
// matching delivery events later.
type Sender interface {
	Send(ctx context.Context, m Message) (id string, err error)
}

// Validate checks that the message can be sent and that every address parses.
func (m Message) Validate() error {
	var errs []error
	if m.From == "" {
		errs = append(errs, errors.New("mail: From is required"))
	}
	if len(m.To) == 0 {
		errs = append(errs, errors.New("mail: To needs at least one recipient"))
	}
	if m.Subject == "" {
		errs = append(errs, errors.New("mail: Subject is required"))
	}
	if m.Text == "" && m.HTML == "" {
		errs = append(errs, errors.New("mail: Text or HTML is required"))
	}
	addrs := append([]string{m.From}, m.Recipients()...)
	if m.ReplyTo != "" {
		addrs = append(addrs, m.ReplyTo)
	}
	for _, a := range addrs {
		if a == "" {
			continue
		}
		if _, err := netmail.ParseAddress(a); err != nil {
			errs = append(errs, fmt.Errorf("mail: %q is not an address: %w", a, err))
		}
	}
	return errors.Join(errs...)
}

// Recipients lists To, Cc and Bcc together: everyone the message reaches.
func (m Message) Recipients() []string {
	out := make([]string, 0, len(m.To)+len(m.Cc)+len(m.Bcc))
	out = append(out, m.To...)
	out = append(out, m.Cc...)
	return append(out, m.Bcc...)
}

// ErrRecipientNotAllowed is returned by a restricted Sender.
var ErrRecipientNotAllowed = errors.New("mail: recipient not allowed")

// Restrict wraps next so a message is refused unless every recipient is at
// one of the domains. Use it outside production: a staging environment with
// a copy of real data must not write to real people.
func Restrict(next Sender, domains ...string) Sender {
	allowed := make(map[string]bool, len(domains))
	for _, d := range domains {
		allowed[strings.ToLower(d)] = true
	}
	return senderFunc(func(ctx context.Context, m Message) (string, error) {
		for _, r := range m.Recipients() {
			addr, err := netmail.ParseAddress(r)
			if err != nil {
				return "", fmt.Errorf("mail: %q is not an address: %w", r, err)
			}
			_, domain, _ := strings.Cut(addr.Address, "@")
			if !allowed[strings.ToLower(domain)] {
				return "", fmt.Errorf("%w: %s (allowed domains: %s)", ErrRecipientNotAllowed, addr.Address, strings.Join(domains, ", "))
			}
		}
		return next.Send(ctx, m)
	})
}

type senderFunc func(ctx context.Context, m Message) (string, error)

func (f senderFunc) Send(ctx context.Context, m Message) (string, error) { return f(ctx, m) }

// Recorder is a Sender for tests: it keeps what was sent.
type Recorder struct {
	mu   sync.Mutex
	sent []Message
}

// Send records m after validating it, like a real sender would.
func (r *Recorder) Send(_ context.Context, m Message) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, m)
	return fmt.Sprintf("recorded-%d", len(r.sent)), nil
}

// Messages returns what was sent so far.
func (r *Recorder) Messages() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Message(nil), r.sent...)
}
