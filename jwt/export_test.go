package jwt

import "time"

// SetJWKSMinRefresh shortens the refetch rate limit for tests.
func SetJWKSMinRefresh(d time.Duration) (restore func()) {
	old := jwksMinRefresh
	jwksMinRefresh = d
	return func() { jwksMinRefresh = old }
}
