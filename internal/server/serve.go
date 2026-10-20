package server

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// NewHTTPServer wraps the console's handler with the timeouts and limits
// both modes serve with. tlsConfig is nil for plain HTTP.
func NewHTTPServer(handler http.Handler, tlsConfig *tls.Config, logger *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}

// Serve serves on every listener until ctx ends or one of them fails, then
// shuts the server down and sends the failure, or nil, on the channel it
// returns. A server with a TLS configuration serves TLS, HTTP/2 included.
func Serve(ctx context.Context, httpServer *http.Server, listeners []net.Listener) <-chan error {
	// Decided once, before anything serves: the first Serve configures HTTP/2
	// and may set TLSConfig on a server that had none, so a second listener
	// that looked afterwards would serve TLS without a certificate.
	useTLS := httpServer.TLSConfig != nil
	errs := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func() {
			if useTLS {
				errs <- httpServer.ServeTLS(listener, "", "")
				return
			}
			errs <- httpServer.Serve(listener)
		}()
	}
	done := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
		case err := <-errs:
			if !errors.Is(err, http.ErrServerClosed) {
				done <- err
				_ = httpServer.Close()
				return
			}
		}
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
		done <- nil
	}()
	return done
}
