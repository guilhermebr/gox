package httpserver

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	gerrors "github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/httpx"
	"github.com/guilhermebr/gox/pkg/log"
)

// SPAConfig describes a single-page application served by the same process
// as its API.
type SPAConfig struct {
	// FS holds the built application: index.html at the root next to its
	// assets (the output directory of Vite, esbuild or webpack).
	FS fs.FS
	// ServerPrefixes are paths the server owns ("/api/", "/webhooks/"): a
	// request under one that matches no route is a 404 error, never the
	// shell.
	ServerPrefixes []string
	// Immutable lists the path prefixes whose files are content-hashed and
	// can be cached forever. Default: "/assets/", where Vite and most
	// bundlers put them. Everything else revalidates.
	Immutable []string
	// Head returns HTML inserted before </head> of the shell on every
	// request: meta tags carrying per-request values such as a CSRF token,
	// the locale or public configuration. It must escape what it prints.
	Head func(r *http.Request) string
}

// NewSPA returns the catch-all handler: files from cfg.FS at their paths,
// the shell (index.html) for every other GET so client-side routes deep-link,
// and the error envelope for everything that is neither.
func NewSPA(cfg SPAConfig) (http.Handler, error) {
	if cfg.FS == nil {
		return nil, errors.New("WithSPA: SPAConfig.FS is nil")
	}
	index, err := fs.ReadFile(cfg.FS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("WithSPA: index.html not found at the root of SPAConfig.FS: %w", err)
	}
	if cfg.Immutable == nil {
		cfg.Immutable = []string{"/assets/"}
	}
	before, after := index, []byte(nil)
	if i := bytes.Index(bytes.ToLower(index), []byte("</head>")); i >= 0 {
		before, after = index[:i], index[i:]
	}
	s := &spa{cfg: cfg, before: before, after: after}
	return s, nil
}

type spa struct {
	cfg           SPAConfig
	before, after []byte
}

func (s *spa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || s.serverOwned(r.URL.Path) {
		s.notFound(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "index.html" && s.serveFile(w, r, name) {
		return
	}
	if path.Ext(name) != "" && name != "index.html" {
		s.notFound(w, r) // a missing asset is an error, not a page
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(s.before)
	if s.cfg.Head != nil {
		_, _ = io.WriteString(w, s.cfg.Head(r))
	}
	_, _ = w.Write(s.after)
}

func (s *spa) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	f, err := s.cfg.FS.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}
	cache := "no-cache"
	for _, p := range s.cfg.Immutable {
		if strings.HasPrefix("/"+name, p) {
			cache = "public, max-age=31536000, immutable"
		}
	}
	w.Header().Set("Cache-Control", cache)
	http.ServeContent(w, r, name, time.Time{}, rs)
	return true
}

func (s *spa) serverOwned(p string) bool {
	for _, prefix := range s.cfg.ServerPrefixes {
		if strings.HasPrefix(p, prefix) || p == strings.TrimSuffix(prefix, "/") {
			return true
		}
	}
	return false
}

func (s *spa) notFound(w http.ResponseWriter, r *http.Request) {
	status, env := gerrors.ToEnvelope(gerrors.NotFound("no route for %s %s", r.Method, r.URL.Path), log.RequestID(r.Context()))
	httpx.WriteError(w, r, status, env)
}
