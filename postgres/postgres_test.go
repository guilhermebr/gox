package postgres

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		envVars map[string]string
		wantErr bool
	}{
		{
			name:   "Valid configuration",
			prefix: "DB",
			envVars: map[string]string{
				"DB_DATABASE_HOST":          "localhost",
				"DB_DATABASE_PORT":          "5432",
				"DB_DATABASE_USER":          "test",
				"DB_DATABASE_PASSWORD":      "test",
				"DB_DATABASE_NAME":          "testdb",
				"DB_DATABASE_SSLMODE":       "disable",
				"DB_DATABASE_POOL_MIN_SIZE": "2",
				"DB_DATABASE_POOL_MAX_SIZE": "10",
			},
			wantErr: false,
		},
		{
			name:   "Missing required configuration",
			prefix: "DB",
			envVars: map[string]string{
				"DB_DATABASE_HOST": "localhost",
				// Missing other required fields
			},
			wantErr: true,
		},
		{
			name:   "Invalid port",
			prefix: "DB",
			envVars: map[string]string{
				"DB_DATABASE_HOST":          "localhost",
				"DB_DATABASE_PORT":          "invalid",
				"DB_DATABASE_USER":          "test",
				"DB_DATABASE_PASSWORD":      "test",
				"DB_DATABASE_NAME":          "testdb",
				"DB_DATABASE_SSLMODE":       "disable",
				"DB_DATABASE_POOL_MIN_SIZE": "2",
				"DB_DATABASE_POOL_MAX_SIZE": "10",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save current environment and restore after test
			oldEnv := make(map[string]string)
			for k, v := range tt.envVars {
				if oldVal, exists := os.LookupEnv(k); exists {
					oldEnv[k] = oldVal
				}
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					if oldVal, exists := oldEnv[k]; exists {
						os.Setenv(k, oldVal)
					} else {
						os.Unsetenv(k)
					}
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			pool, err := New(ctx, tt.prefix)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Failed to create pool: %v", err)
			}

			if pool == nil {
				t.Fatal("Pool should not be null")
			}

			// Clean up
			pool.Close()
		})
	}
}

func TestConfig_ConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name: "Basic configuration",
			config: Config{
				DatabaseHost:              "localhost",
				DatabasePort:              "5432",
				DatabaseUser:              "test",
				DatabasePassword:          "test",
				DatabaseName:              "testdb",
				DatabaseSSLMode:           "disable",
				DatabasePoolMinSize:       2,
				DatabasePoolMaxSize:       10,
				DatabaseMaxConnLifetime:   time.Hour,
				DatabaseMaxConnIdleTime:   15 * time.Minute,
				DatabaseHealthCheckPeriod: time.Minute,
				DatabaseConnectTimeout:    30 * time.Second,
			},
			expected: "postgres://test:test@localhost:5432/testdb?sslmode=disable&pool_min_conns=2&pool_max_conns=10&pool_max_conn_lifetime=1h0m0s&pool_max_conn_idle_time=15m0s&pool_health_check_period=1m0s&connect_timeout=30&default_query_exec_mode=cache_statement",
		},
		{
			name: "Special characters in password - URL escaped",
			config: Config{
				DatabaseHost:              "localhost",
				DatabasePort:              "5432",
				DatabaseUser:              "user@domain",
				DatabasePassword:          "pass@word:123?&=",
				DatabaseName:              "testdb",
				DatabaseSSLMode:           "require",
				DatabasePoolMinSize:       2,
				DatabasePoolMaxSize:       10,
				DatabaseMaxConnLifetime:   time.Hour,
				DatabaseMaxConnIdleTime:   15 * time.Minute,
				DatabaseHealthCheckPeriod: time.Minute,
				DatabaseConnectTimeout:    30 * time.Second,
			},
			expected: "postgres://user%40domain:pass%40word%3A123%3F%26%3D@localhost:5432/testdb?sslmode=require&pool_min_conns=2&pool_max_conns=10&pool_max_conn_lifetime=1h0m0s&pool_max_conn_idle_time=15m0s&pool_health_check_period=1m0s&connect_timeout=30&default_query_exec_mode=cache_statement",
		},
		{
			name: "Empty SSL mode defaults to disable",
			config: Config{
				DatabaseHost:              "localhost",
				DatabasePort:              "5432",
				DatabaseUser:              "test",
				DatabasePassword:          "test",
				DatabaseName:              "testdb",
				DatabaseSSLMode:           "",
				DatabasePoolMinSize:       2,
				DatabasePoolMaxSize:       10,
				DatabaseMaxConnLifetime:   time.Hour,
				DatabaseMaxConnIdleTime:   15 * time.Minute,
				DatabaseHealthCheckPeriod: time.Minute,
				DatabaseConnectTimeout:    30 * time.Second,
			},
			expected: "postgres://test:test@localhost:5432/testdb?sslmode=disable&pool_min_conns=2&pool_max_conns=10&pool_max_conn_lifetime=1h0m0s&pool_max_conn_idle_time=15m0s&pool_health_check_period=1m0s&connect_timeout=30&default_query_exec_mode=cache_statement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connStr := tt.config.ConnectionString()
			if connStr != tt.expected {
				t.Errorf("Expected connection string %q, got %q", tt.expected, connStr)
			}
		})
	}
}

func TestConfig_DSN(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name: "Basic configuration",
			config: Config{
				DatabaseHost:              "localhost",
				DatabasePort:              "5432",
				DatabaseUser:              "test",
				DatabasePassword:          "test",
				DatabaseName:              "testdb",
				DatabaseSSLMode:           "disable",
				DatabasePoolMinSize:       2,
				DatabasePoolMaxSize:       10,
				DatabaseMaxConnLifetime:   time.Hour,
				DatabaseMaxConnIdleTime:   15 * time.Minute,
				DatabaseHealthCheckPeriod: time.Minute,
				DatabaseConnectTimeout:    30 * time.Second,
			},
			expected: "user=test password=test host=localhost port=5432 dbname=testdb pool_min_conns=2 pool_max_conns=10 pool_max_conn_lifetime=1h0m0s pool_max_conn_idle_time=15m0s pool_health_check_period=1m0s connect_timeout=30 sslmode=disable",
		},
		{
			name: "Empty SSL mode defaults to disable",
			config: Config{
				DatabaseHost:              "localhost",
				DatabasePort:              "5432",
				DatabaseUser:              "test",
				DatabasePassword:          "test",
				DatabaseName:              "testdb",
				DatabaseSSLMode:           "",
				DatabasePoolMinSize:       2,
				DatabasePoolMaxSize:       10,
				DatabaseMaxConnLifetime:   time.Hour,
				DatabaseMaxConnIdleTime:   15 * time.Minute,
				DatabaseHealthCheckPeriod: time.Minute,
				DatabaseConnectTimeout:    30 * time.Second,
			},
			expected: "user=test password=test host=localhost port=5432 dbname=testdb pool_min_conns=2 pool_max_conns=10 pool_max_conn_lifetime=1h0m0s pool_max_conn_idle_time=15m0s pool_health_check_period=1m0s connect_timeout=30 sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := tt.config.DSN()
			if dsn != tt.expected {
				t.Errorf("Expected DSN %q, got %q", tt.expected, dsn)
			}
		})
	}
}

func TestConfig_NoMutation(t *testing.T) {
	// Test that ConnectionString and DSN do not mutate the receiver
	cfg := Config{
		DatabaseHost:              "localhost",
		DatabasePort:              "5432",
		DatabaseUser:              "test",
		DatabasePassword:          "test",
		DatabaseName:              "testdb",
		DatabaseSSLMode:           "", // Empty - should default to "disable" but NOT mutate
		DatabasePoolMinSize:       2,
		DatabasePoolMaxSize:       10,
		DatabaseMaxConnLifetime:   time.Hour,
		DatabaseMaxConnIdleTime:   15 * time.Minute,
		DatabaseHealthCheckPeriod: time.Minute,
		DatabaseConnectTimeout:    30 * time.Second,
	}

	// Call ConnectionString multiple times
	_ = cfg.ConnectionString()
	_ = cfg.ConnectionString()

	// Verify DatabaseSSLMode was not mutated
	if cfg.DatabaseSSLMode != "" {
		t.Errorf("ConnectionString() mutated receiver: DatabaseSSLMode changed from empty to %q", cfg.DatabaseSSLMode)
	}

	// Call DSN multiple times
	_ = cfg.DSN()
	_ = cfg.DSN()

	// Verify DatabaseSSLMode was not mutated
	if cfg.DatabaseSSLMode != "" {
		t.Errorf("DSN() mutated receiver: DatabaseSSLMode changed from empty to %q", cfg.DatabaseSSLMode)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Test that defaults are set
	if cfg.DatabaseHost != "localhost" {
		t.Errorf("Expected default host 'localhost', got %q", cfg.DatabaseHost)
	}

	if cfg.DatabasePort != "5432" {
		t.Errorf("Expected default port '5432', got %q", cfg.DatabasePort)
	}

	if cfg.DatabaseSSLMode != "disable" {
		t.Errorf("Expected default SSL mode 'disable', got %q", cfg.DatabaseSSLMode)
	}

	if cfg.DatabaseEnableMetrics != true {
		t.Errorf("Expected metrics to be enabled by default")
	}

	// Test CPU-based pool sizing
	expectedMaxPool := int32(runtime.NumCPU() * 4)
	if expectedMaxPool < 10 {
		expectedMaxPool = 10
	}
	if expectedMaxPool > 50 {
		expectedMaxPool = 50
	}

	if cfg.DatabasePoolMaxSize != expectedMaxPool {
		t.Errorf("Expected max pool size %d, got %d", expectedMaxPool, cfg.DatabasePoolMaxSize)
	}

	expectedMinPool := expectedMaxPool / 5
	if expectedMinPool < 2 {
		expectedMinPool = 2
	}

	if cfg.DatabasePoolMinSize != expectedMinPool {
		t.Errorf("Expected min pool size %d, got %d", expectedMinPool, cfg.DatabasePoolMinSize)
	}
}

func TestNewOptimized(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		envVars map[string]string
		wantErr bool
	}{
		{
			name:   "Valid optimized configuration",
			prefix: "TEST",
			envVars: map[string]string{
				"TEST_DATABASE_HOST":                     "localhost",
				"TEST_DATABASE_PORT":                     "5432",
				"TEST_DATABASE_USER":                     "test",
				"TEST_DATABASE_PASSWORD":                 "test",
				"TEST_DATABASE_NAME":                     "testdb",
				"TEST_DATABASE_SSLMODE":                  "disable",
				"TEST_DATABASE_POOL_MIN_SIZE":            "5",
				"TEST_DATABASE_POOL_MAX_SIZE":            "25",
				"TEST_DATABASE_MAX_CONN_LIFETIME":        "1h",
				"TEST_DATABASE_MAX_CONN_IDLE_TIME":       "15m",
				"TEST_DATABASE_HEALTH_CHECK_PERIOD":      "1m",
				"TEST_DATABASE_CONNECT_TIMEOUT":          "30s",
				"TEST_DATABASE_STATEMENT_CACHE_CAPACITY": "512",
				"TEST_DATABASE_ENABLE_METRICS":           "true",
			},
			wantErr: false,
		},
		{
			name:   "Missing required configuration",
			prefix: "TEST",
			envVars: map[string]string{
				"TEST_DATABASE_HOST": "localhost",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save current environment and restore after test
			oldEnv := make(map[string]string)
			for k, v := range tt.envVars {
				if oldVal, exists := os.LookupEnv(k); exists {
					oldEnv[k] = oldVal
				}
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					if oldVal, exists := oldEnv[k]; exists {
						os.Setenv(k, oldVal)
					} else {
						os.Unsetenv(k)
					}
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
			pool, err := NewOptimized(ctx, tt.prefix, logger)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Failed to create optimized pool: %v", err)
			}

			if pool == nil {
				t.Fatal("Pool should not be null")
			}

			// Test metrics are available when enabled
			if pool.GetMetrics() == nil {
				t.Error("Expected metrics to be available")
			}

			// Test stats functionality
			stats := pool.GetStats()
			if stats == nil {
				t.Error("Expected stats to be available")
			}

			// Clean up
			pool.Close()
		})
	}
}

func TestDatabaseMetrics(t *testing.T) {
	// Skip this test to avoid Prometheus registry conflicts in test environment
	t.Skip("Skipping metrics test to avoid Prometheus registry conflicts during testing")
}

func TestQueryExecutor(t *testing.T) {
	// This test requires a mock database pool since we can't connect to a real database
	// In a real implementation, you might use testcontainers or similar
	t.Skip("Skipping QueryExecutor test - requires database connection")
}
