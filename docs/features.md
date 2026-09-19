# Write a feature package

A feature package adds a heavy integration to gox without touching the
root: a service imports it and passes its `Enable()` option to `gox.New`.
Every feature has the same shape, so a model that learned one learned all:

```go
package redis // github.com/guilhermebr/gox/redis

type Config struct { ... }                 // the REDIS config section
type Option func(*options)                 // With* options for Enable
func Enable(opts ...Option) gox.Option     // registers the section and a component
func From(a *gox.App) *redis.Client        // panics with the standard message if not enabled
```

## 1. The module

Each feature is its own module so a consumer's module graph only carries
what it imports (ADR 0000). Building blocks a service is made of (a
datastore, auth, HTML) live at the top level; a client for a third-party
service (a cloud or SaaS API) lives under `providers/<name>`.

```
mkdir redis && cd redis
go mod init github.com/guilhermebr/gox/redis
go get github.com/redis/go-redis/v9
```

Until the root is tagged, add to `go.mod`:

```
require github.com/guilhermebr/gox v0.0.0
replace github.com/guilhermebr/gox => ../
```

and `./redis` to the repository's `go.work`.

## 2. Config

```go
package redis

import (
	"errors"
	"fmt"
	"time"
)

// Config is the REDIS config section: <PREFIX>_REDIS_*.
type Config struct {
	URL      string        `conf:"required,mask,help:redis://user:pass@host:6379/0"`
	PoolSize int           `conf:"default:10"`
	Timeout  time.Duration `conf:"default:3s"`
}

// Validate runs after loading. Name variables without the prefix.
func (c *Config) Validate() error {
	if c.PoolSize <= 0 {
		return fmt.Errorf("REDIS_POOL_SIZE must be > 0, got %d", c.PoolSize)
	}
	if c.Timeout <= 0 {
		return errors.New("REDIS_TIMEOUT must be > 0")
	}
	return nil
}
```

Rules: `conf` tags, no commas inside `help:` text, secrets carry `mask`,
defaults in tags.

## 3. Enable and the component

```go
package redis

import (
	"context"
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/lifecycle"
)

type key struct{}

type options struct{}

// Option configures Enable.
type Option func(*options)

// Enable declares a Redis client: config section REDIS and a component at
// StageDatastore that pings on Start and closes on Stop.
func Enable(opts ...Option) gox.Option {
	return func(b *gox.Builder) error {
		o := options{}
		for _, opt := range opts {
			opt(&o)
		}
		cfg := &Config{}
		b.ConfigSection("REDIS", cfg, "redis.Enable()")
		b.Component(gox.StageDatastore, func(a *gox.App) (lifecycle.Component, error) {
			ropts, err := goredis.ParseURL(cfg.URL)
			if err != nil {
				return nil, fmt.Errorf("redis: parse REDIS_URL: %w", err)
			}
			ropts.PoolSize = cfg.PoolSize
			client := goredis.NewClient(ropts)
			b.Set(key{}, client)
			return &component{client: client, log: a.Log()}, nil
		})
		return nil
	}
}

// From returns the client. It panics if Enable was not passed to gox.New.
func From(a *gox.App) *goredis.Client {
	return gox.MustValue[*goredis.Client](a, key{}, "redis.From", "redis.Enable()")
}

type component struct {
	client *goredis.Client
	log    *slog.Logger
}

func (c *component) Name() string { return "redis" }

// Start pings so a wrong URL fails the boot, not the first request.
func (c *component) Start(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: ping: %w", err)
	}
	c.log.Info("redis connected")
	return nil
}

func (c *component) Stop(context.Context) error { return c.client.Close() }

// Ready makes the client part of /readyz.
func (c *component) Ready(ctx context.Context) error { return c.client.Ping(ctx).Err() }
```

The `Builder` methods, and when to use each:

| method | use when |
|---|---|
| `ConfigSection(name, dst, declaredBy)` | always: the feature's variables under `<PREFIX>_<NAME>_*`, in `--help` and errors |
| `Component(stage, factory)` | the feature owns something that starts and stops; the factory runs at the end of `New`, after config and logging, in stage order |
| `Setup(fn)` | the feature produces a value with no lifecycle (a JWT service); runs before factories |
| `Finish(fn)` | the feature decorates another feature's output (mount on the mux `HTTP()` built); runs after factories |
| `Middleware(mw...)` | the feature needs request middleware; it runs before the auth slot |
| `ErrorRenderer(fn)` | the feature renders errors differently (HTML pages); installed above the whole chain |
| `Set(key, value)` | what `From` returns; keys are unexported types owned by the feature |

Stages: `StageDatastore` (databases, caches), `StageClient` (clients of
other services), `StageUser` (the service's own components), `StageServer`
(listeners). Lower starts first, stops last.

## 4. Tests

Test through a real app. Every feature has at least these:

```go
func TestEnableRequiresURL(t *testing.T) {
	_, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), redis.Enable())
	// err mentions BILLING_REDIS_URL, "required" and "redis.Enable()"
}

func TestFromPanicsWithTheStandardMessage(t *testing.T) {
	// "gox: redis.From called but redis.Enable() was not passed to gox.New"
}

func TestHelpListsTheSection(t *testing.T) {
	// --help usage contains BILLING_REDIS_URL and "redis.Enable()"
}
```

Integration tests run behind `//go:build integration` against an address
from the environment.

## 5. Wiring it into the repository

- A depguard rule in `.golangci.yml` (copy the `feature-postgres` block).
- A line in `cmd/gox/main.go`'s spec so `llm.txt` documents the API and
  the variables; then `make llm`.
- A probe in the Makefile's `check-deps` proving an example that does not
  import the feature does not link its dependency.
- An example under `examples/` and a recipe under `docs/recipes/`.
- An ADR in `docs/decisions/` saying why this dependency and this design.
