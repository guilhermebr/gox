// Package i18n translates the strings a server owns: error messages, mail
// subjects, labels injected into a page. Catalogs are JSON files named by
// locale (en.json, pt-BR.json) with nested keys addressed by dots; the
// locale of a request comes from a cookie, then Accept-Language.
//
// It does not do plural rules or number and date formatting: keep messages
// free of counts ("Items: {n}") or add a key per case.
package i18n

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Bundle holds the catalogs of every locale.
type Bundle struct {
	fallback string
	messages map[string]map[string]string // locale -> dotted key -> message
	locales  []string
}

// Load reads every <locale>.json at the root of fsys. fallback names the
// locale used when a request's locale, or a key in it, is missing; its
// catalog must exist.
func Load(fsys fs.FS, fallback string) (*Bundle, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("i18n: %w", err)
	}
	b := &Bundle{fallback: fallback, messages: map[string]map[string]string{}}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("i18n: %w", err)
		}
		var tree map[string]any
		if err := json.Unmarshal(raw, &tree); err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", e.Name(), err)
		}
		flat := map[string]string{}
		if err := flatten("", tree, flat); err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", e.Name(), err)
		}
		locale := strings.TrimSuffix(e.Name(), ".json")
		b.messages[locale] = flat
		b.locales = append(b.locales, locale)
	}
	if _, ok := b.messages[fallback]; !ok {
		return nil, fmt.Errorf("i18n: the fallback catalog %s.json is missing", fallback)
	}
	sort.Strings(b.locales)
	return b, nil
}

func flatten(prefix string, tree map[string]any, out map[string]string) error {
	for k, v := range tree {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch x := v.(type) {
		case string:
			out[key] = x
		case map[string]any:
			if err := flatten(key, x, out); err != nil {
				return err
			}
		default:
			return fmt.Errorf("key %q must be a string or an object", key)
		}
	}
	return nil
}

// Locales lists the loaded locales, sorted.
func (b *Bundle) Locales() []string { return append([]string(nil), b.locales...) }

// T returns the message for key in locale, falling back to the fallback
// locale and then to the key itself, so a missing translation is visible
// rather than blank. vars are name, value pairs for {name} placeholders
// (%{name}, as Ruby catalogs write it, works too).
func (b *Bundle) T(locale, key string, vars ...any) string {
	msg, ok := b.messages[locale][key]
	if !ok {
		if msg, ok = b.messages[b.fallback][key]; !ok {
			return key
		}
	}
	for i := 0; i+1 < len(vars); i += 2 {
		name, value := fmt.Sprint(vars[i]), fmt.Sprint(vars[i+1])
		msg = strings.ReplaceAll(msg, "%{"+name+"}", value)
		msg = strings.ReplaceAll(msg, "{"+name+"}", value)
	}
	return msg
}

// Tr is T with the locale Middleware put in ctx.
func (b *Bundle) Tr(ctx context.Context, key string, vars ...any) string {
	return b.T(Locale(ctx), key, vars...)
}

// Messages returns every message under prefix for locale, fallbacks
// included, keyed by full dotted key: what a page needs to hand a frontend
// its strings. An empty prefix returns the whole catalog.
func (b *Bundle) Messages(locale, prefix string) map[string]string {
	out := map[string]string{}
	for _, l := range []string{b.fallback, locale} {
		for k, v := range b.messages[l] {
			if prefix == "" || k == prefix || strings.HasPrefix(k, prefix+".") {
				out[k] = v
			}
		}
	}
	return out
}

// Match picks the best loaded locale for an Accept-Language header: exact
// tags first, then the language of a regional tag (es-MX → es), then a
// regional catalog of a bare language (pt → pt-BR), in quality order.
func (b *Bundle) Match(acceptLanguage string) string {
	type pref struct {
		tag string
		q   float64
	}
	var prefs []pref
	for _, part := range strings.Split(acceptLanguage, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if tag = strings.TrimSpace(tag); tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		prefs = append(prefs, pref{tag, q})
	}
	sort.SliceStable(prefs, func(i, j int) bool { return prefs[i].q > prefs[j].q })
	for _, p := range prefs {
		if l, ok := b.supported(p.tag); ok {
			return l
		}
	}
	return b.fallback
}

func (b *Bundle) supported(tag string) (string, bool) {
	for _, l := range b.locales {
		if strings.EqualFold(l, tag) {
			return l, true
		}
	}
	lang, _, _ := strings.Cut(tag, "-")
	for _, l := range b.locales {
		if strings.EqualFold(l, lang) {
			return l, true
		}
	}
	for _, l := range b.locales {
		if base, _, _ := strings.Cut(l, "-"); strings.EqualFold(base, lang) {
			return l, true
		}
	}
	return "", false
}

type localeKey struct{}

// Middleware resolves the request's locale: the cookie named cookie when it
// names a loaded locale, otherwise Accept-Language, otherwise the fallback.
// It stores it for Locale and Tr and sets Content-Language on the response.
func Middleware(b *Bundle, cookie string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			locale := ""
			if ck, err := r.Cookie(cookie); err == nil {
				if l, ok := b.supported(ck.Value); ok && strings.EqualFold(l, ck.Value) {
					locale = l
				}
			}
			if locale == "" {
				locale = b.Match(r.Header.Get("Accept-Language"))
			}
			w.Header().Set("Content-Language", locale)
			next.ServeHTTP(w, r.WithContext(WithLocale(r.Context(), locale)))
		})
	}
}

// WithLocale stores a locale in ctx, for code outside a request: a job
// rendering a mail in the recipient's language.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeKey{}, locale)
}

// Locale returns the locale stored in ctx, or "" when there is none (T then
// uses the fallback).
func Locale(ctx context.Context) string {
	l, _ := ctx.Value(localeKey{}).(string)
	return l
}
