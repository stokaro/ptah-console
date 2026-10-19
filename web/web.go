//go:build with_frontend

package web

import (
	"embed"
	"io/fs"
)

// The "all:" prefix keeps files whose names begin with "_" or ".", which a
// bundler is free to emit.
//
//go:embed all:app/dist
var dist embed.FS

// Built reports whether this binary carries the web app.
const Built = true

// Assets returns the built web app, rooted where its index.html is.
func Assets() fs.FS {
	assets, err := fs.Sub(dist, "app/dist")
	if err != nil {
		// fs.Sub fails only for an invalid path, and this one is a constant.
		panic(err)
	}
	return assets
}
