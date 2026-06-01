package jwt

import (
	"fmt"

	"github.com/ardanlabs/conf/v3"
)

type Config struct {
	SecretKey string `conf:"env:JWT_SECRET_KEY,default:dev-secret-change-me"`
	Issuer    string `conf:"env:JWT_ISSUER,default:go-app"`
	Expiry    string `conf:"env:JWT_EXPIRY,default:24h"`
}

func LoadConfig(prefix string) (Config, error) {
	var cfg Config

	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parsing jwt config from prefix [%s]: %w", prefix, err)
	}

	return cfg, nil
}
