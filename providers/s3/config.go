package s3

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Config is the S3 config section: BILLING_S3_* under a service prefixed
// BILLING.
type Config struct {
	Bucket          string        `conf:"required"`
	Region          string        `conf:"default:us-east-1"`
	Endpoint        string        `conf:"help:base URL of an S3-compatible server (MinIO or R2); empty for AWS"`
	PathStyle       string        `conf:"default:auto,help:auto | true | false; auto uses path-style addressing when ENDPOINT is set"`
	AccessKeyID     string        `conf:"help:static credentials; leave empty for the AWS default chain (environment or instance role)"`
	SecretAccessKey string        `conf:"mask"`
	PresignExpiry   time.Duration `conf:"default:15m,help:lifetime of presigned URLs when a call sets none; at most 168h"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	var errs []error
	if c.Endpoint != "" {
		if u, err := url.Parse(c.Endpoint); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			errs = append(errs, fmt.Errorf("S3_ENDPOINT must be an absolute http(s) URL, got %q", c.Endpoint))
		}
	}
	if (c.AccessKeyID == "") != (c.SecretAccessKey == "") {
		errs = append(errs, errors.New("S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY must be set together"))
	}
	switch c.PathStyle {
	case "auto", "true", "false":
	default:
		errs = append(errs, fmt.Errorf("S3_PATH_STYLE must be auto, true or false, got %q", c.PathStyle))
	}
	if c.PresignExpiry <= 0 || c.PresignExpiry > 168*time.Hour {
		errs = append(errs, fmt.Errorf("S3_PRESIGN_EXPIRY must be between 1s and 168h (the longest S3 signs), got %v", c.PresignExpiry))
	}
	return errors.Join(errs...)
}

func (c *Config) pathStyle() bool {
	return c.PathStyle == "true" || (c.PathStyle == "auto" && c.Endpoint != "")
}
