package logger

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		envVars map[string]string
		wantErr bool
	}{
		{
			name:   "Valid environment configuration",
			prefix: "APP",
			envVars: map[string]string{
				"APP_ENVIRONMENT": "development",
			},
			wantErr: false,
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

			logger, err := NewLogger(tt.prefix)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Failed to create logger: %v", err)
			}

			if logger == nil {
				t.Fatal("Logger should not be null")
			}

			// Verify the logger is set as default
			if slog.Default() != logger {
				t.Error("Logger should be set as default")
			}
		})
	}
}

func TestNewLoggerConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		checkLevel  slog.Level
		checkType   string
		checkOutput *os.File
	}{
		{
			name: "Development environment defaults",
			config: Config{
				Environment: "development",
			},
			checkLevel:  slog.LevelDebug,
			checkType:   "text",
			checkOutput: os.Stdout,
		},
		{
			name: "Production environment defaults",
			config: Config{
				Environment: "production",
			},
			checkLevel:  slog.LevelInfo,
			checkType:   "json",
			checkOutput: os.Stdout,
		},
		{
			name: "Custom configuration",
			config: Config{
				Environment: "production",
				Level:       "ERROR",
				Type:        "TEXT",
				Stderr:      true,
			},
			checkLevel:  slog.LevelError,
			checkType:   "text",
			checkOutput: os.Stderr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := NewLoggerConfig(tt.config)
			if err != nil {
				t.Fatalf("Failed to create logger: %v", err)
			}

			handler := logger.Handler()

			// Check logger level
			if handler.Enabled(context.Background(), tt.checkLevel-1) {
				t.Errorf("Incorrect logger level. Expected: %v", tt.checkLevel)
			}

			// Check handler type
			switch tt.checkType {
			case "json":
				if _, ok := handler.(*slog.JSONHandler); !ok {
					t.Error("Handler should be of type JSONHandler")
				}
			case "text":
				if _, ok := handler.(*slog.TextHandler); !ok {
					t.Error("Handler should be of type TextHandler")
				}
			}
		})
	}
}

func TestGetLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected slog.Level
	}{
		{
			name: "Debug level",
			config: Config{
				Level: "DEBUG",
			},
			expected: slog.LevelDebug,
		},
		{
			name: "Info level",
			config: Config{
				Level: "INFO",
			},
			expected: slog.LevelInfo,
		},
		{
			name: "Warn level",
			config: Config{
				Level: "WARN",
			},
			expected: slog.LevelWarn,
		},
		{
			name: "Error level",
			config: Config{
				Level: "ERROR",
			},
			expected: slog.LevelError,
		},
		{
			name: "Development environment default",
			config: Config{
				Environment: "development",
			},
			expected: slog.LevelDebug,
		},
		{
			name: "Production environment default",
			config: Config{
				Environment: "production",
			},
			expected: slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := getLogLevel(tt.config)
			if level != tt.expected {
				t.Errorf("Expected level %v, got %v", tt.expected, level)
			}
		})
	}
}

func TestGetLogHandler(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name: "JSON type",
			config: Config{
				Type: "JSON",
			},
			expected: "json",
		},
		{
			name: "Text type",
			config: Config{
				Type: "TEXT",
			},
			expected: "text",
		},
		{
			name: "Development environment default",
			config: Config{
				Environment: "development",
			},
			expected: "text",
		},
		{
			name: "Production environment default",
			config: Config{
				Environment: "production",
			},
			expected: "json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := getLogHandler(tt.config, os.Stdout, &slog.HandlerOptions{})

			switch tt.expected {
			case "json":
				if _, ok := handler.(*slog.JSONHandler); !ok {
					t.Error("Handler should be of type JSONHandler")
				}
			case "text":
				if _, ok := handler.(*slog.TextHandler); !ok {
					t.Error("Handler should be of type TextHandler")
				}
			}
		})
	}
}
