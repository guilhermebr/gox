# providers

Clients for third-party services, one module each, with the same shape and
rules as every feature package: `Config`, `Enable(opts...) gox.Option`,
`From(a)`, a config section under the service prefix, readiness when the
provider exposes a health endpoint. A provider imports the root, `pkg/*` and
its own SDK; never another feature or provider.

| module | what it wraps |
|---|---|
| `github.com/guilhermebr/gox/providers/mailgun` | Mailgun: a `mail.Sender` through the API, verified delivery webhooks and inbound routes |
| `github.com/guilhermebr/gox/providers/s3` | object storage on S3 and compatible servers (MinIO, R2): a `storage.Bucket` with presigned direct uploads |
| `github.com/guilhermebr/gox/providers/supabase` | the Supabase client |
| `github.com/guilhermebr/gox/providers/temporal` | Temporal: the SDK client, workers that start and drain with the app, readiness, tracing |
| `github.com/guilhermebr/gox/providers/workos` | WorkOS: API client, AuthKit login, session cookie with refresh, bearer tokens, organization switch, webhooks |

Add a provider with `mkdir providers/<name>` and a module named
`github.com/guilhermebr/gox/providers/<name>`; `docs/features.md` walks
through the code, `.golangci.yml` already covers the directory.
