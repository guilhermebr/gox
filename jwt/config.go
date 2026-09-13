package jwt

import (
	"fmt"

	"github.com/ardanlabs/conf/v3"
)

// Config holds the JWT signing configuration. SecretKey is required and has no
// default: shipping a known signing key would let anyone forge valid tokens.
type Config struct {
	SecretKey string `conf:"env:JWT_SECRET_KEY,required,mask"`
	Issuer    string `conf:"env:JWT_ISSUER,default:go-app"`
	Expiry    string `conf:"env:JWT_EXPIRY,default:24h"`
}

// LoadConfig loads the JWT Config from environment variables prefixed with prefix.
func LoadConfig(prefix string) (Config, error) {
	var cfg Config

	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parsing jwt config from prefix [%s]: %w", prefix, err)
	}

	return cfg, nil
}
