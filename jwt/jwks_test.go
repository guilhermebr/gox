package jwt_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jwt"
)

// idp is a fake identity provider: a JWKS endpoint and a signer.
type idp struct {
	t       *testing.T
	srv     *httptest.Server
	fetches atomic.Int32
	mu      sync.Mutex
	keys    map[string]*rsa.PrivateKey
	hidden  map[string]bool // signed with, but not published yet
	down    atomic.Bool
}

func newIDP(t *testing.T) *idp {
	t.Helper()
	p := &idp{t: t, keys: map[string]*rsa.PrivateKey{}, hidden: map[string]bool{}}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.fetches.Add(1)
		if p.down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		type jwk struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		var set struct {
			Keys []jwk `json:"keys"`
		}
		p.mu.Lock()
		for kid, k := range p.keys {
			if p.hidden[kid] {
				continue
			}
			set.Keys = append(set.Keys, jwk{
				Kty: "RSA", Kid: kid, Alg: "RS256", Use: "sig",
				N: base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
				E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
			})
		}
		p.mu.Unlock()
		// A key this package must skip rather than choke on.
		set.Keys = append(set.Keys, jwk{Kty: "EC", Kid: "ec-1", Alg: "ES256", Use: "sig"})
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *idp) rotate(kids ...string) {
	p.t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = map[string]*rsa.PrivateKey{}
	for _, kid := range kids {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			p.t.Fatal(err)
		}
		p.keys[kid] = k
	}
}

func (p *idp) publish(kid string, visible bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hidden[kid] = !visible
}

func (p *idp) token(kid string, claims gojwt.MapClaims) string {
	p.t.Helper()
	p.mu.Lock()
	k := p.keys[kid]
	p.mu.Unlock()
	tok := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(k)
	if err != nil {
		p.t.Fatal(err)
	}
	return s
}

func claimsFor(issuer string) gojwt.MapClaims {
	return gojwt.MapClaims{
		"iss": issuer, "sub": "user_01", "exp": time.Now().Add(time.Hour).Unix(),
		"org_id": "org_9", "permissions": []string{"invoices:read", "invoices:write"},
	}
}

func TestJWKSValidatesProviderTokensAndExposesEveryClaim(t *testing.T) {
	p := newIDP(t)
	p.rotate("k1")
	svc := jwt.NewJWKS(p.srv.URL, "https://idp.example", nil)

	claims, err := svc.ValidateToken(p.token("k1", claimsFor("https://idp.example")))
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user_01" {
		t.Fatalf("Subject = %q", claims.Subject)
	}
	if claims.Raw["org_id"] != "org_9" {
		t.Fatalf("Raw[org_id] = %v", claims.Raw["org_id"])
	}
	perms, _ := claims.Raw["permissions"].([]any)
	if len(perms) != 2 || perms[0] != "invoices:read" {
		t.Fatalf("Raw[permissions] = %v", claims.Raw["permissions"])
	}
	// The key set is cached: a second token costs no fetch.
	if _, err := svc.ValidateToken(p.token("k1", claimsFor("https://idp.example"))); err != nil {
		t.Fatal(err)
	}
	if n := p.fetches.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1", n)
	}
}

func TestJWKSRejectsWrongIssuerForeignKeysAndOtherAlgorithms(t *testing.T) {
	p := newIDP(t)
	p.rotate("k1")
	svc := jwt.NewJWKS(p.srv.URL, "https://idp.example", nil)

	if _, err := svc.ValidateToken(p.token("k1", claimsFor("https://evil.example"))); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("wrong issuer: %v", err)
	}
	other := newIDP(t)
	other.rotate("k1") // same kid, different key
	if _, err := svc.ValidateToken(other.token("k1", claimsFor("https://idp.example"))); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("foreign key: %v", err)
	}
	hs := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claimsFor("https://idp.example"))
	hs.Header["kid"] = "k1"
	forged, _ := hs.SignedString([]byte("the-public-key-as-a-secret-is-the-classic"))
	if _, err := svc.ValidateToken(forged); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("HS256 token accepted by a JWKS service: %v", err)
	}
	if _, err := svc.GenerateToken("u", "e", "a"); !errors.Is(err, jwt.ErrNoSigningKey) {
		t.Fatalf("GenerateToken on a JWKS service: %v", err)
	}
}

func TestJWKSFollowsKeyRotationWithoutHammeringTheProvider(t *testing.T) {
	defer jwt.SetJWKSMinRefresh(200 * time.Millisecond)()
	p := newIDP(t)
	p.rotate("k1", "k2") // key generation is slow: do it before the clock matters
	p.publish("k2", false)
	svc := jwt.NewJWKS(p.srv.URL, "https://idp.example", nil)
	if _, err := svc.ValidateToken(p.token("k1", claimsFor("https://idp.example"))); err != nil {
		t.Fatal(err)
	}

	// Unknown kids inside the rate-limit window do not refetch.
	p.publish("k2", true)
	for range 5 {
		if _, err := svc.ValidateToken(p.token("k2", claimsFor("https://idp.example"))); err == nil {
			t.Fatal("k2 accepted before any refetch could have happened")
		}
	}
	if n := p.fetches.Load(); n != 1 {
		t.Fatalf("fetches inside the window = %d, want 1", n)
	}

	time.Sleep(250 * time.Millisecond)
	if _, err := svc.ValidateToken(p.token("k2", claimsFor("https://idp.example"))); err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	if n := p.fetches.Load(); n != 2 {
		t.Fatalf("fetches after rotation = %d, want 2", n)
	}
}

func TestJWKSKeepsServingCachedKeysWhileTheProviderIsDown(t *testing.T) {
	defer jwt.SetJWKSMinRefresh(time.Millisecond)()
	p := newIDP(t)
	p.rotate("k1")
	svc := jwt.NewJWKS(p.srv.URL, "https://idp.example", nil)
	tok := p.token("k1", claimsFor("https://idp.example"))
	if _, err := svc.ValidateToken(tok); err != nil {
		t.Fatal(err)
	}
	p.down.Store(true)
	time.Sleep(5 * time.Millisecond)
	if _, err := svc.ValidateToken(tok); err != nil {
		t.Fatalf("cached key must still verify: %v", err)
	}
	p.rotate("k9")
	_, err := svc.ValidateToken(p.token("k9", claimsFor("https://idp.example")))
	if !errors.Is(err, jwt.ErrInvalidToken) || !strings.Contains(err.Error(), "jwks") {
		t.Fatalf("unknown kid while down: %v", err)
	}
}

func TestEnableBuildsAJWKSServiceFromConfig(t *testing.T) {
	setArgs(t)
	p := newIDP(t)
	p.rotate("k1")
	t.Setenv("BILLING_JWT_JWKS_URL", p.srv.URL)
	t.Setenv("BILLING_JWT_ISSUER", "https://idp.example")
	a, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jwt.Enable())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwt.From(a).ValidateToken(p.token("k1", claimsFor("https://idp.example"))); err != nil {
		t.Fatal(err)
	}
}

func TestConfigAllowsExactlyOneKeySource(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_JWT_JWKS_URL", "https://idp.example/jwks")
	t.Setenv("BILLING_JWT_SECRET_KEY", "config-secret-config-secret-config")
	_, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jwt.Enable())
	if err == nil || !strings.Contains(err.Error(), "exactly one of") {
		t.Fatalf("two key sources: %v", err)
	}

	t.Setenv("BILLING_JWT_SECRET_KEY", "")
	t.Setenv("BILLING_JWT_JWKS_URL", "idp.example/jwks")
	_, err = gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jwt.Enable())
	if err == nil || !strings.Contains(err.Error(), "JWT_JWKS_URL must be an absolute http(s) URL") {
		t.Fatalf("relative url: %v", err)
	}
}
