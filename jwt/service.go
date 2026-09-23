package jwt

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors, usable with errors.Is.
var (
	// ErrInvalidToken indicates the token failed signature or validity checks.
	ErrInvalidToken = errors.New("jwt: invalid token")
	// ErrInvalidClaims indicates the token's claims could not be decoded.
	ErrInvalidClaims = errors.New("jwt: invalid token claims")
	// ErrNoSigningKey is returned by GenerateToken on a verify-only service.
	ErrNoSigningKey = errors.New("jwt: no signing key configured")

	// ErrReservedClaim indicates an application claim used a name the
	// standard claims already occupy.
	ErrReservedClaim = errors.New("jwt: reserved claim name")
)

// Claims are the JWT claims issued and validated by Service.
type Claims struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	AccountType string `json:"account_type"`
	jwt.RegisteredClaims

	// Raw holds every claim in a validated token, including the ones
	// without a field here: what an identity provider adds (org_id,
	// permissions, roles). It is not part of issued tokens.
	Raw map[string]any `json:"-"`

	// Extra holds the application's own claims to issue alongside the
	// standard ones. GenerateTokenWithClaims sets it; reading a token fills
	// Raw instead.
	Extra map[string]any `json:"-"`
}

// reservedClaims are the names Claims and RegisteredClaims already occupy, so
// an application claim cannot redefine the subject, the expiry or the issuer.
var reservedClaims = map[string]bool{
	"user_id": true, "email": true, "account_type": true,
	"iss": true, "sub": true, "aud": true,
	"exp": true, "nbf": true, "iat": true, "jti": true,
}

// MarshalJSON writes the standard claims and then the application's own, so a
// token carries both in one flat object.
func (c Claims) MarshalJSON() ([]byte, error) {
	type plain Claims
	b, err := json.Marshal(plain(c))
	if err != nil {
		return nil, err
	}
	if len(c.Extra) == 0 {
		return b, nil
	}
	var merged map[string]any
	if err := json.Unmarshal(b, &merged); err != nil {
		return nil, err
	}
	for k, v := range c.Extra {
		merged[k] = v
	}
	return json.Marshal(merged)
}

// UnmarshalJSON decodes the typed fields and keeps the whole claim set in Raw.
func (c *Claims) UnmarshalJSON(b []byte) error {
	type plain Claims
	if err := json.Unmarshal(b, (*plain)(c)); err != nil {
		return err
	}
	return json.Unmarshal(b, &c.Raw)
}

// Service signs and validates tokens with one algorithm: HS256 with a
// shared secret, or RS256 with an RSA key pair or an identity provider's
// key set. Tokens signed with any other algorithm are rejected.
type Service struct {
	method    jwt.SigningMethod
	signKey   any
	verifyKey jwt.Keyfunc
	issuer    string
	expiry    time.Duration
}

func staticKey(k any) jwt.Keyfunc { return func(*jwt.Token) (any, error) { return k, nil } }

// NewHS256 creates a Service that signs and verifies with a shared secret.
func NewHS256(secret []byte, issuer string, expiry time.Duration) *Service {
	return &Service{method: jwt.SigningMethodHS256, signKey: secret, verifyKey: staticKey(secret), issuer: issuer, expiry: expiry}
}

// NewRS256 creates a Service that verifies with pub and, when priv is not
// nil, signs with it. A service that only validates tokens issued
// elsewhere passes nil.
func NewRS256(priv *rsa.PrivateKey, pub *rsa.PublicKey, issuer string, expiry time.Duration) *Service {
	s := &Service{method: jwt.SigningMethodRS256, verifyKey: staticKey(pub), issuer: issuer, expiry: expiry}
	if priv != nil {
		s.signKey = priv
	}
	return s
}

// NewFromConfig builds a Service from a validated Config.
func NewFromConfig(cfg Config) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("jwt: %w", err)
	}
	if cfg.SecretKey != "" {
		return NewHS256([]byte(cfg.SecretKey), cfg.Issuer, cfg.Expiry), nil
	}
	if cfg.JWKSURL != "" {
		return NewJWKS(cfg.JWKSURL, cfg.Issuer, nil), nil
	}
	pub, err := parsePublicKey(cfg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("jwt: JWT_PUBLIC_KEY: %w", err)
	}
	var priv *rsa.PrivateKey
	if cfg.PrivateKey != "" {
		if priv, err = parsePrivateKey(cfg.PrivateKey); err != nil {
			return nil, fmt.Errorf("jwt: JWT_PRIVATE_KEY: %w", err)
		}
	}
	return NewRS256(priv, pub, cfg.Issuer, cfg.Expiry), nil
}

// GenerateToken issues a signed token for the given user.
func (s *Service) GenerateToken(userID, email, accountType string) (string, error) {
	return s.GenerateTokenWithClaims(userID, email, accountType, nil)
}

// GenerateTokenWithClaims issues a signed token that also carries the
// application's own claims, such as the organization a user acts for or a
// role. A name the standard claims already use is refused with
// ErrReservedClaim, so a caller cannot move the subject or the expiry through
// this map. Reading them back is claims.Raw["organization_id"].
func (s *Service) GenerateTokenWithClaims(userID, email, accountType string, extra map[string]any) (string, error) {
	if s.signKey == nil {
		return "", ErrNoSigningKey
	}
	for name := range extra {
		if reservedClaims[name] {
			return "", fmt.Errorf("%w: %s", ErrReservedClaim, name)
		}
	}
	now := time.Now()
	claims := &Claims{
		UserID:      userID,
		Email:       email,
		AccountType: accountType,
		Extra:       extra,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.Must(uuid.NewV7()).String(),
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiry)),
		},
	}
	token, err := jwt.NewWithClaims(s.method, claims).SignedString(s.signKey)
	if err != nil {
		return "", fmt.Errorf("jwt: sign: %w", err)
	}
	return token, nil
}

// ValidateToken parses and verifies a token, returning its claims. Errors
// wrap ErrInvalidToken or ErrInvalidClaims.
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	opts := []jwt.ParserOption{jwt.WithValidMethods([]string{s.method.Alg()})}
	if s.issuer != "" {
		opts = append(opts, jwt.WithIssuer(s.issuer))
	}
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, s.verifyKey, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidClaims
	}
	return claims, nil
}

// RefreshWindow is how close to expiry a token must be for RefreshToken to
// issue a new one.
const RefreshWindow = 5 * time.Minute

// RefreshToken returns a fresh token when the supplied one is within
// RefreshWindow of expiry, otherwise the original token unchanged.
func (s *Service) RefreshToken(tokenString string) (string, error) {
	claims, err := s.ValidateToken(tokenString)
	if err != nil {
		return "", err
	}
	if claims.ExpiresAt != nil && time.Until(claims.ExpiresAt.Time) > RefreshWindow {
		return tokenString, nil
	}
	return s.GenerateToken(claims.UserID, claims.Email, claims.AccountType)
}
