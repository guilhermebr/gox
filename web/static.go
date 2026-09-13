package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// manifest maps asset names to content-hashed names and serves them.
type manifest struct {
	fsys   fs.FS
	prefix string
	hashed bool
	toHash map[string]string // css/app.css -> css/app.1a2b3c4d.css
	toName map[string]string // css/app.1a2b3c4d.css -> css/app.css
}

func newManifest(fsys fs.FS, prefix string, hashed bool) (*manifest, error) {
	m := &manifest{fsys: fsys, prefix: prefix, hashed: hashed, toHash: map[string]string{}, toName: map[string]string{}}
	if !hashed {
		return m, nil
	}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		f, err := fsys.Open(name)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		sum := hex.EncodeToString(h.Sum(nil))[:8]
		ext := path.Ext(name)
		hashedName := strings.TrimSuffix(name, ext) + "." + sum + ext
		m.toHash[name] = hashedName
		m.toName[hashedName] = name
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("web: static assets: %w", err)
	}
	return m, nil
}

// url returns the public URL for an asset name.
func (m *manifest) url(name string) string {
	name = strings.TrimPrefix(name, "/")
	if h, ok := m.toHash[name]; ok {
		return m.prefix + h
	}
	return m.prefix + name
}

// handler serves hashed names as immutable and plain names with a short
// cache, both straight from the filesystem.
func (m *manifest) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, m.prefix)
		clean := path.Clean("/" + rel)
		if strings.Contains(clean, "..") || clean == "/" {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(clean, "/")
		if original, ok := m.toName[name]; ok {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			http.ServeFileFS(w, r, m.fsys, original)
			return
		}
		if _, ok := m.toHash[name]; ok || !m.hashed {
			if _, err := fs.Stat(m.fsys, name); err == nil {
				if m.hashed {
					w.Header().Set("Cache-Control", "public, max-age=3600")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				http.ServeFileFS(w, r, m.fsys, name)
				return
			}
		}
		http.NotFound(w, r)
	})
}
