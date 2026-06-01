package postgres

import (
	"fmt"
	"net/url"
	"runtime"
	"time"
)

// Config represents the configuration options for connecting to a Postgres database.
type Config struct {
	// Connection details
	DatabaseName     string `conf:"env:DATABASE_NAME,required"`
	DatabaseUser     string `conf:"env:DATABASE_USER,required"`
	DatabasePassword string `conf:"env:DATABASE_PASSWORD,required,mask"`
	DatabaseHost     string `conf:"env:DATABASE_HOST,default:localhost"`
	DatabasePort     string `conf:"env:DATABASE_PORT,default:5432"`
	DatabaseSSLMode  string `conf:"env:DATABASE_SSLMODE,default:disable"`

	// Connection pooling settings
	DatabasePoolMinSize       int32         `conf:"env:DATABASE_POOL_MIN_SIZE,default:5"`
	DatabasePoolMaxSize       int32         `conf:"env:DATABASE_POOL_MAX_SIZE,default:25"`
	DatabaseMaxConnLifetime   time.Duration `conf:"env:DATABASE_MAX_CONN_LIFETIME,default:1h"`
	DatabaseMaxConnIdleTime   time.Duration `conf:"env:DATABASE_MAX_CONN_IDLE_TIME,default:15m"`
	DatabaseHealthCheckPeriod time.Duration `conf:"env:DATABASE_HEALTH_CHECK_PERIOD,default:1m"`
	DatabaseConnectTimeout    time.Duration `conf:"env:DATABASE_CONNECT_TIMEOUT,default:30s"`

	// Performance settings
	DatabaseStatementCacheCapacity int32 `conf:"env:DATABASE_STATEMENT_CACHE_CAPACITY,default:512"`

	// Monitoring settings
	DatabaseEnableMetrics bool `conf:"env:DATABASE_ENABLE_METRICS,default:true"`
}

// DefaultConfig returns a production-optimized database configuration.
func DefaultConfig() Config {
	// Calculate optimal pool size based on CPU cores
	maxCores := runtime.NumCPU()
	maxPoolSize := int32(maxCores * 4) // 4 connections per CPU core
	if maxPoolSize < 10 {
		maxPoolSize = 10 // Minimum reasonable pool size
	}
	if maxPoolSize > 50 {
		maxPoolSize = 50 // Maximum to prevent resource exhaustion
	}

	minPoolSize := maxPoolSize / 5 // 20% of max pool size
	if minPoolSize < 2 {
		minPoolSize = 2 // Minimum to ensure availability
	}

	return Config{
		DatabaseHost:                   "localhost",
		DatabasePort:                   "5432",
		DatabaseSSLMode:                "disable",
		DatabasePoolMinSize:            minPoolSize,
		DatabasePoolMaxSize:            maxPoolSize,
		DatabaseMaxConnLifetime:        time.Hour,
		DatabaseMaxConnIdleTime:        15 * time.Minute,
		DatabaseHealthCheckPeriod:      time.Minute,
		DatabaseConnectTimeout:         30 * time.Second,
		DatabaseStatementCacheCapacity: 512,
		DatabaseEnableMetrics:          true,
	}
}

// ConnectionString returns the optimized postgres connection string.
// Credentials are URL-escaped to handle special characters safely.
func (c *Config) ConnectionString() string {
	sslMode := c.DatabaseSSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s&pool_min_conns=%d&pool_max_conns=%d&pool_max_conn_lifetime=%s&pool_max_conn_idle_time=%s&pool_health_check_period=%s&connect_timeout=%.0f&default_query_exec_mode=cache_statement",
		url.QueryEscape(c.DatabaseUser), url.QueryEscape(c.DatabasePassword), c.DatabaseHost, c.DatabasePort, c.DatabaseName,
		sslMode, c.DatabasePoolMinSize, c.DatabasePoolMaxSize,
		c.DatabaseMaxConnLifetime, c.DatabaseMaxConnIdleTime, c.DatabaseHealthCheckPeriod, c.DatabaseConnectTimeout.Seconds(),
	)
}

// DSN returns the Postgres Data Source Name (DSN) for use with pgxpool.
func (c *Config) DSN() string {
	sslMode := c.DatabaseSSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	return fmt.Sprintf(
		"user=%s password=%s host=%s port=%s dbname=%s pool_min_conns=%d pool_max_conns=%d pool_max_conn_lifetime=%s pool_max_conn_idle_time=%s pool_health_check_period=%s connect_timeout=%.0f sslmode=%s",
		c.DatabaseUser, c.DatabasePassword, c.DatabaseHost, c.DatabasePort, c.DatabaseName,
		c.DatabasePoolMinSize, c.DatabasePoolMaxSize, c.DatabaseMaxConnLifetime, c.DatabaseMaxConnIdleTime, c.DatabaseHealthCheckPeriod, c.DatabaseConnectTimeout.Seconds(), sslMode)
}
