package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors returned by ValidateToken, usable with errors.Is.
var (
	// ErrInvalidToken indicates the token failed signature or validity checks.
	ErrInvalidToken = errors.New("invalid token")
	// ErrInvalidClaims indicates the token's claims could not be decoded.
	ErrInvalidClaims = errors.New("invalid token claims")
)

// Claims are the JWT claims issued and validated by Service.
type Claims struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	AccountType string `json:"account_type"`
	jwt.RegisteredClaims
}

// Service signs and validates HS256 JWTs with a fixed secret, issuer, and expiry.
type Service struct {
	secretKey []byte
	issuer    string
	expiry    time.Duration
}

// NewService creates a Service. expiry is a Go duration string (e.g. "24h");
// an unparseable value falls back to 24h.
func NewService(secretKey, issuer string, expiry string) Service {
	d, err := time.ParseDuration(expiry)
	if err != nil {
		d = 24 * time.Hour
	}
	return Service{
		secretKey: []byte(secretKey),
		issuer:    issuer,
		expiry:    d,
	}
}

// NewServiceFromConfig creates a Service from a pre-loaded Config.
func NewServiceFromConfig(cfg Config) Service {
	return NewService(cfg.SecretKey, cfg.Issuer, cfg.Expiry)
}

// GenerateToken issues a signed token for the given user.
func (s Service) GenerateToken(userID, email, accountType string) (string, error) {
	claims := &Claims{
		UserID:      userID,
		Email:       email,
		AccountType: accountType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    s.issuer,
			Subject:   userID,
			ID:        uuid.Must(uuid.NewV4()).String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secretKey)
}

// ValidateToken parses and verifies a token, returning its claims. It returns an
// error wrapping ErrInvalidToken or ErrInvalidClaims on failure.
func (s Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secretKey, nil
	})

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

// RefreshToken returns a fresh token if the supplied one is within 5 minutes of
// expiry, otherwise it returns the original token unchanged.
func (s Service) RefreshToken(tokenString string) (string, error) {
	claims, err := s.ValidateToken(tokenString)
	if err != nil {
		return "", fmt.Errorf("invalid token for refresh: %w", err)
	}

	// Check if token is close to expiration (within 5 minutes)
	if time.Until(claims.ExpiresAt.Time) > 5*time.Minute {
		return tokenString, nil // Token is still fresh
	}

	// Generate new token
	return s.GenerateToken(claims.UserID, claims.Email, claims.AccountType)
}
