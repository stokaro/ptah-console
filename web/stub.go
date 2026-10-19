//go:build !with_frontend

package web

import (
	"embed"
	"io/fs"
)

//go:embed stub
var stub embed.FS

// Built reports whether this binary carries the web app.
const Built = false

// Assets returns a page saying the binary was built without the web app.
func Assets() fs.FS {
	assets, err := fs.Sub(stub, "stub")
	if err != nil {
		// fs.Sub fails only for an invalid path, and this one is a constant.
		panic(err)
	}
	return assets
}
