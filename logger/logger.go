package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ardanlabs/conf/v3"
)

func NewLogger(prefix string) (*slog.Logger, error) {
	var cfg Config

	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing logger config from prefix [%s]: %w", prefix, err)
	}

	return NewLoggerConfig(cfg)
}

func NewLoggerConfig(cfg Config) (*slog.Logger, error) {
	logOutput := os.Stdout
	if cfg.Stderr {
		logOutput = os.Stderr
	}

	logOptions := slog.HandlerOptions{
		Level: getLogLevel(cfg),
	}

	logHandler := getLogHandler(cfg, logOutput, &logOptions)

	logger := slog.New(logHandler)
	slog.SetDefault(logger)
	return logger, nil
}

func getLogLevel(cfg Config) slog.Level {
	// If a specific level is configured, use it
	if cfg.Level != "" {
		switch strings.ToUpper(cfg.Level) {
		case "DEBUG":
			return slog.LevelDebug
		case "INFO":
			return slog.LevelInfo
		case "WARN", "WARNING":
			return slog.LevelWarn
		case "ERROR":
			return slog.LevelError
		}
	}

	// Otherwise, use the level based on environment
	if cfg.Environment == "development" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func getLogHandler(cfg Config, output *os.File, opts *slog.HandlerOptions) slog.Handler {
	// If a specific type is configured, use it
	if cfg.Type != "" {
		switch strings.ToUpper(cfg.Type) {
		case "JSON":
			return slog.NewJSONHandler(output, opts)
		case "TEXT":
			return slog.NewTextHandler(output, opts)
		}
	}

	// Otherwise, use the type based on environment
	if cfg.Environment == "development" {
		return slog.NewTextHandler(output, opts)
	}
	return slog.NewJSONHandler(output, opts)
}
