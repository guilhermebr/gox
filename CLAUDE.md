# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

This is a Go modules collection (gox) providing common utilities for Go applications. The repository is structured as independent modules, each with their own go.mod file:

- **logger**: Configurable slog logger with environment-driven configuration
- **http**: HTTP server wrapper with graceful shutdown and multi-server management  
- **monetary**: Precise monetary arithmetic using big.Int for fiat and crypto currencies
- **postgres**: PostgreSQL connection pool management using pgx
- **supabase**: Supabase client helper with environment configuration

Each module is self-contained with its own dependencies and can be imported independently.

## Common Development Commands

### Testing
```bash
# Run tests for all modules
go test ./...

# Run tests for a specific module
cd <module-name> && go test -v

# Run benchmarks (available in monetary module)
cd monetary && go test -bench=.
```

### Module Management
```bash
# Tidy dependencies for all modules
find . -name go.mod -execdir go mod tidy \;

# Update dependencies for a specific module
cd <module-name> && go get -u ./...
```

### Code Quality
```bash
# Format code
go fmt ./...

# Vet code for potential issues
go vet ./...
```

## Architecture Patterns

### Configuration Management
All modules use `github.com/ardanlabs/conf/v3` for environment-driven configuration with prefixes:
- Logger: `<PREFIX>_LOGGING_*`
- HTTP: `<PREFIX>_*` (ADDRESS, READ_TIMEOUT, etc.)
- Postgres: `<PREFIX>_DATABASE_*` 
- Supabase: `<PREFIX>_SUPABASE_*`

### Error Handling
- Always handle returned errors explicitly
- Use context for cancellation in database and network operations
- Pass loggers to components that accept them for structured logging

### Module Import Patterns
```go
// Use aliases for potential naming conflicts
import (
    goxhttp "github.com/guilhermebr/gox/http"
    "github.com/guilhermebr/gox/logger"
)

// Always get the specific subpackage
go get github.com/guilhermebr/gox/logger
```

### Monetary Values
- Store amounts as integers in smallest units (cents, satoshis) using big.Int
- Use NewMonetaryFromString() for decimal inputs 
- Perform arithmetic operations using the provided methods (Add, Subtract, etc.)
- Use predefined assets (monetary.USD, monetary.BTC, etc.)

## Creating New Modules

When creating a new module, follow these patterns from existing modules:

### Structure
- Create a new directory with the module name
- Include a `go.mod` file with module path `github.com/guilhermebr/gox/<module-name>`
- Use Go standard library whenever possible to minimize dependencies
- Only add external dependencies when absolutely necessary

### Configuration Pattern
- Use `github.com/ardanlabs/conf/v3` for environment-driven configuration
- Define a Config struct with appropriate field tags
- Use environment variable prefix: `<PREFIX>_<MODULE_NAME>_*`
- Implement Parse() method for configuration loading

### Testing Requirements
- Include comprehensive tests for all public functions
- Use table-driven tests where appropriate
- Include benchmarks for performance-critical code
- Follow Go testing conventions with `_test.go` suffix

### Documentation
- Include package-level documentation
- Document all exported functions and types
- Follow Go documentation conventions

