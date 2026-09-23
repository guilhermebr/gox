package jwt_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/jwt"
)

func hs256(t *testing.T) *jwt.Service {
	t.Helper()
	return jwt.NewHS256([]byte("a-test-secret-of-at-least-32-bytes!!"), "billing", time.Hour)
}

func TestGenerateTokenWithClaimsRoundTripsTheApplicationsOwnClaims(t *testing.T) {
	s := hs256(t)
	token, err := s.GenerateTokenWithClaims("u-1", "a@example.com", "user", map[string]any{
		"organization_id": "org-7",
		"is_admin":        true,
	})
	if err != nil {
		t.Fatalf("GenerateTokenWithClaims: %v", err)
	}
	claims, err := s.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.UserID != "u-1" || claims.Email != "a@example.com" || claims.AccountType != "user" {
		t.Errorf("typed claims lost: %+v", claims)
	}
	if got := claims.Raw["organization_id"]; got != "org-7" {
		t.Errorf("organization_id = %v, want org-7", got)
	}
	if got := claims.Raw["is_admin"]; got != true {
		t.Errorf("is_admin = %v, want true", got)
	}
}

func TestGenerateTokenWithClaimsRejectsReservedNames(t *testing.T) {
	s := hs256(t)
	for _, name := range []string{"sub", "exp", "iss", "jti", "iat", "nbf", "user_id", "email", "account_type"} {
		t.Run(name, func(t *testing.T) {
			_, err := s.GenerateTokenWithClaims("u-1", "a@example.com", "user", map[string]any{name: "x"})
			if !errors.Is(err, jwt.ErrReservedClaim) {
				t.Fatalf("err = %v, want ErrReservedClaim", err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the error must name the claim, got %q", err)
			}
		})
	}
}

func TestGenerateTokenWithNoExtraClaimsMatchesGenerateToken(t *testing.T) {
	s := hs256(t)
	token, err := s.GenerateTokenWithClaims("u-1", "a@example.com", "user", nil)
	if err != nil {
		t.Fatalf("GenerateTokenWithClaims: %v", err)
	}
	claims, err := s.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.UserID != "u-1" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestGenerateTokenWithClaimsNeedsASigningKey(t *testing.T) {
	s := jwt.NewJWKS("https://idp.example/.well-known/jwks.json", "billing", nil)
	if _, err := s.GenerateTokenWithClaims("u-1", "a@example.com", "user", nil); !errors.Is(err, jwt.ErrNoSigningKey) {
		t.Fatalf("err = %v, want ErrNoSigningKey", err)
	}
}

func TestRefreshTokenKeepsTheApplicationsClaims(t *testing.T) {
	// A short expiry puts the token inside the refresh window straight away.
	s := jwt.NewHS256([]byte("a-test-secret-of-at-least-32-bytes!!"), "billing", time.Minute)
	token, err := s.GenerateTokenWithClaims("u-1", "a@example.com", "user", map[string]any{
		"organization_id": "org-7",
		"is_admin":        true,
	})
	if err != nil {
		t.Fatalf("GenerateTokenWithClaims: %v", err)
	}
	refreshed, err := s.RefreshToken(token)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	claims, err := s.ValidateToken(refreshed)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if got := claims.Raw["organization_id"]; got != "org-7" {
		t.Errorf("organization_id = %v, want org-7: a refresh must not drop the application's claims", got)
	}
	if got := claims.Raw["is_admin"]; got != true {
		t.Errorf("is_admin = %v, want true", got)
	}
}
