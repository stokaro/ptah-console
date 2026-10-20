package server_test

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/server"
)

// A plain server on several listeners serves plain HTTP on every one of
// them, however the first Serve sets the server up.
func TestAPlainServerServesPlainHTTPOnEveryListener(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	listeners := make([]net.Listener, 4)
	for i := range listeners {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		c.Assert(err, qt.IsNil)
		listeners[i] = listener
	}
	ctx, cancel := context.WithCancel(t.Context())
	served := server.Serve(ctx, server.NewHTTPServer(handler, nil, slog.New(slog.DiscardHandler)), listeners)

	// A listener that serves TLS by mistake never answers a plain request.
	client := &http.Client{Timeout: 5 * time.Second}
	statuses := make([]int, len(listeners))
	for i, listener := range listeners {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+listener.Addr().String()+"/", nil)
		c.Assert(err, qt.IsNil)
		response, err := client.Do(request)
		c.Assert(err, qt.IsNil)
		_ = response.Body.Close()
		statuses[i] = response.StatusCode
	}
	cancel()

	c.Assert(<-served, qt.IsNil)
	c.Assert(statuses, qt.DeepEquals, []int{http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK})
}
