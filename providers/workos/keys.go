package workos

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/workos/workos-go/v10"
	"golang.org/x/sync/singleflight"
)

const (
	// keyProbeTTL is how long a fetched key set, or a failure to fetch it, is
	// reused: an outage costs one probe per window, not one per request.
	keyProbeTTL = 30 * time.Second
	// recoveryWindow is how long after the probe last found the key set down
	// a token naming a key the probe does not know is kept anyway: the
	// probe's set may predate a rotation the SDK has not fetched either.
	recoveryWindow = 2 * time.Minute
	// jwtLeeway is the SDK's allowance for clock skew on nbf.
	jwtLeeway = 60 * time.Second
	// maxKeySetBytes bounds the key set response the probe reads.
	maxKeySetBytes = 1 << 20
)

// keyProbe tells a token the SDK refused as invalid_jwt because the SDK had no
// signing key for it apart from a token that is really invalid. The SDK
// reports both the same way and returns no error. It drops its cached keys
// five minutes after the last successful fetch, and after a failed fetch (a
// WorkOS outage, a network blip, a request cancelled mid-fetch) it refuses
// every token for 30 seconds. Without the probe each of those would clear the
// session cookie of every user who reaches the service.
//
// The probe repeats the SDK's checks against a key set it fetches itself: a
// token that passes all of them could only have been refused for want of a
// key. The probe only ever decides whether to keep a cookie: a request it
// keeps a cookie for is still anonymous. Nothing is authenticated on its word.
type keyProbe struct {
	url      string
	issuer   string
	clientID string
	client   *http.Client
	group    singleflight.Group
	// unclaimed is set by each fetch and taken by exactly one caller, so an
	// outage is warned about once per fetch however many requests overlap.
	unclaimed atomic.Bool

	mu       sync.Mutex
	checked  time.Time
	keys     map[string]*rsa.PublicKey // nil when the last fetch failed
	lastDown time.Time                 // when a fetch last failed
}

func newKeyProbe(cfg *Config, issuer string, client *http.Client) *keyProbe {
	base := "https://api.workos.com"
	if cfg.BaseURL != "" {
		base = strings.TrimRight(cfg.BaseURL, "/")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &keyProbe{url: base + "/sso/jwks/" + cfg.ClientID, issuer: issuer, clientID: cfg.ClientID, client: client}
}

// verdict is what the probe concludes about a token the SDK refused.
type verdict int

const (
	keysDown      verdict = iota // the key set cannot be fetched: keep the session
	tokenValid                   // the token passes every check the SDK makes: keep the session
	tokenRejected                // the token fails a check the SDK makes: its refusal stands
)

// check decides about a token the SDK refused as invalid_jwt. fetched is true
// for exactly one caller per key set fetch, so callers can log an outage once
// per fetch rather than once per request.
func (p *keyProbe) check(ctx context.Context, accessToken string) (verdict, bool) {
	keys := p.keySet(ctx)
	fetched := p.unclaimed.CompareAndSwap(true, false)
	if keys == nil {
		return keysDown, fetched
	}
	return p.verify(keys, accessToken), fetched
}

// verify repeats the SDK's verification (workos-go verifyAccessToken): header,
// signature, issuer, not-before, audience. Expiry is not checked, as the SDK
// does not check it there either.
func (p *keyProbe) verify(keys map[string]*rsa.PublicKey, token string) verdict {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenRejected
	}
	var header struct {
		Alg  string          `json:"alg"`
		Kid  string          `json:"kid"`
		Crit json.RawMessage `json:"crit"`
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(raw, &header) != nil || header.Alg != "RS256" || header.Kid == "" || len(header.Crit) != 0 {
		return tokenRejected
	}
	key := keys[header.Kid]
	if key == nil {
		if p.recentlyDown() {
			return tokenValid
		}
		return tokenRejected
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return tokenRejected
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig) != nil {
		return tokenRejected
	}
	raw, err = base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenRejected
	}
	// The SDK decodes the payload into its typed claims; a token it cannot
	// decode is refused, so the probe decodes the same way.
	var claims struct {
		sdk.JWTClaims
		Iss string          `json:"iss"`
		Aud json.RawMessage `json:"aud"`
		Nbf int64           `json:"nbf"`
	}
	if json.Unmarshal(raw, &claims) != nil ||
		strings.TrimRight(p.issuer, "/") == "" || strings.TrimRight(claims.Iss, "/") != strings.TrimRight(p.issuer, "/") ||
		claims.Nbf > time.Now().Add(jwtLeeway).Unix() ||
		!audienceAllows(claims.Aud, p.clientID) {
		return tokenRejected
	}
	return tokenValid
}

// audienceAllows mirrors the SDK: no aud, or an aud (string or list) naming
// the client id.
func audienceAllows(aud json.RawMessage, clientID string) bool {
	if len(aud) == 0 {
		return true
	}
	var one string
	var many []string
	if json.Unmarshal(aud, &one) == nil && one != "" {
		many = []string{one}
	} else if json.Unmarshal(aud, &many) != nil {
		return false
	}
	for _, a := range many {
		if a == clientID {
			return true
		}
	}
	return false
}

func (p *keyProbe) recentlyDown() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.lastDown.IsZero() && time.Since(p.lastDown) < recoveryWindow
}

// keySet returns the cached key set, or fetches it when the cache is stale.
// Concurrent callers share one fetch; a caller whose request ends first gets
// nil (down) rather than waiting.
func (p *keyProbe) keySet(ctx context.Context) map[string]*rsa.PublicKey {
	p.mu.Lock()
	if !p.checked.IsZero() && time.Since(p.checked) < keyProbeTTL {
		keys := p.keys
		p.mu.Unlock()
		return keys
	}
	p.mu.Unlock()

	ch := p.group.DoChan("keys", func() (any, error) {
		keys := p.fetch(ctx)
		p.mu.Lock()
		p.keys, p.checked = keys, time.Now()
		if keys == nil {
			p.lastDown = p.checked
		}
		p.mu.Unlock()
		p.unclaimed.Store(true)
		return keys, nil
	})
	select {
	case res := <-ch:
		keys, _ := res.Val.(map[string]*rsa.PublicKey)
		return keys
	case <-ctx.Done():
		return nil
	}
}

// fetch reads the key set and keeps the keys the SDK would keep (workos-go
// fetchSessionJWKS); like the SDK, a duplicate kid fails the whole set.
func (p *keyProbe) fetch(ctx context.Context) map[string]*rsa.PublicKey {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return nil
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, maxKeySetBytes)).Decode(&set) != nil {
		return nil
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kid == "" || k.Kty != "RSA" || (k.Alg != "" && k.Alg != "RS256") || (k.Use != "" && k.Use != "sig") {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil || len(n) == 0 {
			continue
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		exponent := new(big.Int).SetBytes(e).Int64()
		modulus := new(big.Int).SetBytes(n)
		if exponent < 3 || exponent > 1<<31-1 || exponent%2 == 0 || modulus.BitLen() < 2048 {
			continue
		}
		if _, dup := keys[k.Kid]; dup {
			return nil
		}
		keys[k.Kid] = &rsa.PublicKey{N: modulus, E: int(exponent)}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// keysMayBeAtFault reports whether a token the SDK refused could have been
// refused only because the SDK had no key for it: invalid_jwt for a token
// naming the trusted issuer. Expiry says nothing here: the SDK checks the
// signature before expiry, so without keys an expired token is refused as
// invalid_jwt too, and is refreshed normally once the keys are back.
func keysMayBeAtFault(reason, trusted, accessToken string) bool {
	if reason != "invalid_jwt" {
		return false
	}
	return strings.TrimRight(peekClaims(accessToken).Iss, "/") == strings.TrimRight(trusted, "/")
}
