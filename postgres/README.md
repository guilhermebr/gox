# postgres

Simple constructor for a pgx connection pool using env-driven config.

## Features
- DSN and URL-style connection strings
- Pool sizing via env vars
- Uses `github.com/ardanlabs/conf/v3`

## Install
```bash
go get github.com/guilhermebr/gox/postgres
```

## Usage
```go
import (
  "context"
  "github.com/guilhermebr/gox/postgres"
)

ctx := context.Background()
pool, err := postgres.New(ctx, "DB")
if err != nil { panic(err) }

// pool.Query(ctx, "SELECT 1")
```

## Configuration
Prefix your env vars (e.g., `DB_`).

- `<PREFIX>_DATABASE_HOST` (default: `localhost`)
- `<PREFIX>_DATABASE_PORT` (default: `5432`)
- `<PREFIX>_DATABASE_USER` (required)
- `<PREFIX>_DATABASE_PASSWORD` (required)
- `<PREFIX>_DATABASE_NAME` (required)
- `<PREFIX>_DATABASE_SSLMODE` (default: `disable`)
- `<PREFIX>_DATABASE_POOL_MIN_SIZE` (default: `2`)
- `<PREFIX>_DATABASE_POOL_MAX_SIZE` (default: `10`)


