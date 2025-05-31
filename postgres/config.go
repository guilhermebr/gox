package postgres

import "fmt"

// Config represents the configuration options for connecting to a Postgres database.
type Config struct {
	DatabaseName        string `conf:"env:DATABASE_NAME,required"`
	DatabaseUser        string `conf:"env:DATABASE_USER,required"`
	DatabasePassword    string `conf:"env:DATABASE_PASSWORD,required,mask"`
	DatabaseHost        string `conf:"env:DATABASE_HOST_DIRECT,default:localhost"`
	DatabasePort        string `conf:"env:DATABASE_PORT_DIRECT,default:5432"`
	DatabaseSSLMode     string `conf:"env:DATABASE_SSLMODE,default:disable"`
	DatabasePoolMinSize int32  `conf:"env:DATABASE_POOL_MIN_SIZE,default:2"`
	DatabasePoolMaxSize int32  `conf:"env:DATABASE_POOL_MAX_SIZE,default:10"`
}

// ConnectionString returns the postgres connection string.
func (c *Config) ConnectionString() string {
	if c.DatabaseSSLMode == "" {
		c.DatabaseSSLMode = "disable"
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s&pool_min_conns=%d&pool_max_conns=%d",
		c.DatabaseUser, c.DatabasePassword, c.DatabaseHost, c.DatabasePort, c.DatabaseName,
		c.DatabaseSSLMode, c.DatabasePoolMinSize, c.DatabasePoolMaxSize,
	)
}

// DSN returns the Postgres Data Source Name (DSN) for use with pgxpool.
func (c *Config) DSN() string {
	if c.DatabaseSSLMode == "" {
		c.DatabaseSSLMode = "disable"
	}

	return fmt.Sprintf(
		"user=%s password=%s host=%s port=%s dbname=%s pool_min_conns=%d pool_max_conns=%d sslmode=%s",
		c.DatabaseUser, c.DatabasePassword, c.DatabaseHost, c.DatabasePort, c.DatabaseName,
		c.DatabasePoolMinSize, c.DatabasePoolMaxSize, c.DatabaseSSLMode)
}
