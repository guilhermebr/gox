package jwt

import (
	"testing"
	"time"
)

func TestNewService(t *testing.T) {
	svc := NewService("test-secret", "test-issuer", "1h")
	if svc.expiry != time.Hour {
		t.Errorf("expected expiry 1h, got %v", svc.expiry)
	}
}

func TestNewService_InvalidExpiry(t *testing.T) {
	svc := NewService("test-secret", "test-issuer", "invalid")
	if svc.expiry != 24*time.Hour {
		t.Errorf("expected default expiry 24h, got %v", svc.expiry)
	}
}

func TestNewServiceFromConfig(t *testing.T) {
	cfg := Config{
		SecretKey: "test-secret",
		Issuer:    "test-issuer",
		Expiry:    "2h",
	}
	svc := NewServiceFromConfig(cfg)
	if svc.expiry != 2*time.Hour {
		t.Errorf("expected expiry 2h, got %v", svc.expiry)
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	svc := NewService("test-secret-key-minimum-length", "test-issuer", "1h")

	token, err := svc.GenerateToken("user-123", "test@example.com", "admin")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if claims.UserID != "user-123" {
		t.Errorf("expected user_id user-123, got %s", claims.UserID)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", claims.Email)
	}
	if claims.AccountType != "admin" {
		t.Errorf("expected account_type admin, got %s", claims.AccountType)
	}
	if claims.Issuer != "test-issuer" {
		t.Errorf("expected issuer test-issuer, got %s", claims.Issuer)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	svc := NewService("test-secret", "test-issuer", "1h")

	_, err := svc.ValidateToken("invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	svc1 := NewService("secret-one", "issuer", "1h")
	svc2 := NewService("secret-two", "issuer", "1h")

	token, err := svc1.GenerateToken("user-1", "test@example.com", "user")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	_, err = svc2.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error when validating with wrong secret")
	}
}

func TestRefreshToken_StillFresh(t *testing.T) {
	svc := NewService("test-secret", "test-issuer", "1h")

	token, err := svc.GenerateToken("user-1", "test@example.com", "user")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	refreshed, err := svc.RefreshToken(token)
	if err != nil {
		t.Fatalf("failed to refresh token: %v", err)
	}

	// Token should not be refreshed since it's still fresh (>5 min remaining)
	if refreshed != token {
		t.Error("expected same token back since it's still fresh")
	}
}
