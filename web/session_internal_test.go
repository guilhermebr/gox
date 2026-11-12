package web

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestCodecRoundTripsAndAuthenticates(t *testing.T) {
	c, err := newCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"user_id": "u1", "token": "tok"}
	enc, err := c.encode(values)
	if err != nil {
		t.Fatal(err)
	}
	// The check is on the sealed bytes: the encoded text is random base64,
	// which contains a short value such as "u1" by chance now and then.
	sealed, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(`"user_id"`)) || bytes.Contains(sealed, []byte(`"token"`)) {
		t.Fatal("cookie value must be encrypted, not readable")
	}
	dec, err := c.decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec["user_id"] != "u1" || dec["token"] != "tok" {
		t.Fatalf("decoded = %v", dec)
	}

	sealed[len(sealed)-1] ^= 0x01
	if _, err := c.decode(base64.RawURLEncoding.EncodeToString(sealed)); err == nil {
		t.Fatal("tampered cookie accepted")
	}
	other, _ := newCodec([]byte("fedcba9876543210fedcba9876543210"))
	if _, err := other.decode(enc); err == nil {
		t.Fatal("cookie from another key accepted")
	}
}

func TestCodecRejectsShortKeysAndOversizedPayloads(t *testing.T) {
	if _, err := newCodec([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
	c, _ := newCodec([]byte("0123456789abcdef0123456789abcdef"))
	big := map[string]string{"blob": strings.Repeat("x", 5000)}
	if _, err := c.encode(big); err == nil {
		t.Fatal("payload over the cookie limit accepted")
	}
}
