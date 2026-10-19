package server_test

import (
	"bytes"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	qt "github.com/frankban/quicktest"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/server"
)

const getInfoPath = "/api" + consolev1connect.InfoServiceGetInfoProcedure

var assets = fstest.MapFS{
	"index.html":         {Data: []byte("<!doctype html><title>index</title>")},
	"assets/index-1.js":  {Data: []byte("console.log(1)")},
	"assets/index-1.css": {Data: []byte("body{}")},
	"favicon.svg":        {Data: []byte("<svg/>")},
}

type testServer struct {
	*httptest.Server
	origin string
	log    *bytes.Buffer
}

// newServer starts a console on a loopback port and configures it with the
// origin that port gives it, as ptah-console local does.
func newServer(t *testing.T, adjust ...func(*server.Options)) testServer {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	origin := "http://" + srv.Listener.Addr().String()
	var log bytes.Buffer
	options := server.Options{
		Mode:    consolev1.Mode_MODE_LOCAL,
		Assets:  assets,
		Origins: []string{origin},
		Logger:  slog.New(slog.NewJSONHandler(&log, nil)),
	}
	for _, change := range adjust {
		change(&options)
	}
	handler, err := server.New(options)
	qt.New(t).Assert(err, qt.IsNil)
	srv.Config.Handler = handler
	srv.Start()
	t.Cleanup(srv.Close)
	return testServer{Server: srv, origin: origin, log: &log}
}

// call sends a raw Connect unary request, so a test controls every header.
func (s testServer) call(t *testing.T, header http.Header, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL+getInfoPath, strings.NewReader(body))
	qt.New(t).Assert(err, qt.IsNil)
	request.Header.Set("Content-Type", "application/json")
	maps.Copy(request.Header, header)
	response, err := s.Client().Do(request)
	qt.New(t).Assert(err, qt.IsNil)
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestNewRefusesAnOriginItCannotEnforce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		origins []string
	}{
		{name: "none", origins: nil},
		{name: "a path", origins: []string{"http://127.0.0.1:8080/console"}},
		{name: "credentials", origins: []string{"http://user@127.0.0.1:8080"}},
		{name: "another scheme", origins: []string{"ftp://127.0.0.1:8080"}},
		{name: "no host", origins: []string{"http://"}},
		{name: "a query", origins: []string{"http://127.0.0.1:8080?x=1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			handler, err := server.New(server.Options{Assets: assets, Origins: test.origins})

			c.Assert(err, qt.IsNotNil)
			c.Assert(handler, qt.IsNil)
		})
	}
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
		cache  string
	}{
		{name: "the root", path: "/", status: http.StatusOK, body: "<title>index</title>", cache: "no-cache"},
		{name: "a hashed asset", path: "/assets/index-1.js", status: http.StatusOK, body: "console.log(1)", cache: "public, max-age=31536000, immutable"},
		{name: "an unhashed file", path: "/favicon.svg", status: http.StatusOK, body: "<svg/>", cache: "no-cache"},
		{name: "a route of the app", path: "/approve/application/schema/storefront", status: http.StatusOK, body: "<title>index</title>", cache: "no-cache"},
		// A 404 must not be cached as immutable; the file server drops the
		// header on an error.
		{name: "a missing asset", path: "/assets/index-2.js", status: http.StatusNotFound, body: "404", cache: ""},
		{name: "an unknown API service", path: "/api/ptah.console.v1.NoService/Get", status: http.StatusNotFound, body: "404", cache: "no-store"},
		{name: "the health check", path: "/healthz", status: http.StatusOK, body: "ok", cache: ""},
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
			c.Assert(response.Header.Get("Cache-Control"), qt.Equals, test.cache)
		})
	}
}

// Whatever answers, the browser is told the same rules: nothing from another
// origin, no inline script, no framing, no sniffing.
func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	for _, path := range []string{"/", "/assets/index-1.js", "/approve", "/assets/missing.js", "/healthz", getInfoPath} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			response, err := srv.Client().Get(srv.URL + path)
			c.Assert(err, qt.IsNil)
			defer response.Body.Close()

			csp := response.Header.Get("Content-Security-Policy")
			c.Assert(csp, qt.Contains, "default-src 'self'")
			c.Assert(csp, qt.Contains, "script-src 'self';")
			c.Assert(csp, qt.Contains, "frame-ancestors 'none'")
			c.Assert(csp, qt.Not(qt.Contains), "unsafe")
			c.Assert(response.Header.Get("X-Content-Type-Options"), qt.Equals, "nosniff")
			c.Assert(response.Header.Get("X-Frame-Options"), qt.Equals, "DENY")
			c.Assert(response.Header.Get("Referrer-Policy"), qt.Equals, "no-referrer")
			c.Assert(response.Header.Get("Cross-Origin-Opener-Policy"), qt.Equals, "same-origin")
		})
	}
}

// A page on another site that rebinds its own name to the loopback address
// sends its own name as the Host, and as the Origin. Both match each other,
// which is why the Host is held to the configured origin before anything
// else looks at the request.
func TestAHostTheConsoleDoesNotServeIsRefused(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	tests := []struct {
		name   string
		header http.Header
		host   string
		status int
	}{
		{
			name:   "the configured host",
			host:   srv.Listener.Addr().String(),
			header: http.Header{"Connect-Protocol-Version": {"1"}},
			status: http.StatusOK,
		},
		{
			name:   "a rebound name",
			host:   "attacker.example:80",
			header: http.Header{"Connect-Protocol-Version": {"1"}, "Origin": {"http://attacker.example"}, "Sec-Fetch-Site": {"same-origin"}},
			status: http.StatusMisdirectedRequest,
		},
		{
			name:   "localhost when the console was configured for 127.0.0.1",
			host:   "localhost",
			header: http.Header{"Connect-Protocol-Version": {"1"}},
			status: http.StatusMisdirectedRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+getInfoPath, strings.NewReader("{}"))
			c.Assert(err, qt.IsNil)
			request.Header = test.header
			request.Header.Set("Content-Type", "application/json")
			request.Host = test.host

			response, err := srv.Client().Do(request)
			c.Assert(err, qt.IsNil)
			defer response.Body.Close()

			c.Assert(response.StatusCode, qt.Equals, test.status)
		})
	}
}

func TestACallFromAnotherOriginIsRefused(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	tests := []struct {
		name   string
		header http.Header
		status int
	}{
		{
			name:   "from the console's own page",
			header: http.Header{"Connect-Protocol-Version": {"1"}, "Origin": {srv.origin}, "Sec-Fetch-Site": {"same-origin"}},
			status: http.StatusOK,
		},
		{
			name:   "from another site",
			header: http.Header{"Connect-Protocol-Version": {"1"}, "Origin": {"https://attacker.example"}, "Sec-Fetch-Site": {"cross-site"}},
			status: http.StatusForbidden,
		},
		{
			name:   "from another site, without Sec-Fetch-Site",
			header: http.Header{"Connect-Protocol-Version": {"1"}, "Origin": {"https://attacker.example"}},
			status: http.StatusForbidden,
		},
		{
			// A form or a simple fetch cannot set a custom header without a
			// preflight, and nothing answers a preflight.
			name:   "without the Connect protocol header",
			header: http.Header{"Origin": {srv.origin}},
			status: http.StatusBadRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			response := srv.call(t, test.header, "{}")

			c.Assert(response.StatusCode, qt.Equals, test.status)
		})
	}
}

func TestAnOversizedRequestIsRefusedBeforeItIsRead(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	srv := newServer(t, func(options *server.Options) { options.MaxRequestBytes = 64 })

	response := srv.call(t, http.Header{"Connect-Protocol-Version": {"1"}}, `{"padding":"`+strings.Repeat("x", 100)+`"}`)

	c.Assert(response.StatusCode, qt.Equals, http.StatusRequestEntityTooLarge)
}

// A query string can carry what should never be written down, and is left
// out of the access log.
func TestTheAccessLogLeavesOutTheQuery(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	srv := newServer(t)

	response, err := srv.Client().Get(srv.URL + "/approve?token=do-not-log-this")
	c.Assert(err, qt.IsNil)
	_ = response.Body.Close()

	c.Assert(srv.log.String(), qt.Contains, `"path":"/approve"`)
	c.Assert(srv.log.String(), qt.Not(qt.Contains), "do-not-log-this")
}
