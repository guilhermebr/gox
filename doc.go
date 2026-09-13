// Package gox is an opinionated, batteries-included base for Go services.
//
// A plain HTTP service imports only this package. Heavy integrations
// (Postgres, Supabase, JWT, server-rendered HTML) live in their own
// subpackages and are opted into by importing them and passing their
// Enable() option to New.
//
// The root package is being built in phases; see GOX_FRAMEWORK_PLAN.md and
// docs/audit.md in the repository for the current status.
package gox
