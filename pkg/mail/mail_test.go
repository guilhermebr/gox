package mail_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strings"
	"testing"

	"github.com/guilhermebr/gox/pkg/mail"
)

func TestValidateNamesWhatIsMissing(t *testing.T) {
	for want, m := range map[string]mail.Message{
		"From":           {To: []string{"a@example.com"}, Subject: "s", Text: "t"},
		"To":             {From: "a@example.com", Subject: "s", Text: "t"},
		"Subject":        {From: "a@example.com", To: []string{"b@example.com"}, Text: "t"},
		"Text or HTML":   {From: "a@example.com", To: []string{"b@example.com"}, Subject: "s"},
		"not-an-address": {From: "a@example.com", To: []string{"not-an-address"}, Subject: "s", Text: "t"},
	} {
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func TestRestrictRefusesRecipientsOutsideTheAllowedDomains(t *testing.T) {
	rec := &mail.Recorder{}
	s := mail.Restrict(rec, "example.com", "Partner.Example")
	ok := mail.Message{From: "Shop <no-reply@example.com>", To: []string{"Ana <ana@EXAMPLE.com>"}, Cc: []string{"bo@partner.example"}, Subject: "s", Text: "t"}
	if _, err := s.Send(context.Background(), ok); err != nil {
		t.Fatal(err)
	}
	for _, m := range []mail.Message{
		{From: "a@example.com", To: []string{"ana@example.com", "eve@gmail.com"}, Subject: "s", Text: "t"},
		{From: "a@example.com", To: []string{"ana@example.com"}, Bcc: []string{"eve@evil-example.com"}, Subject: "s", Text: "t"},
	} {
		_, err := s.Send(context.Background(), m)
		if !errors.Is(err, mail.ErrRecipientNotAllowed) || !strings.Contains(err.Error(), "eve@") {
			t.Errorf("err = %v", err)
		}
	}
	if len(rec.Messages()) != 1 {
		t.Fatalf("delivered = %d, want 1", len(rec.Messages()))
	}
}

// smtpServer speaks just enough SMTP to capture one message.
func smtpServer(t *testing.T) (addr string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		say("220 test")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					got <- data.String()
					say("250 queued")
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 test")
			case strings.HasPrefix(cmd, "DATA"):
				inData = true
				say("354 go")
			case strings.HasPrefix(cmd, "QUIT"):
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	return ln.Addr().String(), got
}

func TestSMTPSendsAStandardMultipartMessage(t *testing.T) {
	addr, got := smtpServer(t)
	s := mail.SMTP(addr, "", "")
	id, err := s.Send(context.Background(), mail.Message{
		From: "Shop <no-reply@example.com>", To: []string{"Ana Souza <ana@example.com>"}, Bcc: []string{"audit@example.com"},
		ReplyTo: "help@example.com", Subject: "Recibo nº 42 – obrigado", Text: "Olá, Ana.", HTML: "<p>Olá, <b>Ana</b>.</p>",
		Headers:     map[string]string{"X-Entity-Ref": "inv_42"},
		Attachments: []mail.Attachment{{Filename: "recibo.pdf", ContentType: "application/pdf", Data: []byte("%PDF")}},
	})
	if err != nil || id == "" {
		t.Fatalf("Send = %q %v", id, err)
	}
	raw := <-got
	msg, err := netmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	subject, _ := dec.DecodeHeader(msg.Header.Get("Subject"))
	if subject != "Recibo nº 42 – obrigado" || msg.Header.Get("Reply-To") != "help@example.com" ||
		msg.Header.Get("X-Entity-Ref") != "inv_42" || msg.Header.Get("Message-Id") == "" || msg.Header.Get("Date") == "" {
		t.Fatalf("headers = %v (subject %q)", msg.Header, subject)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Fatal("Bcc must not appear in the headers")
	}
	mt, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if mt != "multipart/mixed" {
		t.Fatalf("content type = %s", mt)
	}
	var kinds []string
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		pt, pp, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		kinds = append(kinds, pt)
		if pt == "multipart/alternative" {
			inner := multipart.NewReader(p, pp["boundary"])
			for {
				ip, err := inner.NextPart()
				if err != nil {
					break
				}
				it, _, _ := mime.ParseMediaType(ip.Header.Get("Content-Type"))
				kinds = append(kinds, it)
			}
		}
	}
	if strings.Join(kinds, ",") != "multipart/alternative,text/plain,text/html,application/pdf" {
		t.Fatalf("parts = %v", kinds)
	}
}
