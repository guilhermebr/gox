package jwt

import (
	"fmt"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	AccountType string `json:"account_type"`
	jwt.RegisteredClaims
}

type Service struct {
	secretKey []byte
	issuer    string
	expiry    time.Duration
}

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

func NewServiceFromConfig(cfg Config) Service {
	return NewService(cfg.SecretKey, cfg.Issuer, cfg.Expiry)
}

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

func (s Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secretKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

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
