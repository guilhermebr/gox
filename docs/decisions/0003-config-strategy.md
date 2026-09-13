# 0003. One config pass, one prefix, feature sections

Date: 2026-09-13
Status: accepted

## Context

The audited services parsed configuration between one and five times per
boot from independently defined structs, so no single place knew what the
service's environment looked like and `--help` covered a fraction of it.
Three of four used unprefixed variables; one used a `PORTAL_` prefix. All
four used `github.com/ardanlabs/conf/v3`.

## Decision

- `ardanlabs/conf/v3` stays. It is small, tag-driven, supports `required`,
  `default`, `mask` and `--help`, and every consumer already uses it.
- `config.Base` holds what every service has: service name, version,
  environment, HTTP, admin, shutdown, log, OTel, HTTP client. **No datastore
  fields.**
- A service embeds `gox.BaseConfig` in its own struct and passes it to
  `gox.WithConfig`. Feature packages register **sections**
  (`b.ConfigSection("POSTGRES", &cfg, "postgres.Enable()")`). `New` loads
  Base, the user struct and every section in one call, validates everything,
  and returns one error. `--help` lists every variable with the option that
  declared it.
- One prefix per service, defaulting to the uppercased name
  (`BILLING_HTTP_ADDR`, `BILLING_POSTGRES_URL`). `gox.WithConfigPrefix("")`
  is first-class so existing services keep their unprefixed variables.
- `HTTP_ADDR` defaults to `:8080` and honors `PORT` when unset, because
  Tsuru, Fly and Heroku inject it.
- Fields set on the struct before loading act as defaults; conf keeps
  non-zero values. `gox.WithVersion` and `gox.WithShutdownTimeout` work this
  way, so the environment can still override them.
- Secrets carry `mask`. The effective configuration is logged at debug level
  with masked values.
- Required-value errors are rewritten to name the full variable and the
  declaring feature: `BILLING_POSTGRES_URL is required` inside
  `section POSTGRES (declared by postgres.Enable())`.

## Consequences

- One struct documents a service. A model reading `--help` sees everything.
- The embed is detected through an unexported marker method, not
  reflection, because an embedded field named `BaseConfig` would shadow an
  exported method of the same name.
- conf derives names from field paths (`HTTPClient.Timeout` →
  `HTTP_CLIENT_TIMEOUT`, `Otel.Enabled` → `OTEL_ENABLED`). Field names in
  `Base` are chosen so the derived names read well; `Otel` rather than
  `OTel` for that reason.
- YAML or file layering is not supported. Environment is the one source.
