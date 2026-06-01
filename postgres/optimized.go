package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ardanlabs/conf/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DatabasePool represents an enhanced PostgreSQL connection pool with monitoring
type DatabasePool struct {
	*pgxpool.Pool
	config  Config
	metrics *DatabaseMetrics
	logger  *slog.Logger
}

// NewOptimized creates a new optimized PostgreSQL connection pool with monitoring
func NewOptimized(ctx context.Context, prefix string, logger *slog.Logger) (*DatabasePool, error) {
	var cfg Config

	// Parse configuration with defaults
	if _, err := conf.Parse(prefix, &cfg); err != nil {
		return nil, fmt.Errorf("parsing database config: %w", err)
	}

	// Apply defaults if not set
	if cfg.DatabasePoolMinSize == 0 || cfg.DatabasePoolMaxSize == 0 {
		defaults := DefaultConfig()
		if cfg.DatabasePoolMinSize == 0 {
			cfg.DatabasePoolMinSize = defaults.DatabasePoolMinSize
		}
		if cfg.DatabasePoolMaxSize == 0 {
			cfg.DatabasePoolMaxSize = defaults.DatabasePoolMaxSize
		}
	}

	// Parse pool configuration for pgxpool
	poolConfig, err := pgxpool.ParseConfig(cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("parsing pool config: %w", err)
	}

	// Apply additional optimizations
	poolConfig.MinConns = cfg.DatabasePoolMinSize
	poolConfig.MaxConns = cfg.DatabasePoolMaxSize
	poolConfig.MaxConnLifetime = cfg.DatabaseMaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.DatabaseMaxConnIdleTime
	poolConfig.HealthCheckPeriod = cfg.DatabaseHealthCheckPeriod

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	// Initialize metrics
	var metrics *DatabaseMetrics
	if cfg.DatabaseEnableMetrics {
		metrics = NewDatabaseMetrics()
	}

	dbPool := &DatabasePool{
		Pool:    pool,
		config:  cfg,
		metrics: metrics,
		logger:  logger,
	}

	// Start metrics collection if enabled
	if cfg.DatabaseEnableMetrics {
		go dbPool.startMetricsCollection(ctx)
	}

	// Log configuration
	if logger != nil {
		logger.Info("Database pool initialized",
			slog.String("host", cfg.DatabaseHost),
			slog.String("port", cfg.DatabasePort),
			slog.String("database", cfg.DatabaseName),
			slog.Int("min_connections", int(cfg.DatabasePoolMinSize)),
			slog.Int("max_connections", int(cfg.DatabasePoolMaxSize)),
			slog.Duration("max_conn_lifetime", cfg.DatabaseMaxConnLifetime),
			slog.Duration("max_conn_idle_time", cfg.DatabaseMaxConnIdleTime),
			slog.Duration("health_check_period", cfg.DatabaseHealthCheckPeriod),
		)
	}

	return dbPool, nil
}

// startMetricsCollection collects pool statistics periodically
func (db *DatabasePool) startMetricsCollection(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second) // Collect metrics every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			db.updatePoolMetrics()
		}
	}
}

// updatePoolMetrics updates Prometheus metrics with current pool statistics
func (db *DatabasePool) updatePoolMetrics() {
	if db.metrics == nil {
		return
	}

	stats := db.Stat()

	// Update pool connection metrics
	db.metrics.activeConnections.Set(float64(stats.AcquiredConns()))
	db.metrics.idleConnections.Set(float64(stats.IdleConns()))
	db.metrics.totalConnections.Set(float64(stats.TotalConns()))

	// Note: MaxConns() returns the maximum, not current waiting
	// For actual waiting connections, we'd need to track this separately
	maxConns := float64(stats.MaxConns())
	totalConns := float64(stats.TotalConns())

	// Estimate waiting connections (this is an approximation)
	estimatedWaiting := max(maxConns-totalConns, 0)
	db.metrics.waitingConnections.Set(estimatedWaiting)

	// Update lifecycle counters
	db.metrics.connectionsCreated.Add(float64(stats.NewConnsCount()))
	db.metrics.connectionsDestroyed.Add(float64(stats.MaxConns() - stats.TotalConns()))
}

// Ping performs a health check and updates metrics
func (db *DatabasePool) Ping(ctx context.Context) error {
	if db.metrics != nil {
		db.metrics.healthCheckTotal.Inc()
	}

	err := db.Pool.Ping(ctx)
	if err != nil && db.metrics != nil {
		db.metrics.healthCheckFailures.Inc()
	}

	return err
}

// GetMetrics returns the database metrics instance
func (db *DatabasePool) GetMetrics() *DatabaseMetrics {
	return db.metrics
}

// GetStats returns detailed connection pool statistics
func (db *DatabasePool) GetStats() map[string]interface{} {
	stats := db.Stat()

	return map[string]interface{}{
		"total_conns":                stats.TotalConns(),
		"acquired_conns":             stats.AcquiredConns(),
		"idle_conns":                 stats.IdleConns(),
		"max_conns":                  stats.MaxConns(),
		"new_conns_count":            stats.NewConnsCount(),
		"acquire_count":              stats.AcquireCount(),
		"acquire_duration":           stats.AcquireDuration(),
		"canceled_acquire_count":     stats.CanceledAcquireCount(),
		"constructing_conns":         stats.ConstructingConns(),
		"empty_acquire_count":        stats.EmptyAcquireCount(),
		"max_idle_destroy_count":     stats.MaxIdleDestroyCount(),
		"max_lifetime_destroy_count": stats.MaxLifetimeDestroyCount(),

		// Configuration
		"config": map[string]interface{}{
			"min_pool_size":       db.config.DatabasePoolMinSize,
			"max_pool_size":       db.config.DatabasePoolMaxSize,
			"max_conn_lifetime":   db.config.DatabaseMaxConnLifetime.String(),
			"max_conn_idle_time":  db.config.DatabaseMaxConnIdleTime.String(),
			"health_check_period": db.config.DatabaseHealthCheckPeriod.String(),
		},
	}
}

// RecordQuery records query metrics for monitoring
func (db *DatabasePool) RecordQuery(operation, table string, duration time.Duration, err error) {
	if db.metrics == nil {
		return
	}

	status := "success"
	if err != nil {
		status = "error"
	}

	db.metrics.queryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
	db.metrics.queryTotal.WithLabelValues(operation, table, status).Inc()
}

// RecordTransaction records transaction metrics
func (db *DatabasePool) RecordTransaction(duration time.Duration) {
	if db.metrics != nil {
		db.metrics.transactionDuration.Observe(duration.Seconds())
	}
}

// Close closes the database pool and cleans up resources
func (db *DatabasePool) Close() {
	if db.logger != nil {
		db.logger.Info("Closing database connection pool")
	}

	db.Pool.Close()
}

