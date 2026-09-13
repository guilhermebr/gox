// Package logger provides a configurable slog.Logger built from environment
// variables, with JSON/text handlers and environment-aware defaults.
//
// Deprecated: services built on gox get their logger from gox.New (see
// github.com/guilhermebr/gox and pkg/log). This module stays standalone and
// unchanged so existing consumers keep working; it will be removed one minor
// version after the root package reaches v1.
package logger

// Config holds the logger settings, populated from environment variables.
type Config struct {
	Level       string `conf:"env:LOGGING_LEVEL,default:info"`
	Type        string `conf:"env:LOGGING_TYPE,default:text"`
	Stderr      bool   `conf:"env:LOGGING_STDERR,default:false"`
	Environment string `conf:"env:ENVIRONMENT,default:development"`
}
