package supabase

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/supabase-community/gotrue-go/types"
)

func TestSignUpWithEmail_NilClient(t *testing.T) {
	_, err := SignUpWithEmail(context.Background(), nil, "a@b.com", "pass", nil)
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if err != ErrNilClient {
		t.Fatalf("expected ErrNilClient, got: %v", err)
	}
}

func TestSignInWithEmail_NilClient(t *testing.T) {
	_, err := SignInWithEmail(context.Background(), nil, "a@b.com", "pass")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if err != ErrNilClient {
		t.Fatalf("expected ErrNilClient, got: %v", err)
	}
}

func TestGetUserFromToken_NilClient(t *testing.T) {
	_, err := GetUserFromToken(context.Background(), nil, "tok")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if err != ErrNilClient {
		t.Fatalf("expected ErrNilClient, got: %v", err)
	}
}

func TestAdminDeleteUser_NilClient(t *testing.T) {
	err := AdminDeleteUser(context.Background(), nil, uuid.New().String())
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if err != ErrNilClient {
		t.Fatalf("expected ErrNilClient, got: %v", err)
	}
}

func TestAdminDeleteUser_InvalidUUID(t *testing.T) {
	// Create a real client so the nil check passes.
	client, err := NewFromConfig(Config{
		URL: "https://test.supabase.co",
		Key: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = AdminDeleteUser(context.Background(), client, "not-a-uuid")
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}
}

func TestUserInfoFromSDK_Nil(t *testing.T) {
	info := userInfoFromSDK(nil)
	if info != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestUserInfoFromSDK_BasicConversion(t *testing.T) {
	now := time.Now()
	id := uuid.New()
	emailConfirmed := now.Add(-time.Hour)
	lastSignIn := now.Add(-time.Minute)

	u := &types.User{
		ID:               id,
		Email:            "test@example.com",
		Phone:            "+1234567890",
		Role:             "authenticated",
		EmailConfirmedAt: &emailConfirmed,
		PhoneConfirmedAt: nil,
		UserMetadata: map[string]interface{}{
			"name": "Test User",
		},
		AppMetadata: map[string]interface{}{
			"provider": "email",
		},
		LastSignInAt: &lastSignIn,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	info := userInfoFromSDK(u)
	if info == nil {
		t.Fatal("expected non-nil UserInfo")
	}

	if info.ID != id.String() {
		t.Errorf("ID: got %q, want %q", info.ID, id.String())
	}
	if info.Email != "test@example.com" {
		t.Errorf("Email: got %q, want %q", info.Email, "test@example.com")
	}
	if info.Phone != "+1234567890" {
		t.Errorf("Phone: got %q, want %q", info.Phone, "+1234567890")
	}
	if info.Role != "authenticated" {
		t.Errorf("Role: got %q, want %q", info.Role, "authenticated")
	}
	if !info.EmailConfirmed {
		t.Error("expected EmailConfirmed to be true")
	}
	if info.PhoneConfirmed {
		t.Error("expected PhoneConfirmed to be false")
	}
	if info.UserMetadata["name"] != "Test User" {
		t.Errorf("UserMetadata[name]: got %v, want %q", info.UserMetadata["name"], "Test User")
	}
	if info.AppMetadata["provider"] != "email" {
		t.Errorf("AppMetadata[provider]: got %v, want %q", info.AppMetadata["provider"], "email")
	}
	if info.LastSignInAt == nil {
		t.Error("expected LastSignInAt to be non-nil")
	}
}

func TestIsConfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{
			name: "valid config",
			cfg:  Config{URL: "https://test.supabase.co", Key: "test-key"},
			want: true,
		},
		{
			name: "empty URL",
			cfg:  Config{URL: "", Key: "test-key"},
			want: false,
		},
		{
			name: "empty Key",
			cfg:  Config{URL: "https://test.supabase.co", Key: ""},
			want: false,
		},
		{
			name: "whitespace URL",
			cfg:  Config{URL: "  ", Key: "test-key"},
			want: false,
		},
		{
			name: "placeholder URL",
			cfg:  Config{URL: "your-url-here", Key: "test-key"},
			want: false,
		},
		{
			name: "placeholder Key",
			cfg:  Config{URL: "https://test.supabase.co", Key: "your-key-here"},
			want: false,
		},
		{
			name: "placeholder TODO",
			cfg:  Config{URL: "TODO", Key: "test-key"},
			want: false,
		},
		{
			name: "placeholder case insensitive",
			cfg:  Config{URL: "YOUR-URL-HERE", Key: "test-key"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsConfigured()
			if got != tt.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}
