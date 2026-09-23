package temporal

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Config is the TEMPORAL config section: BILLING_TEMPORAL_* under a service
// prefixed BILLING.
type Config struct {
	Address        string        `conf:"default:localhost:7233,help:host:port of the Temporal frontend"`
	Namespace      string        `conf:"default:default"`
	APIKey         string        `conf:"mask,help:Temporal Cloud API key; implies TLS"`
	TLS            string        `conf:"default:auto,help:auto | true | false; auto is on with an API key or a client certificate"`
	TLSCert        string        `conf:"help:PEM client certificate for mTLS; needs TLS_KEY"`
	TLSKey         string        `conf:"mask,help:PEM client key for mTLS"`
	TLSCA          string        `conf:"env:TLS_CA,help:PEM root certificates that verify the server; the system pool is used when empty"`
	TLSServerName  string        `conf:"help:server name the server certificate must match; the address host is used when empty"`
	ConnectTimeout time.Duration `conf:"default:10s,help:how long the boot health check may take"`
	Work           bool          `conf:"default:true,help:run the registered workers in this process; false only starts workflows (an API next to a separate worker)"`
}

// Validate checks the section after loading.
func (c *Config) Validate() error {
	var errs []error
	if _, _, err := net.SplitHostPort(c.Address); err != nil || strings.Contains(c.Address, "://") {
		errs = append(errs, fmt.Errorf("TEMPORAL_ADDRESS must be host:port, got %q", c.Address))
	}
	switch c.TLS {
	case "auto", "true", "false":
	default:
		errs = append(errs, fmt.Errorf("TEMPORAL_TLS must be auto, true or false, got %q", c.TLS))
	}
	switch {
	case (c.TLSCert == "") != (c.TLSKey == ""):
		errs = append(errs, errors.New("TEMPORAL_TLS_CERT and TEMPORAL_TLS_KEY must be set together"))
	case c.TLSCert != "":
		if _, err := tls.X509KeyPair([]byte(c.TLSCert), []byte(c.TLSKey)); err != nil {
			errs = append(errs, fmt.Errorf("TEMPORAL_TLS_CERT and TEMPORAL_TLS_KEY: %w", err))
		}
	}
	if c.TLSCA != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(c.TLSCA)) {
		errs = append(errs, errors.New("TEMPORAL_TLS_CA must be one or more PEM certificates"))
	}
	if c.ConnectTimeout <= 0 {
		errs = append(errs, fmt.Errorf("TEMPORAL_CONNECT_TIMEOUT must be > 0, got %v", c.ConnectTimeout))
	}
	return errors.Join(errs...)
}

// tlsConfig returns nil for a plaintext connection.
func (c *Config) tlsConfig() *tls.Config {
	on := c.TLS == "true" || (c.TLS == "auto" && (c.APIKey != "" || c.TLSCert != "" || c.TLSCA != ""))
	if !on {
		return nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.TLSCert != "" {
		if pair, err := tls.X509KeyPair([]byte(c.TLSCert), []byte(c.TLSKey)); err == nil { // Validate already checked it
			cfg.Certificates = []tls.Certificate{pair}
		}
	}
	if c.TLSCA != "" {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM([]byte(c.TLSCA)) { // Validate already checked it
			cfg.RootCAs = pool
		}
	}
	cfg.ServerName = c.TLSServerName
	return cfg
}
