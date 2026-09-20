package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type clientIPKey struct{}

// ClientIP resolves the address of the client behind proxies. trusted is a
// comma-separated list of CIDRs ("10.0.0.0/8,fd00::/8"): X-Forwarded-For is
// believed only when the peer is inside one, and it is read right to left,
// skipping trusted hops, so an entry a client put in front is never used.
// With no trusted proxies the client is the peer. Read it with ClientIPFrom.
func ClientIP(trusted string) (Middleware, error) {
	var nets []netip.Prefix
	for _, part := range strings.Split(trusted, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is not a CIDR: %w", part, err)
		}
		nets = append(nets, p)
	}
	isTrusted := func(a netip.Addr) bool {
		a = a.Unmap()
		for _, n := range nets {
			if n.Contains(a) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := peer(r)
			if addr, err := netip.ParseAddr(ip); err == nil && isTrusted(addr) {
				hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
				for i := len(hops) - 1; i >= 0; i-- {
					hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
					if err != nil {
						break // a malformed chain is not evidence of anything
					}
					ip = hop.Unmap().String()
					if !isTrusted(hop) {
						break
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
		})
	}, nil
}

// ClientIPFrom returns the address ClientIP resolved, or the peer address
// when the middleware is not installed.
func ClientIPFrom(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok {
		return ip
	}
	return peer(r)
}

func peer(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
