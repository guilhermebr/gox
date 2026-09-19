// Package static embeds the example's assets; web.WithStatic(FS) serves
// them under /static/ and page.Asset("css/app.css") resolves the URL.
package static

import "embed"

// FS holds css/.
//
//go:embed all:css
var FS embed.FS
