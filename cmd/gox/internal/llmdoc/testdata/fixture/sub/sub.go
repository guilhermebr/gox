// Package sub holds a type the fixture re-exports.
package sub

import "time"

// Options is re-exported by the fixture.
type Options struct {
	Name    string // shown
	Nested  Inner
	Pointer *Inner
	Every   time.Duration
	hidden  int
}

// Inner is a nested type.
type Inner struct{ A int }

// Handler is a function type the fixture re-exports.
type Handler func(name string, in Inner) error

// Level is a named basic type; an alias to it stays an alias.
type Level int
