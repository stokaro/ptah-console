package server

// White-box testing required: limitInFlight and recoverHTTP are tested on
// their own because driving them through New would need a handler that blocks
// or panics, and the console registers none.

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
)

// The limit is a count of requests being served, and the one over it is told
// to come back rather than queued behind the others.
func TestLimitInFlightRefusesTheRequestOverTheLimit(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	handler := limitInFlight(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}), 2)

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Go(func() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			statuses[i] = recorder.Code
		})
	}
	<-entered
	<-entered
	over := httptest.NewRecorder()
	handler.ServeHTTP(over, httptest.NewRequest(http.MethodGet, "/", nil))
	close(release)
	wg.Wait()

	c.Assert(over.Code, qt.Equals, http.StatusServiceUnavailable)
	c.Assert(over.Header().Get("Retry-After"), qt.Equals, "1")
	c.Assert(statuses, qt.DeepEquals, []int{http.StatusOK, http.StatusOK})
}

// A panic becomes a 500 and a log line; its value never reaches the client.
func TestRecoverHTTPKeepsThePanicOnTheServer(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	var log bytes.Buffer
	handler := recoverHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret detail")
	}), slog.New(slog.NewJSONHandler(&log, nil)))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	c.Assert(recorder.Code, qt.Equals, http.StatusInternalServerError)
	c.Assert(recorder.Body.String(), qt.Not(qt.Contains), "secret detail")
	c.Assert(log.String(), qt.Contains, "secret detail")
}
