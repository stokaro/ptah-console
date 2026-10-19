package session

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect/v2"
)

// Cookie names. The hosted cookie carries the __Host- prefix, which the
// browser accepts only with Secure, Path=/ and no Domain, so no other host
// under the same domain can set or read it.
const (
	LocalCookieName  = "ptah_console"
	HostedCookieName = "__Host-ptah_console"
)

// ActionHeader marks a request a person made, as opposed to a background
// refresh. Only marked requests extend the idle timeout.
const (
	ActionHeader = "Ptah-Action"
	ActionValue  = "1"
)

// Cookies issues and reads the session cookie.
type Cookies struct {
	// Secure is true when the console is served over HTTPS.
	Secure bool
}

// Name is the cookie's name.
func (c Cookies) Name() string {
	if c.Secure {
		return HostedCookieName
	}
	return LocalCookieName
}

// Issue returns the cookie that carries token. It lasts as long as the
// browser session; the server ends the session on its own clock.
func (c Cookies) Issue(token string) *http.Cookie {
	// #nosec G124 -- Secure follows the scheme: hosted mode serves HTTPS and
	// sets it; local mode serves loopback HTTP, where not every browser keeps a
	// Secure cookie, and the listener refuses every Host but its own.
	return &http.Cookie{
		Name:     c.Name(),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// Clear returns a cookie that removes the session cookie.
func (c Cookies) Clear() *http.Cookie {
	cookie := c.Issue("") // #nosec G124 -- the attributes are Issue's; see there.
	cookie.MaxAge = -1
	return cookie
}

// Token reads the session token out of the values of a request's Cookie
// header.
func (c Cookies) Token(lines []string) (string, bool) {
	for _, line := range lines {
		cookies, err := http.ParseCookie(line)
		if err != nil {
			continue
		}
		for _, cookie := range cookies {
			if cookie.Name == c.Name() && cookie.Value != "" {
				return cookie.Value, true
			}
		}
	}
	return "", false
}

type contextKey struct{}

// FromContext returns the session a call was authenticated with.
func FromContext(ctx context.Context) (*Session, bool) {
	session, ok := ctx.Value(contextKey{}).(*Session)
	return session, ok
}

// Interceptor authenticates every call except the procedures in public, which
// are the ones a browser makes before it has a session. It puts the session in
// the call's context, ends the call when the session ends, and refuses a call
// over the session's concurrency limit.
func Interceptor(store *Store, cookies Cookies, public ...string) connect.ServerInterceptor {
	open := make(map[string]bool, len(public))
	for _, procedure := range public {
		open[procedure] = true
	}
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			if open[spec.Procedure] {
				return next(ctx, spec, stream)
			}
			info, _ := connect.CallInfoForServerContext(ctx)
			token, ok := cookies.Token(info.RequestHeader().Values("Cookie"))
			if !ok {
				return connect.NewError(connect.CodeUnauthenticated, "sign in first")
			}
			use := Background
			if info.RequestHeader().Get(ActionHeader) == ActionValue {
				use = Action
			}
			session, err := store.Lookup(token, use)
			switch {
			case errors.Is(err, ErrExpired):
				return connect.NewError(connect.CodeUnauthenticated, "the session expired; sign in again")
			case err != nil:
				return connect.NewError(connect.CodeUnauthenticated, "sign in first")
			}
			release, ok := session.Acquire()
			if !ok {
				return connect.NewError(connect.CodeResourceExhausted, "this session has too many requests in flight")
			}
			defer release()

			ctx, cancel := context.WithCancel(context.WithValue(ctx, contextKey{}, session))
			defer cancel()
			stop := context.AfterFunc(session.Context(), cancel)
			defer stop()
			return next(ctx, spec, stream)
		}
	}
}
