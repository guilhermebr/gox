package jwt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// Config is the JWT config section: BILLING_JWT_* under a service prefixed
// BILLING. Set SECRET_KEY for HS256, or PUBLIC_KEY (and PRIVATE_KEY on the
// service that signs) for RS256.
type Config struct {
	SecretKey  string        `conf:"mask,help:HS256 secret; set this or PUBLIC_KEY"`
	PublicKey  string        `conf:"help:PEM RSA public key for RS256 verification"`
	PrivateKey string        `conf:"mask,help:PEM RSA private key for RS256 signing; omit on verify-only services"`
	Issuer     string        `conf:"default:gox"`
	Expiry     time.Duration `conf:"default:24h"`
}

// Validate checks that exactly one algorithm is configured and that the
// PEM keys parse.
func (c *Config) Validate() error {
	var errs []error
	switch {
	case c.SecretKey == "" && c.PublicKey == "":
		errs = append(errs, errors.New("JWT_SECRET_KEY (HS256) or JWT_PUBLIC_KEY (RS256) is required"))
	case c.SecretKey != "" && c.PublicKey != "":
		errs = append(errs, errors.New("set JWT_SECRET_KEY or JWT_PUBLIC_KEY, not both"))
	}
	if c.SecretKey != "" && len(c.SecretKey) < 32 {
		errs = append(errs, errors.New("JWT_SECRET_KEY must be at least 32 bytes"))
	}
	if c.PublicKey != "" {
		if _, err := parsePublicKey(c.PublicKey); err != nil {
			errs = append(errs, fmt.Errorf("JWT_PUBLIC_KEY: %w", err))
		}
	}
	if c.PrivateKey != "" {
		if _, err := parsePrivateKey(c.PrivateKey); err != nil {
			errs = append(errs, fmt.Errorf("JWT_PRIVATE_KEY: %w", err))
		}
	}
	if c.Expiry <= 0 {
		errs = append(errs, fmt.Errorf("JWT_EXPIRY must be > 0, got %v", c.Expiry))
	}
	return errors.Join(errs...)
}

func parsePublicKey(pemText string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("not PEM encoded")
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("not an RSA public key")
	}
	key, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse RSA public key: %w", err)
	}
	return key, nil
}

func parsePrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("not PEM encoded")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse RSA private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an RSA private key")
	}
	return rsaKey, nil
}
