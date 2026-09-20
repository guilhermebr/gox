package stripe_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	sdk "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/providers/stripe"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T) {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
}

func TestTheClientCallsTheConfiguredAPIWithTheKey(t *testing.T) {
	var auth, path string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cus_1","object":"customer","email":"ana@example.com"}`))
	}))
	defer api.Close()
	setArgs(t)
	t.Setenv("SHOP_STRIPE_API_KEY", "sk_test_123")
	t.Setenv("SHOP_STRIPE_BASE_URL", api.URL)
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), stripe.Enable())
	if err != nil {
		t.Fatal(err)
	}
	cus, err := stripe.From(a).V1Customers.Retrieve(context.Background(), "cus_1", nil)
	if err != nil || cus.Email != "ana@example.com" {
		t.Fatalf("customer = %+v %v", cus, err)
	}
	if auth != "Bearer sk_test_123" || path != "/v1/customers/cus_1" {
		t.Fatalf("request = %q %q", auth, path)
	}
}

func TestVerifyWebhook(t *testing.T) {
	setArgs(t)
	t.Setenv("SHOP_STRIPE_API_KEY", "sk_test_123")
	t.Setenv("SHOP_STRIPE_WEBHOOK_SECRET", "whsec_test")
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), stripe.Enable())
	if err != nil {
		t.Fatal(err)
	}
	// An event from an API version other than the SDK's must still verify:
	// endpoints keep the version they were created with.
	payload := []byte(`{"id":"evt_1","object":"event","api_version":"2020-08-27","type":"invoice.paid","data":{"object":{"id":"in_1"}}}`)
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: payload, Secret: "whsec_test"})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(payload)))
	req.Header.Set("Stripe-Signature", signed.Header)
	ev, err := stripe.VerifyWebhook(a, req)
	if err != nil || ev.Type != sdk.EventType("invoice.paid") || ev.ID != "evt_1" || !strings.Contains(string(ev.Data.Raw), "in_1") {
		t.Fatalf("event = %+v %v", ev, err)
	}
	forged := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(`{"id":"evt_2"}`))
	forged.Header.Set("Stripe-Signature", signed.Header)
	if _, err := stripe.VerifyWebhook(a, forged); gox.CodeOf(err) != gox.CodeUnauthenticated {
		t.Fatalf("forged = %v", err)
	}
}

func TestEnableNamesTheMissingKeyAndFromPanics(t *testing.T) {
	setArgs(t)
	if _, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), stripe.Enable()); err == nil || !strings.Contains(err.Error(), "SHOP_STRIPE_API_KEY") {
		t.Fatalf("err = %v", err)
	}
	a, _ := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		if r := recover(); r != "gox: stripe.From called but stripe.Enable() was not passed to gox.New" {
			t.Fatalf("panic = %v", r)
		}
	}()
	stripe.From(a)
}
