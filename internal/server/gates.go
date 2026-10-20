package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
)

// contentSecurityPolicy allows the page to load only its own files and to
// talk only to its own origin, and never to run inline script or be framed.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; " +
	"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; " +
	"frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		header.Set("Cross-Origin-Resource-Policy", "same-origin")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		next.ServeHTTP(w, r)
	})
}

// requireHost refuses a request whose Host the console was not configured to
// answer to. A page that rebinds its own DNS name to the loopback address
// sends its own name as the Host, and stops here. The health check is the
// exception: a kubelet probes it by the Pod's address, and it answers "ok"
// and nothing else.
func requireHost(next http.Handler, hosts map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] && (r.Method != http.MethodGet || r.URL.Path != HealthPath) {
			http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// refuseIdentityHeaders refuses a request that names an identity in its
// headers. The console takes whom it acts for from the session and its own
// configuration, never from the browser, and client-go builds every request to
// the API server afresh, so these headers would reach nothing. Refusing them
// keeps a proxy in front of the console, or one between it and the API server,
// from ever being handed one.
func refuseIdentityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.Header {
			lower := strings.ToLower(name)
			if strings.HasPrefix(lower, "impersonate-") || strings.HasPrefix(lower, "x-remote-") {
				http.Error(w, "identity headers are not accepted", http.StatusBadRequest)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// noStore keeps API responses out of every cache, the browser's included.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// limitBody refuses a declared body over the limit before any handler reads
// it, and cuts off an undeclared one at the limit.
func limitBody(next http.Handler, limit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > limit {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

// limitInFlight answers 503 rather than queue once the limit is reached.
func limitInFlight(next http.Handler, limit int) http.Handler {
	slots := make(chan struct{}, limit)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests in flight", http.StatusServiceUnavailable)
		}
	})
}

// app serves the web app's files, and index.html for any other path without
// a file extension, which is a route the app resolves in the browser. A
// missing asset stays a 404 rather than a page that parses as broken script.
// Hashed assets never change under their name; everything else is revalidated.
func app(assets fs.FS) http.Handler {
	files := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		switch {
		case strings.HasPrefix(name, "assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
		case name == "" || exists(assets, name) || path.Ext(name) != "":
			w.Header().Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
		default:
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, assets, "index.html")
		}
	})
}

func exists(assets fs.FS, name string) bool {
	_, err := fs.Stat(assets, name)
	return err == nil
}

// recoverHTTP turns a panic outside the Connect handlers into a 500 and a
// log line, without the panic value reaching the response.
func recoverHTTP(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				logger.Error("panic serving request", "path", r.URL.Path,
					"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// recoverInterceptor does the same inside a Connect call.
func recoverInterceptor(logger *slog.Logger) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic serving call", "procedure", spec.Procedure,
						"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
					err = connect.NewError(connect.CodeInternal, "internal error")
				}
			}()
			return next(ctx, spec, stream)
		}
	}
}

// unaryTimeoutInterceptor bounds a unary call however long the client asked
// to wait. Streams set their own bounds.
func unaryTimeoutInterceptor(timeout time.Duration) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			if spec.StreamType != connect.StreamTypeUnary {
				return next(ctx, spec, stream)
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return next(ctx, spec, stream)
		}
	}
}

// accessLog records each request without its query string or body: a query
// can carry what should not be written down, and a body carries SQL.
func accessLog(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"bytes", recorder.bytes,
			"duration", time.Since(started))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

// Flush keeps server streams working through the recorder.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
