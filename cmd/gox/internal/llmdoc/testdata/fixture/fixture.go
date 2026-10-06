// Package fixture is a tiny package for llmdoc tests.
package fixture

import (
	"context"
	"time"

	"github.com/example/fixture/sub"
)

// Config is the FIXTURE section.
type Config struct {
	URL        string        `conf:"required,mask,help:where to connect"`
	MaxConns   int32         `conf:"default:10"`
	Timeout    time.Duration `conf:"default:5s,help:per call"`
	Legacy     string        `conf:"env:OLD_NAME,default:x"`
	HTTPClient struct {
		Timeout time.Duration `conf:"default:30s"`
	}
	skipped string
}

// Option configures Enable.
type Option func(*Config)

// Enable turns the fixture on. Second sentence is dropped.
func Enable(opts ...Option) Option { return nil }

// From returns the thing.
func From(a any) *Config { return nil }

// Validate checks the config.
func (c *Config) Validate() error { return nil }

func unexported() {}

// Sentinel errors.
var (
	// ErrNope is returned when nope.
	ErrNope = errNope
)

var errNope error

// Kind classifies.
type Kind int

// Kinds.
const (
	KindA Kind = iota
	KindB
)

// Alias is Config under another name.
type Alias = Config

// Wrapped ends its first sentence at a line break.
// The second sentence is dropped. So is the third.
func Wrapped() {}

// Old builds the thing the old way. It does not dial.
//
// Deprecated: use Enable and From. Old goes away later.
func Old() {}

// Store is an interface whose method comments stay out of llm.txt.
type Store interface {
	// Put stores. Second sentence.
	Put(ctx context.Context, key string) error
	// Get loads.
	Get(key string) (string, error)
}

// Options is sub.Options, shown with its fields.
type Options = sub.Options

// Handler is sub.Handler, shown with its signature.
type Handler = sub.Handler

// Level is sub.Level, left as an alias.
type Level = sub.Level
