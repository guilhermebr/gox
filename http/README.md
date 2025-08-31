# http

Thin wrapper over `net/http` with graceful shutdown, env-driven config, and multi-server management.

## Features
- Graceful shutdown with timeout
- Env-configurable address and timeouts
- `ServerManager` to run multiple servers
- Uses `slog` for structured logs

## Install
```bash
go get github.com/guilhermebr/gox/http
```

## Usage
```go
package main

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

## Configuration
Environment variables are parsed using `github.com/ardanlabs/conf/v3`. Prefix with your chosen name (e.g., `HTTP_`).

- `<PREFIX>_ADDRESS` (default: `0.0.0.0:3000`)
- `<PREFIX>_READ_HEADER_TIMEOUT` (default: `60s`)
- `<PREFIX>_READ_TIMEOUT` (default: `10s`)
- `<PREFIX>_WRITE_TIMEOUT` (default: `10s`)
- `<PREFIX>_IDLE_TIMEOUT` (default: `60s`)
- `<PREFIX>_SHUTDOWN_TIMEOUT` (default: `20s`)

## Multiple servers
```go
log, _ := logger.NewLogger("APP")
mgr := goxhttp.NewServerManager(log)

api, _ := goxhttp.NewServer("API", apiMux, log)
admin, _ := goxhttp.NewServer("ADMIN", adminMux, log)

mgr.AddServer(api)
mgr.AddServer(admin)

_ = mgr.StartAll()
```


