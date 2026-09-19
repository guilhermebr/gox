// Package gox is an opinionated, batteries-included base for Go services.
//
// A plain HTTP service imports only this package. Heavy integrations
// (Postgres, Supabase, JWT, server-rendered HTML) live in their own
// subpackages and are opted into by importing them and passing their
// Enable() option to New.
package gox
