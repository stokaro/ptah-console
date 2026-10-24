package hosted

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/auth/clientaddr"
	"github.com/stokaro/ptah-console/internal/auth/oidc"
	"github.com/stokaro/ptah-console/internal/auth/ratelimit"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/server"
)

// bindingCookie carries a sign-in's state from the start to the callback,
// so a callback another browser started finishes nothing here. It is Lax,
// not Strict: the provider sends the browser back from another site, and a
// Strict cookie would not come with it.
const bindingCookie = "__Host-ptah_console_oidc"

// Sign-in starts allowed per client address: a person rarely starts more
// than a few a minute, and each start holds a slot of oidc.MaxAttempts.
const (
	startsEvery = 6 * time.Second
	startsBurst = 10
	startsKeys  = 4096
)

// Why a sign-in did not end in a session, as the sign-in page reads it from
// the address. The codes say nothing the person could not see already.
const (
	refusedThrottled   = "throttled"
	refusedBusy        = "busy"
	refusedProvider    = "provider"
	refusedState       = "state"
	refusedToken       = "token"
	refusedAPIServer   = "refused"
	refusedUnavailable = "unavailable"
	refusedFull        = "full"
)

// errSignedOut ends a credential whose session ended first.
var errSignedOut = errors.New("hosted: the session ended")

// providerError is what an OAuth error code looks like; anything else is
// not repeated in the log.
var providerError = regexp.MustCompile(`^[a-z_]{1,64}$`)

// oidcSignIn serves the two pages a sign-in with an OpenID Connect provider
// goes through: the start, which sends the browser to the provider, and the
// callback it comes back to.
type oidcSignIn struct {
	signer     *oidc.Signer
	factory    *kube.OIDCFactory
	store      *session.Store
	cookies    session.Cookies
	generation uint64
	names      *signInNames
	starts     *ratelimit.Limiter
	addresses  clientaddr.Resolver
	audit      *audit.Logger
	logger     *slog.Logger
	now        func() time.Time
}

func (s *oidcSignIn) pages() map[string]http.Handler {
	return map[string]http.Handler{
		"GET " + oidc.LoginPath:    http.HandlerFunc(s.begin),
		"GET " + oidc.CallbackPath: http.HandlerFunc(s.finish),
	}
}

func (s *oidcSignIn) begin(w http.ResponseWriter, r *http.Request) {
	from := s.addresses.Resolve(r.RemoteAddr, r.Header.Values(clientaddr.Header))
	if !s.starts.Allow(clientaddr.Key(from), s.now()) {
		s.refuse(w, r, refusedThrottled, kube.Principal{}, "too many sign-in starts", from.String())
		return
	}
	authorization, state, err := s.signer.Begin()
	if err != nil {
		s.refuse(w, r, refusedBusy, kube.Principal{}, "too many sign-ins in progress", from.String())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: bindingCookie, Value: state, Path: "/",
		MaxAge:   int(oidc.AttemptLifetime / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, authorization, http.StatusFound)
}

func (s *oidcSignIn) finish(w http.ResponseWriter, r *http.Request) {
	from := s.addresses.Resolve(r.RemoteAddr, r.Header.Values(clientaddr.Header)).String()
	http.SetCookie(w, &http.Cookie{
		Name: bindingCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	query := r.URL.Query()
	if code := query.Get("error"); code != "" {
		reason := "the provider refused"
		if providerError.MatchString(code) {
			reason += ": " + code
		}
		s.refuse(w, r, refusedProvider, kube.Principal{}, reason, from)
		return
	}
	state := query.Get("state")
	bound, err := r.Cookie(bindingCookie)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(bound.Value), []byte(state)) != 1 {
		s.refuse(w, r, refusedState, kube.Principal{}, "the callback is not this browser's sign-in", from)
		return
	}
	credential, identity, err := s.signer.Finish(r.Context(), state, query.Get("code"))
	switch {
	case errors.Is(err, oidc.ErrUnknownAttempt):
		s.refuse(w, r, refusedState, kube.Principal{}, "the sign-in is unknown or expired", from)
		return
	case err != nil:
		s.logger.WarnContext(r.Context(), "an OIDC sign-in did not check out", "error", err)
		s.refuse(w, r, refusedToken, kube.Principal{}, "the provider's answer did not check out", from)
		return
	}
	s.start(w, r, credential, identity, from)
}

// start asks the API server whom the token stands for and, if it accepts
// the token, opens the session. The session and the credential end
// together.
func (s *oidcSignIn) start(w http.ResponseWriter, r *http.Request, credential *oidc.Credential, identity oidc.Identity, from string) {
	signIn, err := newSignIn()
	if err != nil {
		credential.End(err)
		s.refuse(w, r, refusedBusy, kube.Principal{}, "no sign-in ID could be drawn", from)
		return
	}
	principal := kube.Principal{Mode: kube.ModeOIDC, AccountID: signIn, Context: kube.HostedConnection, Generation: s.generation}
	if err := s.factory.Add(signIn, credential); err != nil {
		credential.End(err)
		s.refuse(w, r, refusedBusy, principal, "no client could be built", from)
		return
	}
	abandon := func() {
		s.factory.Remove(signIn)
		credential.End(errSignedOut)
	}
	client, err := s.factory.For(principal)
	if err != nil {
		abandon()
		s.refuse(w, r, refusedBusy, principal, "no client could be built", from)
		return
	}
	who, err := kube.WhoAmI(r.Context(), client)
	switch {
	case apierrors.IsUnauthorized(err) || errors.Is(err, oidc.ErrRefused):
		abandon()
		s.refuse(w, r, refusedAPIServer, principal, "the API server refused the token", from)
		return
	case err != nil:
		abandon()
		s.logger.WarnContext(r.Context(), "the API server did not say whom an OIDC token stands for", "error", err)
		s.refuse(w, r, refusedUnavailable, principal, "the API server did not answer", from)
		return
	}
	if old, ok := s.cookies.Token(r.Header.Values("Cookie")); ok {
		s.store.Revoke(old)
	}
	token, created, err := s.store.Create(principal)
	if err != nil {
		abandon()
		s.refuse(w, r, refusedFull, principal, "too many sessions are open", from)
		return
	}
	s.names.set(signIn, identity.Name)
	context.AfterFunc(created.Context(), func() {
		s.factory.Remove(signIn)
		s.names.forget(signIn)
		credential.End(errSignedOut)
	})
	credential.OnEnd(func(error) {
		s.store.RevokeWhere(func(p kube.Principal) bool { return p == principal })
	})
	http.SetCookie(w, s.cookies.Issue(token))
	s.audit.Record(r.Context(), audit.Event{
		Action: "sign-in", Outcome: audit.Admitted, AccountID: signIn, Identity: who.Username,
		Context: principal.Context, Reason: "oidc", Address: from,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *oidcSignIn) refuse(w http.ResponseWriter, r *http.Request, code string, principal kube.Principal, reason, from string) {
	s.audit.Record(r.Context(), audit.Event{
		Action: "sign-in", Outcome: audit.Refused, AccountID: principal.AccountID,
		Context: principal.Context, Reason: reason, Address: from,
	})
	http.Redirect(w, r, "/login?error="+code, http.StatusSeeOther)
}

// newSignIn is an ID for one sign-in: random, and no secret, since the
// session's own token is what authenticates.
func newSignIn() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "oidc-" + hex.EncodeToString(buffer), nil
}

// signInNames are the names the provider gave the people signed in, for the
// page. Nothing decides by them.
type signInNames struct {
	mu    sync.Mutex
	names map[string]string
}

func newSignInNames() *signInNames { return &signInNames{names: make(map[string]string)} }

func (n *signInNames) set(signIn, name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.names[signIn] = name
}

func (n *signInNames) forget(signIn string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.names, signIn)
}

// of is the name of the principal's sign-in.
func (n *signInNames) of(principal kube.Principal) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.names[principal.AccountID]
}

// maxClientSecretBytes bounds the client secret file.
const maxClientSecretBytes = 4096

// oidcBrowser is the browser's side of a console whose people sign in with
// an OpenID Connect provider: the same services as with accounts, the two
// sign-in pages, and clients that carry each person's own token.
func (o Options) oidcBrowser(ctx context.Context, config *accounts.Config, kubeConfig *rest.Config) (*browserSide, error) {
	secret, err := o.clientSecret()
	if err != nil {
		return nil, err
	}
	origin, err := originOf(config.PublicURL)
	if err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(ctx, config.OIDC, origin, secret, o.Now)
	if err != nil {
		return nil, err
	}
	factory, err := kube.NewOIDCFactory(kubeConfig, generation)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := o.tlsConfig()
	if err != nil {
		return nil, err
	}
	store := session.NewStore(session.Options{})
	cookies := session.Cookies{Secure: true}
	names := newSignInNames()
	signIn := &oidcSignIn{
		signer: oidc.NewSigner(provider, o.Now), factory: factory, store: store, cookies: cookies,
		generation: generation, names: names, starts: ratelimit.New(startsEvery, startsBurst, startsKeys),
		addresses: clientaddr.New(config.Proxies()), audit: o.Audit, logger: o.Logger, now: o.Now,
	}
	sessions := api.NewOIDCSession(store, cookies, provider.Name(), names.of, factory, api.SessionFacts{
		Mode: consolev1.Mode_MODE_HOSTED, SignIn: consolev1.SignIn_SIGN_IN_OIDC, ReadOnly: o.ReadOnly,
	}, o.Audit)
	scope := api.Scope{Candidates: config.Namespaces, Ceiling: config.Namespaces}
	interceptors := []connect.ServerInterceptor{
		session.Interceptor(store, cookies,
			consolev1connect.InfoServiceGetInfoProcedure,
			consolev1connect.SessionServiceLoginProcedure),
	}
	if o.ReadOnly {
		interceptors = append(interceptors, api.ReadOnly(
			consolev1connect.SessionServiceLoginProcedure,
			consolev1connect.SessionServiceLogoutProcedure))
	}
	handler, err := server.New(server.Options{
		Mode:         consolev1.Mode_MODE_HOSTED,
		Info:         api.InfoFacts{SignIn: consolev1.SignIn_SIGN_IN_OIDC, SignInProvider: provider.Name()},
		ReadOnly:     o.ReadOnly,
		Assets:       o.Assets,
		Origins:      []string{origin},
		Logger:       o.Logger,
		Interceptors: interceptors,
		Services: []func(*connect.Server){func(rpc *connect.Server) {
			api.Register(rpc, sessions, factory, scope, o.Audit)
		}},
		Pages: signIn.pages(),
	})
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(o.Stdout, "People sign in with %s; the API server has to trust issuer %s for client %s\n",
		provider.Name(), provider.Issuer(), provider.ClientID())
	return &browserSide{handler: handler, store: store, tls: tlsConfig, origin: origin}, nil
}

// clientSecret reads the client secret file, when one is named.
func (o Options) clientSecret() (string, error) {
	if o.OIDCClientSecretFile == "" {
		return "", nil
	}
	file, err := os.Open(o.OIDCClientSecretFile) // #nosec G304 -- the installer names the file, by flag.
	if err != nil {
		return "", fmt.Errorf("read --oidc-client-secret-file: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxClientSecretBytes+1))
	switch {
	case err != nil:
		return "", fmt.Errorf("read --oidc-client-secret-file: %w", err)
	case len(data) > maxClientSecretBytes:
		return "", fmt.Errorf("--oidc-client-secret-file is larger than %d bytes", maxClientSecretBytes)
	}
	secret := string(bytes.TrimRight(data, "\r\n"))
	if secret == "" {
		return "", errors.New("--oidc-client-secret-file is empty; leave the flag out for a public client")
	}
	return secret, nil
}
