package resend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/mail"
)

// fakeAPI answers POST /emails with one status per call (the last repeats).
type fakeAPI struct {
	mu       sync.Mutex
	calls    int
	bodies   []map[string]any
	headers  []http.Header
	statuses []int
	retry    map[int]string // Retry-After per call (1-based)
	srv      *httptest.Server
}

func newFake(t *testing.T, statuses ...int) *fakeAPI {
	t.Helper()
	f := &fakeAPI{statuses: statuses, retry: map[int]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.calls++
		n := f.calls
		f.bodies = append(f.bodies, body)
		f.headers = append(f.headers, r.Header.Clone())
		st := f.statuses[min(n, len(f.statuses))-1]
		ra := f.retry[n]
		f.mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/emails" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(st)
		if st == http.StatusOK {
			_, _ = w.Write([]byte(`{"id":"re_abc123"}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"err","message":"nope"}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// newApp builds a service prefixed SHOP with the provider pointed at base.
func newApp(t *testing.T, apiKey, base string) *gox.App {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
	t.Setenv("SHOP_RESEND_API_KEY", apiKey)
	t.Setenv("SHOP_RESEND_BASE_URL", base)
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), Enable())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// send returns the app's Sender with sleeping replaced by a record of the waits.
func (f *fakeAPI) sender(t *testing.T, sleeps *[]time.Duration) mail.Sender {
	t.Helper()
	return stubSleep(t, newApp(t, "re_test_key", f.srv.URL), sleeps)
}

func stubSleep(t *testing.T, a *gox.App, sleeps *[]time.Duration) mail.Sender {
	t.Helper()
	s, ok := Sender(a).(*sender)
	if !ok {
		t.Fatalf("Sender is %T", Sender(a))
	}
	s.backoff = 10 * time.Millisecond
	s.sleep = func(_ context.Context, d time.Duration) error {
		if sleeps != nil {
			*sleeps = append(*sleeps, d)
		}
		return nil
	}
	return s
}

func msg() mail.Message {
	return mail.Message{
		From: "Shop <no-reply@example.com>", To: []string{"ana@example.com"},
		Subject: "Receipt 42", Text: "plain", HTML: "<p>html</p>",
		Headers: map[string]string{HeaderIdempotencyKey: "receipt/42", "X-Entity-Ref": "inv_42"},
	}
}

func TestSenderDeliversTheMessageAndMovesTheIdempotencyKeyToTheRequest(t *testing.T) {
	f := newFake(t, 200)
	m := msg()
	m.Cc, m.Bcc, m.ReplyTo, m.Tags = []string{"bo@example.com"}, []string{"audit@example.com"}, "help@example.com", []string{"receipts"}
	m.Attachments = []mail.Attachment{{Filename: "receipt.pdf", ContentType: "application/pdf", Data: []byte("%PDF")}}
	id, err := f.sender(t, nil).Send(context.Background(), m)
	if err != nil || id != "re_abc123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if f.calls != 1 {
		t.Fatalf("calls = %d", f.calls)
	}
	h, b := f.headers[0], f.bodies[0]
	if h.Get("Authorization") != "Bearer re_test_key" {
		t.Errorf("authorization = %q", h.Get("Authorization"))
	}
	if h.Get("Idempotency-Key") != "receipt/42" {
		t.Errorf("idempotency key = %q", h.Get("Idempotency-Key"))
	}
	if b["from"] != "Shop <no-reply@example.com>" || b["subject"] != "Receipt 42" || b["text"] != "plain" || b["html"] != "<p>html</p>" || b["reply_to"] != "help@example.com" {
		t.Errorf("payload = %v", b)
	}
	for field, want := range map[string]string{"to": "ana@example.com", "cc": "bo@example.com", "bcc": "audit@example.com"} {
		if got, _ := b[field].([]any); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %v", field, b[field])
		}
	}
	hdrs, _ := b["headers"].(map[string]any)
	if hdrs["X-Entity-Ref"] != "inv_42" {
		t.Errorf("extra headers = %v", hdrs)
	}
	if _, leaked := hdrs[HeaderIdempotencyKey]; leaked {
		t.Error("the idempotency key must not travel as an email header")
	}
	if tags, _ := b["tags"].([]any); len(tags) != 1 {
		t.Errorf("tags = %v", b["tags"])
	}
	att, _ := b["attachments"].([]any)
	if len(att) != 1 || att[0].(map[string]any)["filename"] != "receipt.pdf" {
		t.Errorf("attachments = %v", b["attachments"])
	}
}

func TestSendRetries5xxWithTheSameKeyAndExponentialBackoff(t *testing.T) {
	f := newFake(t, 500, 503, 200)
	var sleeps []time.Duration
	id, err := f.sender(t, &sleeps).Send(context.Background(), msg())
	if err != nil || id != "re_abc123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if f.calls != 3 {
		t.Fatalf("calls = %d, want 3", f.calls)
	}
	for i, h := range f.headers {
		if h.Get("Idempotency-Key") != "receipt/42" {
			t.Errorf("attempt %d key = %q", i+1, h.Get("Idempotency-Key"))
		}
	}
	if len(sleeps) != 2 || sleeps[1] < 2*10*time.Millisecond || sleeps[1] <= sleeps[0] {
		t.Errorf("backoff is not exponential: %v", sleeps)
	}
}

func TestSendGivesUpAfterThreeAttempts(t *testing.T) {
	f := newFake(t, 502)
	_, err := f.sender(t, nil).Send(context.Background(), msg())
	if err == nil || !strings.HasPrefix(err.Error(), "resend: send: ") {
		t.Fatalf("err = %v", err)
	}
	if f.calls != 3 {
		t.Fatalf("calls = %d, want 3", f.calls)
	}
}

func TestSendRetries429AndHonorsRetryAfter(t *testing.T) {
	f := newFake(t, 429, 200)
	f.retry[1] = "2"
	var sleeps []time.Duration
	if _, err := f.sender(t, &sleeps).Send(context.Background(), msg()); err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 || len(sleeps) != 1 || sleeps[0] < 2*time.Second {
		t.Fatalf("calls=%d sleeps=%v: Retry-After: 2 was not honored", f.calls, sleeps)
	}
}

func TestSendDoesNotWaitForAHugeRetryAfter(t *testing.T) {
	f := newFake(t, 429)
	f.retry[1] = "3600"
	if _, err := f.sender(t, nil).Send(context.Background(), msg()); err == nil {
		t.Fatal("expected an error")
	}
	if f.calls != 1 {
		t.Fatalf("calls = %d, want 1", f.calls)
	}
}

func TestSendNeverRetriesOtherClientErrors(t *testing.T) {
	for _, st := range []int{400, 401, 403, 404, 409, 422} {
		f := newFake(t, st)
		if _, err := f.sender(t, nil).Send(context.Background(), msg()); err == nil {
			t.Fatalf("%d: expected an error", st)
		}
		if f.calls != 1 {
			t.Errorf("%d: calls = %d, want 1 (no retry)", st, f.calls)
		}
	}
}

func TestSendRetriesNetworkErrors(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n < 3 {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close() // connection dropped mid-request
			return
		}
		_, _ = w.Write([]byte(`{"id":"re_net"}`))
	}))
	defer srv.Close()
	id, err := stubSleep(t, newApp(t, "k", srv.URL), nil).Send(context.Background(), msg())
	if err != nil || id != "re_net" || calls != 3 {
		t.Fatalf("id=%q err=%v calls=%d", id, err, calls)
	}
}

func TestSendStopsWhenTheContextIsDone(t *testing.T) {
	f := newFake(t, 500)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.sender(t, nil).Send(ctx, msg()); err == nil {
		t.Fatal("expected an error")
	}
	if f.calls > 1 {
		t.Fatalf("calls = %d", f.calls)
	}
}

func TestSendValidatesTheMessageBeforeCallingTheAPI(t *testing.T) {
	f := newFake(t, 200)
	m := msg()
	m.To = nil
	if _, err := f.sender(t, nil).Send(context.Background(), m); err == nil || !strings.Contains(err.Error(), "To") {
		t.Fatalf("err = %v", err)
	}
	if f.calls != 0 {
		t.Fatal("an invalid message must not reach the API")
	}
}

func TestWithoutAnAPIKeyTheProviderIsOffAndSendSaysSo(t *testing.T) {
	f := newFake(t, 200)
	a := newApp(t, "", f.srv.URL)
	if Configured(a) {
		t.Fatal("Configured must be false without RESEND_API_KEY")
	}
	if _, err := Sender(a).Send(context.Background(), msg()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	if f.calls != 0 {
		t.Fatal("an unconfigured sender must not call the API")
	}
	if !Configured(newApp(t, "re_test_key", f.srv.URL)) {
		t.Fatal("Configured must be true with RESEND_API_KEY")
	}
}

func TestFromIsTheSDKClientPointedAtTheConfiguredBase(t *testing.T) {
	f := newFake(t, 200)
	if got := From(newApp(t, "re_test_key", f.srv.URL)).BaseURL.String(); got != f.srv.URL+"/" {
		t.Fatalf("base = %q", got)
	}
}

func TestABaseURLThatIsNotAbsoluteFailsAtStartNamingTheVariable(t *testing.T) {
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
	t.Setenv("SHOP_RESEND_BASE_URL", "api.resend.com")
	_, err := gox.New("shop", gox.WithoutAdminServer(), Enable())
	if err == nil || !strings.Contains(err.Error(), "RESEND_BASE_URL") {
		t.Fatalf("err = %v", err)
	}
}
