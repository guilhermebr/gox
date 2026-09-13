// Package http is a thin wrapper over net/http with environment-driven
// configuration, graceful shutdown, and multi-server management.
//
// Deprecated: services built on gox declare gox.HTTP() and get the server,
// middleware chain and graceful shutdown from gox.New (see
// github.com/guilhermebr/gox and pkg/httpserver). This module stays
// standalone and unchanged so existing consumers keep working; it will be
// removed one minor version after the root package reaches v1.
package http
