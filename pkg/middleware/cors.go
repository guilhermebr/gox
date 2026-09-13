package middleware

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// CORSConfig configures CORS. It is never on by default: a same-origin
// service has no use for it, and one existing service shipped a wildcard
// origin with credentials by copy-paste.
type CORSConfig struct {
	AllowedOrigins   []string // exact origins, or "*" (never with credentials)
	AllowedMethods   []string // default: GET, HEAD, POST, PUT, PATCH, DELETE
	AllowedHeaders   []string // default: Content-Type, Authorization, X-Request-ID
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           time.Duration
}

// CORS answers preflight requests and adds the allow headers to responses
// for allowed origins. It panics on the one configuration browsers reject
// anyway: "*" together with credentials.
func CORS(cfg CORSConfig) Middleware {
	if cfg.AllowCredentials && slices.Contains(cfg.AllowedOrigins, "*") {
		panic("middleware: CORS: AllowedOrigins \"*\" cannot be combined with AllowCredentials")
	}
	if len(cfg.AllowedMethods) == 0 {
		cfg.AllowedMethods = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	}
	if len(cfg.AllowedHeaders) == 0 {
		cfg.AllowedHeaders = []string{"Content-Type", "Authorization", HeaderRequestID}
	}
	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")
	exposed := strings.Join(cfg.ExposedHeaders, ", ")
	wildcard := slices.Contains(cfg.AllowedOrigins, "*")

	allowed := func(origin string) bool {
		return origin != "" && (wildcard || slices.Contains(cfg.AllowedOrigins, origin))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			w.Header().Add("Vary", "Origin")
			if !allowed(origin) {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			if wildcard && !cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			if cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if exposed != "" {
				h.Set("Access-Control-Expose-Headers", exposed)
			}

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", methods)
				h.Set("Access-Control-Allow-Headers", headers)
				if cfg.MaxAge > 0 {
					h.Set("Access-Control-Max-Age", strconv.Itoa(int(cfg.MaxAge.Seconds())))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
