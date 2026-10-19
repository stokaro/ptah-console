package api

import (
	"context"
	"errors"

	"connectrpc.com/connect/v2"

	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/launch"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// SessionFacts are what the console knows about how it acts, beside who it
// acts as.
type SessionFacts struct {
	Mode     consolev1.Mode
	ReadOnly bool
	// Impersonates is who the kubeconfig itself asks to impersonate.
	Impersonates string
	// InsecureTLS is true when the kubeconfig turns off certificate checks.
	InsecureTLS bool
}

// Session serves SessionService.
type Session struct {
	consolev1connect.UnimplementedSessionServiceHandler

	store   *session.Store
	cookies session.Cookies
	factory kube.ClientFactory
	facts   SessionFacts
	audit   *audit.Logger

	// Local mode signs a browser in with a launch token, for the one
	// principal the process acts for.
	launcher  *launch.Launcher
	principal kube.Principal
}

// NewLocalSession returns the SessionService of ptah-console local.
func NewLocalSession(
	store *session.Store,
	cookies session.Cookies,
	launcher *launch.Launcher,
	principal kube.Principal,
	factory kube.ClientFactory,
	facts SessionFacts,
	auditLog *audit.Logger,
) *Session {
	return &Session{
		store: store, cookies: cookies, factory: factory, facts: facts, audit: auditLog,
		launcher: launcher, principal: principal,
	}
}

// ExchangeLaunchToken trades a launch token for a session cookie. A cookie
// the browser already had is revoked, so signing in always rotates.
func (s *Session) ExchangeLaunchToken(ctx context.Context, request *consolev1.ExchangeLaunchTokenRequest) (*consolev1.ExchangeLaunchTokenResponse, error) {
	if s.launcher == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, "this console signs in with an account, not a launch link")
	}
	if err := s.launcher.Redeem(request.GetToken()); err != nil {
		s.record(ctx, "sign-in", audit.Refused, s.principal, "launch token not valid")
		return nil, connect.NewError(connect.CodePermissionDenied,
			"this link is not valid any more; press Enter where ptah-console local runs to open a new one").WithCause(err)
	}
	info, _ := connect.CallInfoForServerContext(ctx)
	if old, ok := s.cookies.Token(info.RequestHeader().Values("Cookie")); ok {
		s.store.Revoke(old)
	}
	token, _, err := s.store.Create(s.principal)
	if errors.Is(err, session.ErrFull) {
		return nil, connect.NewError(connect.CodeResourceExhausted, "too many sessions are open").WithCause(err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "the session could not be started").WithCause(err)
	}
	info.ResponseHeader().Add("Set-Cookie", s.cookies.Issue(token).String())
	s.record(ctx, "sign-in", audit.Admitted, s.principal, "launch token")
	return &consolev1.ExchangeLaunchTokenResponse{}, nil
}

// GetSession says whom the session acts for, as the API server sees it.
func (s *Session) GetSession(ctx context.Context, _ *consolev1.GetSessionRequest) (*consolev1.GetSessionResponse, error) {
	current, ok := session.FromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "sign in first")
	}
	principal := current.Principal()
	client, err := s.factory.For(principal)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "this session's identity is no longer the console's").WithCause(err)
	}
	identity, err := kube.WhoAmI(ctx, client)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "the API server did not say who this identity is").WithCause(err)
	}
	return &consolev1.GetSessionResponse{
		Mode:                   s.facts.Mode,
		Context:                principal.Context,
		KubernetesUsername:     identity.Username,
		KubernetesGroups:       identity.Groups,
		AccountId:              principal.AccountID,
		ReadOnly:               s.facts.ReadOnly,
		KubeconfigImpersonates: s.facts.Impersonates,
		InsecureTls:            s.facts.InsecureTLS,
	}, nil
}

// Logout ends the session and clears the cookie.
func (s *Session) Logout(ctx context.Context, _ *consolev1.LogoutRequest) (*consolev1.LogoutResponse, error) {
	info, _ := connect.CallInfoForServerContext(ctx)
	if token, ok := s.cookies.Token(info.RequestHeader().Values("Cookie")); ok {
		if current, err := s.store.Lookup(token, session.Background); err == nil {
			s.record(ctx, "sign-out", audit.Admitted, current.Principal(), "")
		}
		s.store.Revoke(token)
	}
	info.ResponseHeader().Add("Set-Cookie", s.cookies.Clear().String())
	return &consolev1.LogoutResponse{}, nil
}

func (s *Session) record(ctx context.Context, action, outcome string, principal kube.Principal, reason string) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Event{
		Action: action, Outcome: outcome,
		AccountID: principal.AccountID, Context: principal.Context, Reason: reason,
	})
}
