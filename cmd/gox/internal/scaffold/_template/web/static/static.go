// Package static embeds the site's assets. web.WithStatic(FS) serves them
// under /static/ with content-hashed URLs in production; templates resolve
// names with page.Asset("css/app.css").
package static

import "embed"

// FS holds css/, js/ and img/ (run `make assets` to vendor htmx and Alpine.js).
//
//go:embed all:css all:js
var FS embed.FS
