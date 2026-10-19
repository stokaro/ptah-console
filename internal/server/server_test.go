package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	qt "github.com/frankban/quicktest"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/server"
)

var assets = fstest.MapFS{
	"index.html":         {Data: []byte("<!doctype html><title>index</title>")},
	"assets/index-1.js":  {Data: []byte("console.log(1)")},
	"favicon.svg":        {Data: []byte("<svg/>")},
	"assets/index-1.css": {Data: []byte("body{}")},
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := server.New(server.Options{Mode: consolev1.Mode_MODE_LOCAL, Assets: assets})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// The browser's transport uses /api as its base URL, so the procedure path
// is the rest of the route.
func TestTheAPIIsServedUnderItsPrefix(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	srv := newServer(t)
	client := consolev1connect.NewInfoServiceClient(connect.NewClient(
		connecthttp.NewTransport(srv.Client(), srv.URL+server.APIPrefix)))

	info, err := client.GetInfo(t.Context(), &consolev1.GetInfoRequest{})

	c.Assert(err, qt.IsNil)
	c.Assert(info.GetMode(), qt.Equals, consolev1.Mode_MODE_LOCAL)
}

func TestTheAppIsServedAndRoutesFallBackToIndex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		path   string
		status int
		body   string
	}{
		{name: "the root", path: "/", status: http.StatusOK, body: "<title>index</title>"},
		{name: "an asset", path: "/assets/index-1.js", status: http.StatusOK, body: "console.log(1)"},
		{name: "a route of the app", path: "/approve/application/schema/storefront", status: http.StatusOK, body: "<title>index</title>"},
		{name: "a missing asset", path: "/assets/index-2.js", status: http.StatusNotFound, body: "404"},
		{name: "an unknown API service", path: "/api/ptah.console.v1.NoService/Get", status: http.StatusNotFound, body: "404"},
	}
	srv := newServer(t)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			response, err := srv.Client().Get(srv.URL + test.path)
			c.Assert(err, qt.IsNil)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)

			c.Assert(err, qt.IsNil)
			c.Assert(response.StatusCode, qt.Equals, test.status)
			c.Assert(string(body), qt.Contains, test.body)
		})
	}
}
