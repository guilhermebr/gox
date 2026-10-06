// Package jwt issues and validates JSON Web Tokens with one algorithm, chosen
// by which <PREFIX>_JWT_* key is set: HS256 with SECRET_KEY, RS256 with
// PUBLIC_KEY (plus PRIVATE_KEY on the service that signs), or RS256 against
// an identity provider's key set with JWKS_URL (plus ISSUER, the provider's
// issuer: the default, gox, rejects its tokens). Tokens signed with any other
// algorithm are rejected.
//
// Pass Enable to gox.New and read the Service with From(a).
// Enable(WithAuth()) requires a valid bearer token on every route except
// /healthz and /readyz; to protect only some routes, wrap their handlers with
// Auth(From(a)) instead. Handlers read the token's claims with
// ClaimsFromContext; Claims.Raw holds every claim, including those an
// identity provider adds.
package jwt
