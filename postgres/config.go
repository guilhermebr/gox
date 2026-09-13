package postgres

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Config is the POSTGRES config section: BILLING_POSTGRES_URL and friends
// under a service prefixed BILLING.
type Config struct {
	URL               string        `conf:"required,mask,help:postgres://user:pass@host:5432/db?sslmode=require"`
	MaxConns          int32         `conf:"default:10"`
	MinConns          int32         `conf:"default:2"`
	MaxConnLifetime   time.Duration `conf:"default:1h,help:jittered so a fleet does not recycle in lockstep"`
	MaxConnIdleTime   time.Duration `conf:"default:15m"`
	HealthCheckPeriod time.Duration `conf:"default:1m"`
	ConnectTimeout    time.Duration `conf:"default:10s"`
	Migrate           bool          `conf:"default:true,help:run embedded migrations at boot (WithMigrations)"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	var errs []error
	if u, err := url.Parse(c.URL); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("POSTGRES_URL must be a postgres:// URL, got %q", mask(c.URL)))
	}
	if c.MaxConns <= 0 {
		errs = append(errs, fmt.Errorf("POSTGRES_MAX_CONNS must be > 0, got %d", c.MaxConns))
	}
	if c.MinConns < 0 || c.MinConns > c.MaxConns {
		errs = append(errs, fmt.Errorf("POSTGRES_MIN_CONNS must be between 0 and MAX_CONNS (%d), got %d", c.MaxConns, c.MinConns))
	}
	if c.MaxConnLifetime <= 0 {
		errs = append(errs, fmt.Errorf("POSTGRES_MAX_CONN_LIFETIME must be > 0, got %v", c.MaxConnLifetime))
	}
	return errors.Join(errs...)
}

// mask hides the password of a URL in error messages.
func mask(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, has := u.User.Password(); has {
		u.User = url.UserPassword(u.User.Username(), "xxxxx")
	}
	return u.String()
}
