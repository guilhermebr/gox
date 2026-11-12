# 0016 — Server-sent events

Date: 2026-11-12
Status: accepted

## Context

Services need to push changes to browsers (an order was paid, an export is
ready) without polling. Server-sent events do this over plain HTTP, but a
streaming handler could not run on the gox server: the timeout middleware
buffers every response until the handler returns, and the server's read and
write timeouts close long-lived connections.

## Decision

- gox supports server-sent events with `gox.HTTP(gox.WithSSE(gox.SSEConfig{...}))`,
  following `WithCORS` and `WithSPA`; `a.SSE()` returns the hub. The hub
  lives in `pkg/sse`, standard library only, so the root gains no
  dependency.
- Clients subscribe to topics with `GET /events?topic=…` (an exact topic,
  `{name}`/`{name...}` segment templates as in net/http patterns, or `*`);
  the service publishes `SSEUpdate`s from Go. Private updates reach only
  the subscribers `SSEConfig.Authorize(r, topic)` allows, so the service's
  existing auth decides.
- The middleware changes only for the SSE route, and only when SSE is
  enabled. With `WithSSE`, `middleware.StreamCapture` joins the chain and
  marks requests for the SSE route; `Timeout` lets marked requests through,
  and the route lifts the server's read and write deadlines for its
  connection, setting a per-write deadline of its own so a stalled client
  is dropped. Without `WithSSE` the chain is unchanged, and with it every
  other route keeps its timeouts.
- Delivery never blocks a publisher: a subscriber that falls behind is
  disconnected and resumes with `Last-Event-ID` from an in-memory history.
  When what it missed is gone (the ID left the history, or the transport
  lost updates while reconnecting), it gets an `SSEResyncEvent` instead of
  a silently incomplete replay, and should refetch. On shutdown the hub
  ends every stream before the server drains.
- Streams are bounded: `MaxAge` (15m) ends each one so the client
  reconnects through the middleware chain, which re-checks its credentials;
  `Authorize` answers are cached for a minute; `MaxSubscribers` (10000)
  caps open streams per process.
- Updates fan out in memory by default. Across replicas they go through an
  `SSETransport`; the first is `postgres.SSETransport` (LISTEN/NOTIFY),
  since the services gox targets already run Postgres. The publishing
  replica delivers to its own subscribers directly and skips its echo, so
  a listener outage never loses a replica's own updates.

## Consequences

- NOTIFY carries about 7.9 KB; bigger updates fail with
  `postgres.ErrSSETooLarge` and should carry an ID or a URL instead.
- Delivery is at most once, and the replay history is per replica. Each
  replica records its own updates when it publishes them and the others'
  when NOTIFY delivers them, so two replicas publishing at the same moment
  can remember the pair in different orders; a client that resumes on
  another replica can then miss or repeat one of them. Events are a hint
  to update, not the record.
- A subscription refused with an error status (503 at `MaxSubscribers` or
  during shutdown, 401 once credentials expire) ends a browser's
  `EventSource` for good: the page subscribes again itself.
- LISTEN needs a session: behind a transaction-mode pooler (PgBouncer,
  Supabase's port 6543) it succeeds but nothing arrives. The listener
  probes itself on every connect and logs an error when the probe is lost.
- Browsers' `EventSource` cannot send an `Authorization` header;
  subscriptions authenticate with a cookie or a fetch-based client.
