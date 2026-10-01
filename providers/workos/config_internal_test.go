package workos

import (
	"strings"
	"testing"
)

func TestIssuer(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"AuthKit default", Config{ClientID: "client_123"}, "https://api.workos.com/user_management/client_123"},
		{"emulator base URL", Config{ClientID: "client_123", BaseURL: "http://localhost:8001/"}, "http://localhost:8001/user_management/client_123"},
		{"explicit issuer wins", Config{ClientID: "client_123", BaseURL: "http://localhost:8001", Issuer: "https://auth.example.com"}, "https://auth.example.com"},
	}
	for _, c := range cases {
		if got := c.cfg.issuer(); got != c.want {
			t.Errorf("%s: issuer() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestValidateRejectsARelativeIssuer(t *testing.T) {
	c := Config{ClientID: "client_123", SecureCookies: "auto", CookieMaxAge: 1, Issuer: "api.workos.com"}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "WORKOS_ISSUER") {
		t.Fatalf("a relative WORKOS_ISSUER must fail validation naming it, got %v", err)
	}
}
