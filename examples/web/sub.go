package main

import (
	"embed"
	"io/fs"
)

// mustSub strips the embed directory prefix so asset names are "css/app.css",
// not "static/css/app.css".
func mustSub(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
