package jobs

import (
	"errors"
	"fmt"
)

// Config is the JOBS config section: BILLING_JOBS_* under a service
// prefixed BILLING.
type Config struct {
	Work       bool `conf:"default:true,help:work jobs in this process; false makes it insert-only (an API next to a separate worker)"`
	MaxWorkers int  `conf:"default:10,help:jobs worked at once in this process"`
	Migrate    bool `conf:"default:true,help:create or upgrade River's tables at boot"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	var errs []error
	if c.MaxWorkers < 1 || c.MaxWorkers > 10000 {
		errs = append(errs, fmt.Errorf("JOBS_MAX_WORKERS must be between 1 and 10000, got %d", c.MaxWorkers))
	}
	return errors.Join(errs...)
}
