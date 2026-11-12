package postgres

import (
	"strings"
	"testing"
)

func TestSSEChannelNameIsAValidIdentifierPerService(t *testing.T) {
	if got := channelName("Billing-API"); !strings.HasPrefix(got, "gox_sse_billing_api_") {
		t.Fatalf("got %q", got)
	}
	if channelName("billing-api") == channelName("billing_api") {
		t.Fatal("billing-api and billing_api share a channel")
	}
	if got := channelName(strings.Repeat("x", 100)); len(got) != 63 {
		t.Fatalf("len = %d", len(got))
	}
}
