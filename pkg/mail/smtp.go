package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// SMTP returns a Sender for an SMTP server at addr ("localhost:1025"): a
// development mail catcher or a self-hosted relay. With a username it
// authenticates with PLAIN, which net/smtp only allows over TLS or to
// localhost.
func SMTP(addr, username, password string) Sender {
	return &smtpSender{addr: addr, username: username, password: password}
}

type smtpSender struct {
	addr, username, password string
}

func (s *smtpSender) Send(_ context.Context, m Message) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	host, _, err := net.SplitHostPort(s.addr)
	if err != nil {
		return "", fmt.Errorf("mail: smtp address %q: %w", s.addr, err)
	}
	from, _ := netmail.ParseAddress(m.From)
	var rcpt []string
	for _, r := range m.Recipients() {
		a, _ := netmail.ParseAddress(r) // Validate parsed them already
		rcpt = append(rcpt, a.Address)
	}
	_, domain, _ := strings.Cut(from.Address, "@")
	id := messageID(domain)
	raw, err := build(m, id)
	if err != nil {
		return "", err
	}
	var auth smtp.Auth
	if s.username != "" {
		auth = smtp.PlainAuth("", s.username, s.password, host)
	}
	if err := smtp.SendMail(s.addr, auth, from.Address, rcpt, raw); err != nil {
		return "", fmt.Errorf("mail: smtp %s: %w", s.addr, err)
	}
	return id, nil
}

func messageID(domain string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "<" + hex.EncodeToString(b) + "@" + domain + ">"
}

// build renders the RFC 5322 message: multipart/alternative for the bodies,
// wrapped in multipart/mixed when there are attachments. Bcc is envelope only.
func build(m Message, id string) ([]byte, error) {
	var buf bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	header("From", m.From)
	header("To", strings.Join(m.To, ", "))
	if len(m.Cc) > 0 {
		header("Cc", strings.Join(m.Cc, ", "))
	}
	if m.ReplyTo != "" {
		header("Reply-To", m.ReplyTo)
	}
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-Id", id)
	header("MIME-Version", "1.0")
	for k, v := range m.Headers {
		header(textproto.CanonicalMIMEHeaderKey(k), mime.QEncoding.Encode("utf-8", v))
	}

	var body bytes.Buffer
	alt := multipart.NewWriter(&body)
	text := func(contentType, content string) error {
		if content == "" {
			return nil
		}
		part, err := alt.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {contentType + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return err
		}
		qp := quotedprintable.NewWriter(part)
		if _, err := qp.Write([]byte(content)); err != nil {
			return err
		}
		return qp.Close()
	}
	if err := text("text/plain", m.Text); err != nil {
		return nil, fmt.Errorf("mail: %w", err)
	}
	if err := text("text/html", m.HTML); err != nil {
		return nil, fmt.Errorf("mail: %w", err)
	}
	_ = alt.Close()
	altType := "multipart/alternative; boundary=" + alt.Boundary()

	if len(m.Attachments) == 0 {
		header("Content-Type", altType)
		buf.WriteString("\r\n")
		buf.Write(body.Bytes())
		return buf.Bytes(), nil
	}

	var mixedBody bytes.Buffer
	mixed := multipart.NewWriter(&mixedBody)
	part, err := mixed.CreatePart(textproto.MIMEHeader{"Content-Type": {altType}})
	if err != nil {
		return nil, fmt.Errorf("mail: %w", err)
	}
	_, _ = part.Write(body.Bytes())
	for _, a := range m.Attachments {
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		part, err := mixed.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {ct},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename})},
		})
		if err != nil {
			return nil, fmt.Errorf("mail: %w", err)
		}
		enc := base64.StdEncoding.EncodeToString(a.Data)
		for len(enc) > 76 {
			_, _ = part.Write([]byte(enc[:76] + "\r\n"))
			enc = enc[76:]
		}
		_, _ = part.Write([]byte(enc + "\r\n"))
	}
	_ = mixed.Close()
	header("Content-Type", "multipart/mixed; boundary="+mixed.Boundary())
	buf.WriteString("\r\n")
	buf.Write(mixedBody.Bytes())
	return buf.Bytes(), nil
}
