// Package static embeds the site's assets. web.WithStatic(FS) serves them
// under /static/ with content-hashed URLs in production; templates resolve
// names with page.Asset("css/app.css").
package static

import "embed"

// FS holds css/ and js/ (run `make assets` to vendor htmx and Alpine.js). A
// new directory (img/) is served only once it is added to the line below.
//
//go:embed all:css all:js
var FS embed.FS
