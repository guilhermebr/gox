# logger

Configurable `slog` logger with env-driven level/format and sane defaults.

## Features
- JSON or text handlers
- Level: debug/info/warn/error
- Environment-aware defaults
- Output to stdout or stderr

## Install
```bash
go get github.com/guilhermebr/gox/logger
```

## Usage
```go
import "github.com/guilhermebr/gox/logger"

log, err := logger.NewLogger("APP")
if err != nil { panic(err) }

log.Info("started", "version", "1.0.0")
log.Debug("details", "k", 1)
```

Or provide a config directly:
```go
cfg := logger.Config{Level: "debug", Type: "json", Environment: "production"}
log, _ := logger.NewLoggerConfig(cfg)
```

## Configuration
Environment variables are parsed using `github.com/ardanlabs/conf/v3` with your chosen prefix (e.g., `APP_`).

- `<PREFIX>_LOGGING_LEVEL` (default: `info`)
- `<PREFIX>_LOGGING_TYPE` (default: `text`)
- `<PREFIX>_LOGGING_STDERR` (default: `false`)
- `<PREFIX>_ENVIRONMENT` (default: `development`)


