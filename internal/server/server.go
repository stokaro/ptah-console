// Package server is the console's HTTP surface: the Connect API under /api/
// and the web app everywhere else.
package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/stokaro/ptah-console/internal/api"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
)

// APIPrefix is where the Connect services are served.
const APIPrefix = "/api"

// Options configure a console's HTTP surface.
type Options struct {
	Mode     consolev1.Mode
	ReadOnly bool
	// Assets is the built web app, rooted where its index.html is.
	Assets fs.FS
}

// New returns the console's HTTP handler.
func New(options Options) http.Handler {
	rpc := connect.NewServer()
	consolev1connect.RegisterInfoServiceHandler(rpc, api.NewInfo(options.Mode, options.ReadOnly))

	services := http.NewServeMux()
	connecthttp.Mount(services, rpc)

	mux := http.NewServeMux()
	mux.Handle(APIPrefix+"/", http.StripPrefix(APIPrefix, services))
	mux.Handle("/", app(options.Assets))
	return mux
}

// app serves the web app's files, and index.html for any other path without
// a file extension, which is a route the app resolves in the browser. A
// missing asset stays a 404 rather than a page that parses as broken script.
func app(assets fs.FS) http.Handler {
	files := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			files.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(assets, name); err == nil || path.Ext(name) != "" {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFileFS(w, r, assets, "index.html")
	})
}
