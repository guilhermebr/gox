# 0010 — OpenAPI request validation

Date: 2026-09-19
Status: accepted

## Context

When a client is generated from an OpenAPI document, the document is the
contract. Enforcing it only on the client lets the server drift: a handler
accepts a field the contract forbids, or stops requiring one it demands,
and nothing fails until a client breaks.

## Decision

- `gox/openapi` is a feature module: `openapi.Enable(documents...)` installs
  request validation as feature middleware. It has no config section and no
  accessor; the documents are embedded bytes.
- Validation uses `pb33f/libopenapi-validator` because it supports OpenAPI
  3.1 and JSON Schema 2020-12, which is what current tooling (TypeSpec,
  modern generators) emits. Format assertions are on: a `uuid` or `date`
  format is part of the contract.
- A request no document describes passes through. The mux, not the
  contract, decides what exists, so health probes, webhooks and pages need
  no entries, and adopting a contract for one API does not break the rest.
- Security schemes are not evaluated. Authentication stays in the service's
  middleware, which runs first when its option is passed first.
- A violation is a gox `invalid_argument` error with an `errors` detail of
  `{code, message, pointer}`, so it renders through the application's error
  shape, problem details included.
- Responses are not validated in production traffic; that belongs in tests.
- The module does not generate code. `oapi-codegen`'s `std-http-server`
  target already emits handlers for `net/http`'s mux; the recipe shows it.

## Consequences

- The validator reads the body, so the middleware buffers it (within the
  framework's body limit) and hands the handler a fresh reader.
- The dependency tree of the validator stays out of services that do not
  import the module; the linkage probe checks it.
