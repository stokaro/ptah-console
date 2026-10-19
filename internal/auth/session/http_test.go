package session_test

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
)

func TestCookiesAreHostOnlyAndOutOfReachOfScript(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		secure  bool
		cookie  string
		wantSec bool
	}{
		{name: "local, over loopback HTTP", secure: false, cookie: session.LocalCookieName, wantSec: false},
		{name: "hosted, over HTTPS", secure: true, cookie: session.HostedCookieName, wantSec: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			cookies := session.Cookies{Secure: test.secure}

			issued := cookies.Issue("token")
			cleared := cookies.Clear()

			c.Assert(issued.Name, qt.Equals, test.cookie)
			c.Assert(issued.HttpOnly, qt.IsTrue)
			c.Assert(issued.SameSite, qt.Equals, http.SameSiteStrictMode)
			c.Assert(issued.Secure, qt.Equals, test.wantSec)
			c.Assert(issued.Path, qt.Equals, "/")
			c.Assert(issued.Domain, qt.Equals, "")
			c.Assert(cleared.MaxAge, qt.Equals, -1)
		})
	}
}

func TestTokenFindsTheSessionCookieAmongOthers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lines []string
		token string
		found bool
	}{
		{name: "alone", lines: []string{"ptah_console=abc"}, token: "abc", found: true},
		{name: "among others", lines: []string{"theme=dark; ptah_console=abc; other=1"}, token: "abc", found: true},
		{name: "in a second header line", lines: []string{"theme=dark", "ptah_console=abc"}, token: "abc", found: true},
		{name: "the hosted name on a local console", lines: []string{"__Host-ptah_console=abc"}, found: false},
		{name: "empty", lines: []string{"ptah_console="}, found: false},
		{name: "absent", lines: nil, found: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			token, found := session.Cookies{}.Token(test.lines)

			c.Assert(found, qt.Equals, test.found)
			c.Assert(token, qt.Equals, test.token)
		})
	}
}

// whoami answers with the principal of the session the call carried, or
// waits until the call ends when told to.
type whoami struct {
	consolev1connect.UnimplementedInfoServiceHandler
	entered chan struct{}
	block   bool
}

func (w *whoami) GetInfo(ctx context.Context, _ *consolev1.GetInfoRequest) (*consolev1.GetInfoResponse, error) {
	if w.block {
		w.entered <- struct{}{}
		<-ctx.Done()
		return nil, connect.NewError(connect.CodeCanceled, "the call ended")
	}
	s, ok := session.FromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, "no session in the context")
	}
	return &consolev1.GetInfoResponse{Version: s.Principal().Context}, nil
}

func newGuarded(store *session.Store, handler *whoami, public ...string) consolev1connect.InfoServiceClient {
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}, public...))
	consolev1connect.RegisterInfoServiceHandler(server, handler)
	return consolev1connect.NewInfoServiceClient(connect.NewClient(connectinprocess.New(server)))
}

// call sends the session cookie, and the action header for a person's action.
func call(ctx context.Context, client consolev1connect.InfoServiceClient, token string, use session.Use) (*consolev1.GetInfoResponse, error) {
	ctx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	info.RequestHeader().Set(session.ActionHeader, map[session.Use]string{session.Action: session.ActionValue, session.Background: "0"}[use])
	return client.GetInfo(ctx, &consolev1.GetInfoRequest{})
}

func TestTheInterceptorLetsOnlyALiveSessionThrough(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c := qt.New(t)
		store := session.NewStore(session.Options{})
		token, _, err := store.Create(local)
		c.Assert(err, qt.IsNil)
		client := newGuarded(store, &whoami{})

		live, liveErr := call(t.Context(), client, token, session.Background)
		_, wrongErr := call(t.Context(), client, "not-a-token", session.Background)
		_, noCookieErr := client.GetInfo(t.Context(), &consolev1.GetInfoRequest{})
		time.Sleep(30 * time.Minute)
		_, expiredErr := call(t.Context(), client, token, session.Action)

		c.Assert(liveErr, qt.IsNil)
		c.Assert(live.GetVersion(), qt.Equals, "production")
		c.Assert(connect.CodeOf(wrongErr), qt.Equals, connect.CodeUnauthenticated)
		c.Assert(connect.CodeOf(noCookieErr), qt.Equals, connect.CodeUnauthenticated)
		c.Assert(connect.CodeOf(expiredErr), qt.Equals, connect.CodeUnauthenticated)
		c.Assert(expiredErr, qt.ErrorMatches, ".*expired.*")
	})
}

// The person's action header is what extends the session through the
// interceptor; a call without it reads the session and leaves the clock.
func TestTheInterceptorPassesThePersonsActionOn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c := qt.New(t)
		store := session.NewStore(session.Options{})
		token, _, err := store.Create(local)
		c.Assert(err, qt.IsNil)
		client := newGuarded(store, &whoami{})

		time.Sleep(20 * time.Minute)
		_, actionErr := call(t.Context(), client, token, session.Action)
		time.Sleep(20 * time.Minute)
		_, refreshErr := call(t.Context(), client, token, session.Background)
		time.Sleep(10 * time.Minute)
		_, laterErr := call(t.Context(), client, token, session.Background)

		c.Assert(actionErr, qt.IsNil)
		c.Assert(refreshErr, qt.IsNil)
		c.Assert(connect.CodeOf(laterErr), qt.Equals, connect.CodeUnauthenticated)
	})
}

func TestAPublicProcedureNeedsNoSession(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	client := newGuarded(session.NewStore(session.Options{}), &whoami{block: false}, consolev1connect.InfoServiceGetInfoProcedure)

	_, err := client.GetInfo(t.Context(), &consolev1.GetInfoRequest{})

	// The handler is reached; it finds no session because none was needed.
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeInternal)
}

// Logout ends a call that is still running, which is what closes a stream.
func TestRevokingTheSessionEndsACallInFlight(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c := qt.New(t)
		store := session.NewStore(session.Options{})
		token, _, err := store.Create(local)
		c.Assert(err, qt.IsNil)
		handler := &whoami{block: true, entered: make(chan struct{}, 1)}
		client := newGuarded(store, handler)

		done := make(chan error, 1)
		go func() {
			_, err := call(context.Background(), client, token, session.Action)
			done <- err
		}()
		<-handler.entered
		store.Revoke(token)

		c.Assert(connect.CodeOf(<-done), qt.Equals, connect.CodeCanceled)
	})
}

func TestASessionOverItsConcurrencyIsRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c := qt.New(t)
		store := session.NewStore(session.Options{MaxConcurrency: 1})
		token, _, err := store.Create(local)
		c.Assert(err, qt.IsNil)
		handler := &whoami{block: true, entered: make(chan struct{}, 1)}
		client := newGuarded(store, handler)
		ctx, cancel := context.WithCancel(context.Background())

		first := make(chan error, 1)
		go func() {
			_, err := call(ctx, client, token, session.Action)
			first <- err
		}()
		<-handler.entered
		_, second := call(context.Background(), client, token, session.Action)
		cancel()

		c.Assert(connect.CodeOf(second), qt.Equals, connect.CodeResourceExhausted)
		c.Assert(connect.CodeOf(<-first), qt.Equals, connect.CodeCanceled)
	})
}
