# Gox

Gox is a collection of Go modules that provide common functionality and utilities for Go applications. It aims to simplify common tasks and provide consistent patterns across different projects.

## Modules

### Logger

The `logger` module provides a flexible logging system built on top of Go's `slog` package. It supports both JSON and text-based logging formats, with configurable log levels and output destinations.

#### Features
- Configurable log levels (DEBUG, INFO, WARN, ERROR)
- Support for both JSON and text-based logging formats
- Environment-aware defaults (development vs production)
- Configurable output destination (stdout/stderr)

#### Usage
```go
import "github.com/guilhermebr/gox/logger"

// Create a new logger with configuration prefix
logger, err := logger.NewLogger("APP")
if err != nil {
    // Handle error
}

// Use the logger
logger.Info("Application started", "version", "1.0.0")
```

### Postgres

The `postgres` module provides a simple way to create and manage PostgreSQL database connections using connection pools.

#### Features
- Connection pool management using `pgx`
- Configuration through environment variables
- Context-aware connection handling

#### Usage
```go
import "github.com/guilhermebr/gox/postgres"

// Create a new database connection pool
pool, err := postgres.New(ctx, "DB")
if err != nil {
    // Handle error
}

// Use the pool
// pool.QueryRow(ctx, "SELECT * FROM users WHERE id = $1", userID)
```

## Configuration

Both modules use the `ardanlabs/conf` package for configuration management. Configuration can be provided through environment variables with the specified prefix.

### Logger Configuration
- `APP_LOG_LEVEL`: Log level (DEBUG, INFO, WARN, ERROR)
- `APP_LOG_TYPE`: Log format (JSON, TEXT)
- `APP_LOG_STDERR`: Output to stderr instead of stdout (true/false)
- `APP_ENVIRONMENT`: Environment (development/production)

### Postgres Configuration
- `DB_HOST`: Database host
- `DB_PORT`: Database port
- `DB_USER`: Database user
- `DB_PASSWORD`: Database password
- `DB_NAME`: Database name

## Installation

```bash
go get github.com/guilhermebr/gox
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details. 