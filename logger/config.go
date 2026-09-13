// Package logger provides a configurable slog.Logger built from environment
// variables, with JSON/text handlers and environment-aware defaults.
package logger

// Config holds the logger settings, populated from environment variables.
type Config struct {
	Level       string `conf:"env:LOGGING_LEVEL,default:info"`
	Type        string `conf:"env:LOGGING_TYPE,default:text"`
	Stderr      bool   `conf:"env:LOGGING_STDERR,default:false"`
	Environment string `conf:"env:ENVIRONMENT,default:development"`
}
