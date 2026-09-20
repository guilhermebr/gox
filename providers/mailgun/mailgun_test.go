package mailgun_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/mail"
	"github.com/guilhermebr/gox/providers/mailgun"
)

const signingKey = "whsec-test-key"

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type sent struct {
	path, user, pass string
	form             map[string][]string
	files            map[string]string
}

func newApp(t *testing.T) (*gox.App, chan sent) {
	t.Helper()
	got := make(chan sent, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s := sent{path: r.URL.Path, form: r.MultipartForm.Value, files: map[string]string{}}
		s.user, s.pass, _ = r.BasicAuth()
		for _, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				f, _ := fh.Open()
				b, _ := io.ReadAll(f)
				s.files[fh.Filename] = string(b)
			}
		}
		got <- s
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"<20260101.1@mg.example.com>","message":"Queued. Thank you."}`))
	}))
	t.Cleanup(api.Close)
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
	t.Setenv("SHOP_MAILGUN_API_KEY", "key-123")
	t.Setenv("SHOP_MAILGUN_DOMAIN", "mg.example.com")
	t.Setenv("SHOP_MAILGUN_BASE_URL", api.URL)
	t.Setenv("SHOP_MAILGUN_WEBHOOK_SIGNING_KEY", signingKey)
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), mailgun.Enable())
	if err != nil {
		t.Fatal(err)
	}
	return a, got
}

func TestSenderDeliversTheWholeMessageThroughTheAPI(t *testing.T) {
	a, got := newApp(t)
	s := mailgun.Sender(a)
	id, err := s.Send(context.Background(), mail.Message{
		From: "Shop <no-reply@example.com>", To: []string{"ana@example.com"}, Cc: []string{"bo@example.com"}, Bcc: []string{"audit@example.com"},
		ReplyTo: "help@example.com", Subject: "Receipt 42", Text: "plain", HTML: "<p>html</p>",
		Headers: map[string]string{"X-Entity-Ref": "inv_42"}, Tags: []string{"receipts"},
		Attachments: []mail.Attachment{{Filename: "receipt.pdf", ContentType: "application/pdf", Data: []byte("%PDF")}},
	})
	if err != nil || id != "<20260101.1@mg.example.com>" {
		t.Fatalf("Send = %q %v", id, err)
	}
	s1 := <-got
	if s1.path != "/v3/mg.example.com/messages" || s1.user != "api" || s1.pass != "key-123" {
		t.Fatalf("request = %+v", s1)
	}
	for field, want := range map[string]string{
		"from": "Shop <no-reply@example.com>", "to": "ana@example.com", "cc": "bo@example.com", "bcc": "audit@example.com",
		"subject": "Receipt 42", "text": "plain", "html": "<p>html</p>", "h:Reply-To": "help@example.com", "h:X-Entity-Ref": "inv_42", "o:tag": "receipts",
	} {
		if v := s1.form[field]; len(v) == 0 || v[0] != want {
			t.Errorf("%s = %v, want %q", field, v, want)
		}
	}
	if s1.files["receipt.pdf"] != "%PDF" {
		t.Errorf("attachment = %v", s1.files)
	}
}

func TestSenderRefusesAnInvalidMessageBeforeCallingTheAPI(t *testing.T) {
	a, got := newApp(t)
	if _, err := mailgun.Sender(a).Send(context.Background(), mail.Message{From: "a@example.com", Subject: "s", Text: "t"}); err == nil {
		t.Fatal("no recipient must be an error")
	}
	select {
	case <-got:
		t.Fatal("the API must not be called")
	default:
	}
}

func sign(ts, token string) string {
	m := hmac.New(sha256.New, []byte(signingKey))
	m.Write([]byte(ts + token))
	return hex.EncodeToString(m.Sum(nil))
}

func TestVerifyWebhookReturnsTheDeliveryEvent(t *testing.T) {
	a, _ := newApp(t)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := func(sig string) string {
		return `{"signature":{"timestamp":"` + ts + `","token":"tok1","signature":"` + sig + `"},
		 "event-data":{"event":"failed","severity":"permanent","id":"ev1","timestamp":1767225600.5,"recipient":"ana@example.com",
		 "message":{"headers":{"message-id":"20260101.1@mg.example.com"}},"delivery-status":{"code":550,"message":"mailbox unavailable"}}}`
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mailgun", strings.NewReader(body(sign(ts, "tok1"))))
	ev, err := mailgun.VerifyWebhook(a, req)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "failed" || ev.Severity != "permanent" || ev.Recipient != "ana@example.com" || ev.MessageID != "20260101.1@mg.example.com" ||
		ev.ID != "ev1" || ev.Time.IsZero() || !strings.Contains(string(ev.Raw), "mailbox unavailable") {
		t.Fatalf("event = %+v", ev)
	}
	forged := httptest.NewRequest(http.MethodPost, "/webhooks/mailgun", strings.NewReader(body("deadbeef")))
	if _, err := mailgun.VerifyWebhook(a, forged); gox.CodeOf(err) != gox.CodeUnauthenticated {
		t.Fatalf("forged = %v", err)
	}
}

func TestParseInboundReadsARoutedMessage(t *testing.T) {
	a, _ := newApp(t)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	build := func(sig string) *http.Request {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for k, v := range map[string]string{
			"timestamp": ts, "token": "tok2", "signature": sig,
			"sender": "ana@example.com", "recipient": "inbox@mg.example.com", "from": "Ana <ana@example.com>",
			"subject": "Re: Receipt 42", "body-plain": "thanks!\n> quoted", "stripped-text": "thanks!", "body-html": "<p>thanks!</p>",
			"Message-Id": "<reply-1@example.com>", "In-Reply-To": "<20260101.1@mg.example.com>",
		} {
			_ = w.WriteField(k, v)
		}
		f, _ := w.CreateFormFile("attachment-1", "photo.jpg")
		_, _ = f.Write([]byte("JPEG"))
		_ = w.Close()
		req := httptest.NewRequest(http.MethodPost, "/webhooks/mailgun/inbound", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		return req
	}
	in, err := mailgun.ParseInbound(a, build(sign(ts, "tok2")))
	if err != nil {
		t.Fatal(err)
	}
	if in.Sender != "ana@example.com" || in.Recipient != "inbox@mg.example.com" || in.From != "Ana <ana@example.com>" || in.Subject != "Re: Receipt 42" ||
		in.Text != "thanks!\n> quoted" || in.StrippedText != "thanks!" || in.HTML != "<p>thanks!</p>" ||
		in.MessageID != "<reply-1@example.com>" || in.InReplyTo != "<20260101.1@mg.example.com>" {
		t.Fatalf("inbound = %+v", in)
	}
	if len(in.Attachments) != 1 || in.Attachments[0].Filename != "photo.jpg" || string(in.Attachments[0].Data) != "JPEG" {
		t.Fatalf("attachments = %+v", in.Attachments)
	}
	if _, err := mailgun.ParseInbound(a, build("deadbeef")); gox.CodeOf(err) != gox.CodeUnauthenticated {
		t.Fatalf("forged = %v", err)
	}
}

func TestEnableNamesTheMissingVariablesAndFromPanics(t *testing.T) {
	old := os.Args
	os.Args = []string{"svc"}
	defer func() { os.Args = old }()
	_, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), mailgun.Enable())
	if err == nil || !strings.Contains(err.Error(), "SHOP_MAILGUN_API_KEY") {
		t.Fatalf("err = %v", err)
	}
	a, _ := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		if r := recover(); r != "gox: mailgun.From called but mailgun.Enable() was not passed to gox.New" {
			t.Fatalf("panic = %v", r)
		}
	}()
	mailgun.From(a)
}
