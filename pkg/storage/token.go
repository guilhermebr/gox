package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Token errors, usable with errors.Is.
var (
	ErrInvalidToken = errors.New("storage: invalid upload token")
	ErrExpiredToken = errors.New("storage: expired upload token")
)

// Upload is what an upload token vouches for: which object a client was
// allowed to write, who asked, and until when the token can be redeemed.
type Upload struct {
	Key     string            `json:"k"`
	Subject string            `json:"s,omitempty"` // who may redeem it: a user or actor id
	Expires time.Time         `json:"e"`
	Meta    map[string]string `json:"m,omitempty"` // filename, content type, tenant: whatever redeeming needs
}

// Signer issues and verifies upload tokens (HMAC-SHA256). The handshake:
// the service presigns a PUT for a key it chose and returns the URL with
// Sign(Upload{...}); the client uploads, then sends the token back with the
// form it belongs to; the service calls Verify, checks Subject against the
// caller, and attaches Key. The client never names a key itself.
type Signer struct {
	secret []byte
}

// NewSigner needs a secret of at least 32 bytes.
func NewSigner(secret []byte) (*Signer, error) {
	if len(secret) < 32 {
		return nil, errors.New("storage: the upload token secret must be at least 32 bytes")
	}
	return &Signer{secret: secret}, nil
}

// Sign returns the URL-safe token for u.
func (s *Signer) Sign(u Upload) string {
	u.Expires = u.Expires.UTC().Truncate(time.Second)
	payload, _ := json.Marshal(u) // a struct of strings and a time cannot fail
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + base64.RawURLEncoding.EncodeToString(s.mac(body))
}

// Verify returns the claims of a token this Signer issued. Errors wrap
// ErrInvalidToken or ErrExpiredToken.
func (s *Signer) Verify(token string) (Upload, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" {
		return Upload{}, ErrInvalidToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, s.mac(body)) {
		return Upload{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Upload{}, ErrInvalidToken
	}
	var u Upload
	if err := json.Unmarshal(payload, &u); err != nil || u.Key == "" {
		return Upload{}, ErrInvalidToken
	}
	if time.Now().After(u.Expires) {
		return Upload{}, ErrExpiredToken
	}
	return u, nil
}

func (s *Signer) mac(body string) []byte {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(body))
	return m.Sum(nil)
}
