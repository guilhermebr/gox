package postgres

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// DatabaseMetrics tracks database connection pool performance
type DatabaseMetrics struct {
	// Pool metrics
	activeConnections  prometheus.Gauge
	idleConnections    prometheus.Gauge
	waitingConnections prometheus.Gauge
	totalConnections   prometheus.Gauge

	// Connection lifecycle metrics
	connectionsCreated   prometheus.Counter
	connectionsDestroyed prometheus.Counter
	connectionsFailed    prometheus.Counter

	// Query performance metrics
	queryDuration       prometheus.HistogramVec
	queryTotal          prometheus.CounterVec
	transactionDuration prometheus.Histogram

	// Health metrics
	healthCheckTotal    prometheus.Counter
	healthCheckFailures prometheus.Counter
}

// NewDatabaseMetrics creates database performance metrics
func NewDatabaseMetrics() *DatabaseMetrics {
	return &DatabaseMetrics{
		activeConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "database_active_connections",
			Help: "Number of active database connections",
		}),
		idleConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "database_idle_connections",
			Help: "Number of idle database connections",
		}),
		waitingConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "database_waiting_connections",
			Help: "Number of connections waiting for availability",
		}),
		totalConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "database_total_connections",
			Help: "Total number of database connections",
		}),
		connectionsCreated: promauto.NewCounter(prometheus.CounterOpts{
			Name: "database_connections_created_total",
			Help: "Total number of database connections created",
		}),
		connectionsDestroyed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "database_connections_destroyed_total",
			Help: "Total number of database connections destroyed",
		}),
		connectionsFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "database_connections_failed_total",
			Help: "Total number of failed database connection attempts",
		}),
		queryDuration: *promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "database_query_duration_seconds",
			Help:    "Database query execution duration",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~32s
		}, []string{"operation", "table"}),
		queryTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "database_queries_total",
			Help: "Total number of database queries executed",
		}, []string{"operation", "table", "status"}),
		transactionDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "database_transaction_duration_seconds",
			Help:    "Database transaction duration",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}),
		healthCheckTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "database_health_checks_total",
			Help: "Total number of database health checks performed",
		}),
		healthCheckFailures: promauto.NewCounter(prometheus.CounterOpts{
			Name: "database_health_check_failures_total",
			Help: "Total number of failed database health checks",
		}),
	}
}