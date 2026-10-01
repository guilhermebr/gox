package workos

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const probeIssuer = "https://idp.test/"

func probeKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func signed(t *testing.T, key *rsa.PrivateKey, kid string, claims gojwt.MapClaims, header map[string]any) string {
	t.Helper()
	c := gojwt.MapClaims{"iss": probeIssuer, "exp": time.Now().Add(time.Hour).Unix()}
	for k, v := range claims {
		c[k] = v
	}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodRS256, c)
	tok.Header["kid"] = kid
	for k, v := range header {
		tok.Header[k] = v
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func jwk(kid string, k *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
	}
}

// keyServer serves keys, or 503 while down is set.
func keyServer(t *testing.T, down *atomic.Bool, keys ...map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down != nil && down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func probeFor(srv *httptest.Server) *keyProbe {
	cfg := &Config{BaseURL: srv.URL, ClientID: "client_123", Issuer: probeIssuer}
	return newKeyProbe(cfg, cfg.issuer(), srv.Client())
}

func (p *keyProbe) checkV(ctx context.Context, token string) verdict {
	v, _ := p.check(ctx, token)
	return v
}

func TestKeyProbeVerdicts(t *testing.T) {
	key, other := probeKey(t), probeKey(t)
	var down atomic.Bool
	srv := keyServer(t, &down, jwk("k1", &key.PublicKey))
	ctx := context.Background()
	recovering := func() *keyProbe { p := probeFor(srv); p.lastDown = time.Now(); return p }

	cases := []struct {
		name  string
		probe *keyProbe
		token string
		want  verdict
	}{
		// Everything the SDK checks passes: only missing keys can explain its refusal.
		{"good token, no outage seen", probeFor(srv), signed(t, key, "k1", nil, nil), tokenValid},
		{"good token with our audience", probeFor(srv), signed(t, key, "k1", gojwt.MapClaims{"aud": []string{"other", "client_123"}}, nil), tokenValid},
		{"good token, expired: the SDK checks the signature first", probeFor(srv), signed(t, key, "k1", gojwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()}, nil), tokenValid},
		// Something else the SDK checks fails: its refusal stands.
		{"signed by another key", probeFor(srv), signed(t, other, "k1", nil, nil), tokenRejected},
		{"another audience", probeFor(srv), signed(t, key, "k1", gojwt.MapClaims{"aud": "client_999"}, nil), tokenRejected},
		{"not valid yet", probeFor(srv), signed(t, key, "k1", gojwt.MapClaims{"nbf": time.Now().Add(time.Hour).Unix()}, nil), tokenRejected},
		{"another issuer", probeFor(srv), signed(t, key, "k1", gojwt.MapClaims{"iss": "https://other.test/"}, nil), tokenRejected},
		{"critical header", probeFor(srv), signed(t, key, "k1", nil, map[string]any{"crit": []string{"x"}}), tokenRejected},
		{"empty kid", probeFor(srv), signed(t, key, "", nil, nil), tokenRejected},
		{"garbage", probeFor(srv), "not.a.jwt", tokenRejected},
		// Unknown kid: inconclusive right after an outage (the set may predate a rotation).
		{"unknown kid, no outage seen", probeFor(srv), signed(t, key, "k9", nil, nil), tokenRejected},
		{"unknown kid, right after an outage", recovering(), signed(t, key, "k9", nil, nil), tokenValid},
	}
	for _, c := range cases {
		if got := c.probe.checkV(ctx, c.token); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}

	down.Store(true)
	p := probeFor(srv)
	if v := p.checkV(ctx, signed(t, key, "k1", nil, nil)); v != keysDown {
		t.Errorf("key set answering 503 = %v, want keysDown", v)
	}
	if p.lastDown.IsZero() {
		t.Error("a failed fetch must record the outage")
	}
}

func TestKeyProbeFiltersKeysLikeTheSDK(t *testing.T) {
	key := probeKey(t)
	small, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	weak := keyServer(t, nil, jwk("k1", &small.PublicKey))
	if v := probeFor(weak).checkV(ctx, signed(t, small, "k1", nil, nil)); v != keysDown {
		t.Errorf("only a 1024-bit key published (the SDK ignores it) = %v, want keysDown", v)
	}
	dup := keyServer(t, nil, jwk("k1", &key.PublicKey), jwk("k1", &key.PublicKey))
	if v := probeFor(dup).checkV(ctx, signed(t, key, "k1", nil, nil)); v != keysDown {
		t.Errorf("duplicate kids (the SDK rejects the whole set) = %v, want keysDown", v)
	}
}

func TestKeyProbeRefusesAnOversizedKeySet(t *testing.T) {
	key := probeKey(t)
	k := jwk("k1", &key.PublicKey)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A valid key followed by 2 MiB of padding: past the read limit the
		// set is not trusted, however good its first key.
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{k}, "pad": strings.Repeat("A", 2<<20)})
	}))
	defer srv.Close()
	if v := probeFor(srv).checkV(context.Background(), signed(t, key, "k1", nil, nil)); v != keysDown {
		t.Errorf("oversized key set = %v, want keysDown", v)
	}
}

func TestKeyProbeDecodesClaimsLikeTheSDK(t *testing.T) {
	key := probeKey(t)
	srv := keyServer(t, nil, jwk("k1", &key.PublicKey))
	// role must be a string for the SDK; a number makes it refuse the token.
	tok := signed(t, key, "k1", gojwt.MapClaims{"role": 7}, nil)
	if v := probeFor(srv).checkV(context.Background(), tok); v != tokenRejected {
		t.Errorf("a token the SDK cannot decode = %v, want tokenRejected", v)
	}
}

func TestKeyProbeWarnsOncePerFetch(t *testing.T) {
	var down atomic.Bool
	down.Store(true)
	srv := keyServer(t, &down)
	p := probeFor(srv)
	ctx := context.Background()

	results := make(chan bool, 10)
	for range 10 {
		go func() { _, fetched := p.check(ctx, "e30.e30.c2ln"); results <- fetched }()
	}
	claimed := 0
	for range 10 {
		if <-results {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("%d of 10 concurrent callers claimed the fetch, want exactly 1", claimed)
	}
}

func TestKeysMayBeAtFault(t *testing.T) {
	tok := func(iss string) string {
		claims, _ := json.Marshal(map[string]any{"iss": iss})
		return "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".c2ln"
	}
	cases := []struct {
		name, reason, token string
		want                bool
	}{
		{"trusted issuer", "invalid_jwt", tok("https://idp.test"), true},
		{"another issuer", "invalid_jwt", tok("https://other.test/"), false},
		{"another reason", "session_expired", tok("https://idp.test/"), false},
	}
	for _, c := range cases {
		if got := keysMayBeAtFault(c.reason, probeIssuer, c.token); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
