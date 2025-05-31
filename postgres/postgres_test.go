package postgres

import (
	"context"
	"os"
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
				"DB_DATABASE_HOST_DIRECT":   "localhost",
				"DB_DATABASE_PORT_DIRECT":   "5432",
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
				"DB_DATABASE_HOST_DIRECT": "localhost",
				// Missing other required fields
			},
			wantErr: true,
		},
		{
			name:   "Invalid port",
			prefix: "DB",
			envVars: map[string]string{
				"DB_DATABASE_HOST_DIRECT":   "localhost",
				"DB_DATABASE_PORT_DIRECT":   "invalid",
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
				DatabaseHost:        "localhost",
				DatabasePort:        "5432",
				DatabaseUser:        "test",
				DatabasePassword:    "test",
				DatabaseName:        "testdb",
				DatabaseSSLMode:     "disable",
				DatabasePoolMinSize: 2,
				DatabasePoolMaxSize: 10,
			},
			expected: "postgres://test:test@localhost:5432/testdb?sslmode=disable&pool_min_conns=2&pool_max_conns=10",
		},
		{
			name: "With custom SSL mode",
			config: Config{
				DatabaseHost:        "localhost",
				DatabasePort:        "5432",
				DatabaseUser:        "test",
				DatabasePassword:    "test",
				DatabaseName:        "testdb",
				DatabaseSSLMode:     "require",
				DatabasePoolMinSize: 2,
				DatabasePoolMaxSize: 10,
			},
			expected: "postgres://test:test@localhost:5432/testdb?sslmode=require&pool_min_conns=2&pool_max_conns=10",
		},
		{
			name: "With custom pool sizes",
			config: Config{
				DatabaseHost:        "localhost",
				DatabasePort:        "5432",
				DatabaseUser:        "test",
				DatabasePassword:    "test",
				DatabaseName:        "testdb",
				DatabaseSSLMode:     "disable",
				DatabasePoolMinSize: 5,
				DatabasePoolMaxSize: 20,
			},
			expected: "postgres://test:test@localhost:5432/testdb?sslmode=disable&pool_min_conns=5&pool_max_conns=20",
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
				DatabaseHost:        "localhost",
				DatabasePort:        "5432",
				DatabaseUser:        "test",
				DatabasePassword:    "test",
				DatabaseName:        "testdb",
				DatabaseSSLMode:     "disable",
				DatabasePoolMinSize: 2,
				DatabasePoolMaxSize: 10,
			},
			expected: "user=test password=test host=localhost port=5432 dbname=testdb pool_min_conns=2 pool_max_conns=10 sslmode=disable",
		},
		{
			name: "With custom SSL mode",
			config: Config{
				DatabaseHost:        "localhost",
				DatabasePort:        "5432",
				DatabaseUser:        "test",
				DatabasePassword:    "test",
				DatabaseName:        "testdb",
				DatabaseSSLMode:     "require",
				DatabasePoolMinSize: 2,
				DatabasePoolMaxSize: 10,
			},
			expected: "user=test password=test host=localhost port=5432 dbname=testdb pool_min_conns=2 pool_max_conns=10 sslmode=require",
		},
		{
			name: "With custom pool sizes",
			config: Config{
				DatabaseHost:        "localhost",
				DatabasePort:        "5432",
				DatabaseUser:        "test",
				DatabasePassword:    "test",
				DatabaseName:        "testdb",
				DatabaseSSLMode:     "disable",
				DatabasePoolMinSize: 5,
				DatabasePoolMaxSize: 20,
			},
			expected: "user=test password=test host=localhost port=5432 dbname=testdb pool_min_conns=5 pool_max_conns=20 sslmode=disable",
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
