package jwt

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwksMinRefresh is the shortest interval between two fetches of a key set,
// so tokens with unknown key ids cannot make the service hammer the
// provider. jwksMaxAge is how long a fetched set is trusted before it is
// fetched again, so a key the provider withdrew stops verifying.
var (
	jwksMinRefresh = time.Minute
	jwksMaxAge     = time.Hour
)

// NewJWKS creates a verify-only Service for tokens an identity provider
// signs with RS256: keys come from the provider's JWKS document at url, are
// cached, and follow rotation (an unknown key id triggers one rate-limited
// refetch). issuer must match the tokens' iss claim. A nil client uses one
// with a 10s timeout.
func NewJWKS(url, issuer string, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	set := &keySet{url: url, client: client}
	return &Service{method: jwt.SigningMethodRS256, verifyKey: set.key, issuer: issuer}
}

type keySet struct {
	url    string
	client *http.Client

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

// key is the jwt.Keyfunc: the cached key for the token's kid, refetching
// the set when the kid is unknown or the set is old.
func (s *keySet) key(t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("jwks: token has no kid header")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, known := s.keys[kid]
	age := time.Since(s.fetched)
	if (!known || age > jwksMaxAge) && age >= jwksMinRefresh {
		if err := s.fetch(); err != nil {
			if known { // a provider outage must not reject tokens we can verify
				return k, nil
			}
			return nil, err
		}
		k, known = s.keys[kid]
	}
	if !known {
		return nil, fmt.Errorf("jwks: no key with kid %q at %s", kid, s.url)
	}
	return k, nil
}

func (s *keySet) fetch() error {
	s.fetched = time.Now() // failures count too: the rate limit protects the provider
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, s.url, nil)
	if err != nil {
		return fmt.Errorf("jwks: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("jwks: fetch %s: %w", s.url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks: fetch %s: status %d", s.url, resp.StatusCode)
	}
	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("jwks: decode %s: %w", s.url, err)
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(k.N)
		e, errE := base64.RawURLEncoding.DecodeString(k.E)
		if errN != nil || errE != nil || len(n) == 0 || len(e) == 0 || len(e) > 4 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(keys) == 0 {
		return fmt.Errorf("jwks: no RSA signing keys at %s", s.url)
	}
	s.keys = keys
	return nil
}
