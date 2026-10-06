# providers

Clients for third-party services, one module each: pass `<name>.Enable()` to
`gox.New` and read the client with `<name>.From(a)`. `go doc` on the module is
the reference.

| module | what it wraps |
|---|---|
| `github.com/guilhermebr/gox/providers/mailgun` | Mailgun: a `mail.Sender` through the API, verified delivery webhooks and inbound routes |
| `github.com/guilhermebr/gox/providers/posthog` | PostHog: analytics and feature flags, queue flushed on shutdown |
| `github.com/guilhermebr/gox/providers/s3` | object storage on S3 and compatible servers (MinIO, R2, Google Cloud Storage): a `storage.Bucket` with presigned direct uploads |
| `github.com/guilhermebr/gox/providers/stripe` | Stripe: the API client and verified webhooks |
| `github.com/guilhermebr/gox/providers/supabase` | the Supabase client, with readiness from its auth health endpoint |
| `github.com/guilhermebr/gox/providers/temporal` | Temporal: the SDK client, workers that start and drain with the app, readiness, tracing |
| `github.com/guilhermebr/gox/providers/workos` | WorkOS: API client, AuthKit login, session cookie with refresh, bearer tokens, organization switch, webhooks |

Adding a provider follows one checklist,
`.claude/skills/gox-add-module/SKILL.md`; `providers/AGENTS.md` has the rules
specific to providers.
