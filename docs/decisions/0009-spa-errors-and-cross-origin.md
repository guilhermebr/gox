# 0009 — Single-page apps, replaceable error shape, cross-origin protection

Date: 2026-09-19
Status: accepted

## Context

Services whose frontend is a single-page application need three things the
framework did not have: the built app served from the API's origin with
client-side routes that deep-link, an error shape other than the gox
envelope when the client was generated from a contract that fixes one, and
CSRF protection that does not depend on rendering a token into a form.

## Decision

- `gox.WithSPA(SPAConfig)` registers a catch-all on the mux, so registered
  routes always win. It serves files at their paths, `index.html` for every
  other `GET`, and the error envelope for unmatched requests under the
  declared server prefixes and for missing files with an extension. Caching
  is by path prefix (`/assets/` immutable by default), not by guessing
  which names look hashed. `Head` injects per-request tags; there is no
  templating of the shell. It lives in the root because it is stdlib only.
  The cost: with a catch-all, a wrong method on a real route is a 404
  rather than a 405.
- `gox.WithErrorRenderer` makes the renderer slot feature packages already
  used public. Application renderers run after feature renderers, so HTML
  error pages keep working, and replace the envelope for everything else,
  framework rejections included. `gox.ProblemJSON` is the stock RFC 9457
  renderer; error details become extension members, which is how field
  errors with JSON pointers travel. The envelope stays the default.
- Cross-origin protection is on by default through net/http's
  `CrossOriginProtection`: it rejects state-changing browser requests from
  another origin using `Sec-Fetch-Site` and `Origin`, and lets through
  everything that is not a browser. Origins listed in `WithCORS` are
  trusted; a `*` wildcard is not. gox/web keeps its form tokens for
  browsers that send neither header.
- A platform that probes another liveness path mounts
  `a.Health().LivenessHandler()` there; no option was added.

## Consequences

- An API consumed by browsers on other origins must list them in
  `WithCORS`, which was already required for reads.
- Token-based CSRF is no longer needed for same-origin single-page apps.
