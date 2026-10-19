// Package web holds the console's web app for the Go binary to serve.
//
// The app is built by npm into app/dist. A binary built with the
// with_frontend tag embeds that directory; without the tag it carries a page
// that says so, which keeps `go build` and `go test` free of a Node toolchain.
//
// web is a module of its own, like denisvmedia/inventario's frontend, so the
// Go tools never walk node_modules from the repository root.
package web
