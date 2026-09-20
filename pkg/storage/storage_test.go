package storage_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/storage"
)

func TestUploadTokensRoundTripAndBindTheirClaims(t *testing.T) {
	s, err := storage.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	in := storage.Upload{
		Key: "uploads/2026/ab12.pdf", Subject: "user_01", Expires: time.Now().Add(time.Hour).Truncate(time.Second),
		Meta: map[string]string{"filename": "lease.pdf", "organization": "org_9"},
	}
	token := s.Sign(in)
	if strings.ContainsAny(token, "+/= ") {
		t.Fatalf("the token must be URL-safe: %q", token)
	}
	got, err := s.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != in.Key || got.Subject != in.Subject || !got.Expires.Equal(in.Expires) || got.Meta["organization"] != "org_9" {
		t.Fatalf("got %+v", got)
	}
}

func TestUploadTokensRejectTamperingOtherKeysAndExpiry(t *testing.T) {
	s, _ := storage.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	other, _ := storage.NewSigner([]byte("ffffffffffffffffffffffffffffffff"))
	token := s.Sign(storage.Upload{Key: "k", Subject: "user_01", Expires: time.Now().Add(time.Hour)})

	payload, sig, _ := strings.Cut(token, ".")
	for name, bad := range map[string]string{
		"tampered payload": "A" + payload[1:] + "." + sig,
		"no signature":     payload,
		"garbage":          "not-a-token",
		"empty":            "",
	} {
		if _, err := s.Verify(bad); !errors.Is(err, storage.ErrInvalidToken) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := other.Verify(token); !errors.Is(err, storage.ErrInvalidToken) {
		t.Errorf("another secret: %v", err)
	}
	expired := s.Sign(storage.Upload{Key: "k", Expires: time.Now().Add(-time.Second)})
	if _, err := s.Verify(expired); !errors.Is(err, storage.ErrExpiredToken) {
		t.Errorf("expired: %v", err)
	}
	if _, err := storage.NewSigner([]byte("short")); err == nil {
		t.Error("a short secret must be refused")
	}
}
