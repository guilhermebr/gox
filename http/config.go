package http

import (
	"fmt"
	"time"

	"github.com/ardanlabs/conf/v3"
)

type Config struct {
	Address           string        `conf:"env:ADDRESS,default:0.0.0.0:3000"`
	ReadHeaderTimeout time.Duration `conf:"env:READ_HEADER_TIMEOUT,default:60s"`
	ReadTimeout       time.Duration `conf:"env:READ_TIMEOUT,default:10s"`
	WriteTimeout      time.Duration `conf:"env:WRITE_TIMEOUT,default:10s"`
	IdleTimeout       time.Duration `conf:"env:IDLE_TIMEOUT,default:60s"`
	ShutdownTimeout   time.Duration `conf:"env:SHUTDOWN_TIMEOUT,default:20s"`
}

func LoadConfig(prefix string) (Config, error) {
	var cfg Config

	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parsing http config from prefix [%s]: %w", prefix, err)
	}

	return cfg, nil
}
