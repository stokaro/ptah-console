// Package server is the console's HTTP surface: the Connect API under /api/
// and the web app everywhere else.
//
// Every request passes the same gates in the same order. The Host has to be
// one the console was configured to answer to, which is what stops a page
// that rebinds its DNS name to the loopback listener. A request that names an
// identity in Impersonate-* or X-Remote-* headers is refused. A request to the API
// that changes anything has to come from the console's own origin. Responses
// carry a Content Security Policy that allows nothing from another origin and
// no inline script, and API responses are never cached.
package server

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/stokaro/ptah-operator/pkg/planread"

	"github.com/stokaro/ptah-console/internal/api"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
)

// APIPrefix is where the Connect services are served.
const APIPrefix = "/api"

// HealthPath answers "ok" to anyone who asks.
const HealthPath = "/healthz"

// Defaults for the limits Options leaves at zero.
const (
	DefaultMaxRequestBytes = 1 << 20
	DefaultMaxInFlight     = 64
	DefaultUnaryTimeout    = 30 * time.Second
	// DefaultMaxResponseBytes fits the largest plan the operator stores in
	// the JSON a browser reads, where a control character in the SQL takes
	// six bytes, with room for the rest of the message.
	DefaultMaxResponseBytes = 6*planread.MaxPlanBytes + 1<<20
)

// Options configure a console's HTTP surface.
type Options struct {
	Mode     consolev1.Mode
	ReadOnly bool
	// Assets is the built web app, rooted where its index.html is.
	Assets fs.FS
	// Origins are the exact origins the console is served from, such as
	// "http://127.0.0.1:8443". A request's Host has to be one of their hosts,
	// and a cross-origin request that changes anything is refused. At least
	// one is required.
	Origins []string
	// Logger receives the access log. Nil discards it.
	Logger *slog.Logger

	// MaxRequestBytes bounds a request body.
	MaxRequestBytes int64
	// MaxResponseBytes bounds one response message.
	MaxResponseBytes int64
	// MaxInFlight bounds the API requests served at once. Streams count
	// while they are open.
	MaxInFlight int
	// UnaryTimeout bounds one unary call, whatever the client asked for.
	UnaryTimeout time.Duration

	// Interceptors run on every call, in order, after panic recovery and the
	// unary timeout. Authentication goes here.
	Interceptors []connect.ServerInterceptor
	// Services register the console's services beside InfoService.
	Services []func(*connect.Server)
}

// New returns the console's HTTP handler.
func New(options Options) (http.Handler, error) {
	if options.Assets == nil {
		return nil, errors.New("server: the web app's assets are required")
	}
	hosts, crossOrigin, err := originGates(options.Origins)
	if err != nil {
		return nil, err
	}
	if options.MaxRequestBytes <= 0 {
		options.MaxRequestBytes = DefaultMaxRequestBytes
	}
	if options.MaxResponseBytes <= 0 {
		options.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if options.MaxInFlight <= 0 {
		options.MaxInFlight = DefaultMaxInFlight
	}
	if options.UnaryTimeout <= 0 {
		options.UnaryTimeout = DefaultUnaryTimeout
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	interceptors := append([]connect.ServerInterceptor{
		recoverInterceptor(logger), unaryTimeoutInterceptor(options.UnaryTimeout),
	}, options.Interceptors...)
	rpc := connect.NewServer(interceptors...)
	consolev1connect.RegisterInfoServiceHandler(rpc, api.NewInfo(options.Mode, options.ReadOnly))
	for _, register := range options.Services {
		register(rpc)
	}

	services := http.NewServeMux()
	connecthttp.Mount(services, rpc,
		connecthttp.WithReadMaxBytes(int(options.MaxRequestBytes)),
		connecthttp.WithSendMaxBytes(int(options.MaxResponseBytes)),
		// A browser cannot send this header cross-origin without a preflight,
		// which the console never answers, so a form or a simple fetch from
		// another page never reaches a handler.
		connecthttp.WithRequireConnectProtocolHeader(),
	)

	var apiHandler http.Handler = services
	apiHandler = limitBody(apiHandler, options.MaxRequestBytes)
	apiHandler = crossOrigin.Handler(apiHandler)
	apiHandler = limitInFlight(apiHandler, options.MaxInFlight)
	apiHandler = noStore(apiHandler)

	mux := http.NewServeMux()
	mux.Handle(APIPrefix+"/", http.StripPrefix(APIPrefix, apiHandler))
	mux.HandleFunc("GET "+HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/", app(options.Assets))

	var handler http.Handler = mux
	handler = securityHeaders(handler)
	handler = recoverHTTP(handler, logger)
	handler = refuseIdentityHeaders(handler)
	handler = requireHost(handler, hosts)
	handler = accessLog(handler, logger)
	return handler, nil
}

// originGates turns the configured origins into the set of acceptable Host
// headers and a cross-origin protection that trusts exactly those origins.
func originGates(origins []string) (map[string]bool, *http.CrossOriginProtection, error) {
	if len(origins) == 0 {
		return nil, nil, errors.New("server: at least one origin is required")
	}
	hosts := make(map[string]bool, len(origins))
	protection := http.NewCrossOriginProtection()
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
			parsed.Path != "" || parsed.RawQuery != "" || parsed.User != nil {
			return nil, nil, fmt.Errorf("server: %q is not an origin of the form scheme://host[:port]", origin)
		}
		hosts[parsed.Host] = true
		if err := protection.AddTrustedOrigin(origin); err != nil {
			return nil, nil, fmt.Errorf("server: trust origin %q: %w", origin, err)
		}
	}
	return hosts, protection, nil
}
