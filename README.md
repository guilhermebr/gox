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

### HTTP

The `http` module provides a thin wrapper around Go's `net/http` server with sensible configuration via environment variables, graceful shutdown, and a `ServerManager` to run multiple servers.

#### Features
- Graceful shutdown on SIGINT/SIGTERM with configurable timeout
- Environment-driven configuration (address, timeouts)
- Run one or many servers with `ServerManager`
- Structured logging via `slog`

#### Usage
```go
import (
    goxhttp "github.com/guilhermebr/gox/http"
    "github.com/guilhermebr/gox/logger"
    "net/http"
)

func main() {
    log, _ := logger.NewLogger("APP")

    mux := http.NewServeMux()
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ok"))
    })

    srv, err := goxhttp.NewServer("HTTP", mux, log)
    if err != nil {
        panic(err)
    }

    if err := srv.StartWithGracefulShutdown(); err != nil {
        log.Error("server exited", "error", err)
    }
}
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

### Supabase

The `supabase` module provides a simple way to create and configure Supabase clients for interacting with Supabase services.

#### Features
- Easy Supabase client creation
- Configuration through environment variables
- Built on top of the official Supabase Go client

#### Usage
```go
import "github.com/guilhermebr/gox/supabase"

// Create a new Supabase client with configuration prefix
client, err := supabase.New("APP")
if err != nil {
    // Handle error
}

// Use the client for database operations, authentication, etc.
// client.From("users").Select("*")
```

### Monetary

The `monetary` module provides types and functions for handling monetary values with precise arithmetic using `big.Int`. It supports both fiat currencies and cryptocurrencies, storing amounts as integers in the smallest unit to maintain precision.

#### Features
- Precise arithmetic using `big.Int` for amounts (no floating-point errors)
- Support for fiat currencies (USD, BRL, GBP, CHF, JPY, etc.)
- Support for cryptocurrencies (BTC, ETH, USDT, USDC, etc.)
- Mathematical operations (Add, Subtract, Multiply, Divide)
- Comparison operations (Equal, GreaterThan, LessThan)
- JSON marshaling/unmarshaling support
- Decimal string parsing and formatting
- Predefined assets with appropriate precision

#### Usage
```go
import "github.com/guilhermebr/gox/monetary"

// Create monetary values from decimal strings
usd100, _ := monetary.NewMonetaryFromString(monetary.USD, "100.50")
usd50, _ := monetary.NewMonetaryFromString(monetary.USD, "50.25")

// Create from big.Int (amounts in smallest unit - cents for USD)
amount := big.NewInt(10050) // $100.50 in cents
usd, _ := monetary.NewMonetary(monetary.USD, amount)

// Perform arithmetic operations
sum, _ := usd100.Add(usd50)           // $150.75
diff, _ := usd100.Subtract(usd50)     // $50.25
doubled, _ := usd100.Multiply(big.NewInt(2)) // $201.00

// Comparisons
isEqual := usd100.Equal(usd50)        // false
isGreater, _ := usd100.GreaterThan(usd50) // true

// Work with cryptocurrencies
btc, _ := monetary.NewMonetaryFromString(monetary.BTC, "0.00123456")
fmt.Println(btc.String()) // [BTC (BTC) 0.00123456]

// Find assets by symbol or name
asset, found := monetary.FindAssetBySymbol("BTC")
if found {
    fmt.Println(asset.String()) // BTC (BTC)
}
```

## Configuration

The Logger, Postgres, and Supabase modules use the `ardanlabs/conf` package for configuration management. Configuration can be provided through environment variables with the specified prefix. The Monetary module does not require external configuration.

### Logger Configuration
- `APP_LOGGING_LEVEL`: Log level (DEBUG, INFO, WARN, ERROR)
- `APP_LOGGING_TYPE`: Log format (JSON, TEXT)
- `APP_LOGGING_STDERR`: Output to stderr instead of stdout (true/false)
- `APP_ENVIRONMENT`: Environment (development/production)

### Postgres Configuration
- `DB_DATABASE_HOST_DIRECT`: Database host
- `DB_DATABASE_PORT_DIRECT`: Database port
- `DB_DATABASE_USER`: Database user
- `DB_DATABASE_PASSWORD`: Database password
- `DB_DATABASE_NAME`: Database name
- `DB_DATABASE_SSLMODE`: SSL mode (disable/require)
- `DB_DATABASE_POOL_MIN_SIZE`: Minimum pool size
- `DB_DATABASE_POOL_MAX_SIZE`: Maximum pool size

### Supabase Configuration
- `APP_SUPABASE_URL`: Supabase project URL
- `APP_SUPABASE_KEY`: Supabase API key (anon or service role key)

## Installation

```bash
go get github.com/guilhermebr/gox
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details. 