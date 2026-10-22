package agent

import (
	"context"
	"errors"
	"io"
	"regexp"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/session"
	"github.com/stokaro/ptah-console/internal/decision"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// Bounds a connection holds the console to.
const (
	// MaxDeadline is the longest a request may take, on the agent's clock.
	MaxDeadline = 30 * time.Second
	// MaxInFlight is how many requests one connection may have running.
	MaxInFlight = 64
	// MaxPerAccount is how many of them may be one account's.
	MaxPerAccount = 8
	// MaxClockSkew is how far the console's clock and the agent's may
	// disagree. A request is acted on until its deadline, counted from when
	// the console sent it, plus this; never later, however long it waited to
	// be read.
	MaxClockSkew = 15 * time.Second
)

const (
	maxRequestID  = 64
	replayWindow  = 10 * time.Minute
	replayEntries = 4096
)

var requestID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Dispatcher serves the console's requests. Each runs through the console's
// own services, the ones the console serves the cluster it runs in with,
// behind a session of the account it names, whose client impersonates that
// account's username.
type Dispatcher struct {
	factory *Factory
	ceiling []string
	now     func() time.Time
	store   *session.Store
	cookies session.Cookies
	slots   chan struct{}
	seen    *replays

	mu     sync.Mutex
	tokens map[string]string

	scope       consolev1connect.ScopeServiceClient
	schemas     consolev1connect.SchemaServiceClient
	migrations  consolev1connect.MigrationServiceClient
	decisions   consolev1connect.DecisionServiceClient
	watch       consolev1connect.WatchServiceClient
	diagnostics consolev1connect.DiagnosticsServiceClient
}

// NewDispatcher returns a dispatcher for the factory's connection. Decisions
// are audited to auditLog.
func NewDispatcher(factory *Factory, auditLog *audit.Logger, clock func() time.Time) *Dispatcher {
	if clock == nil {
		clock = time.Now
	}
	store := session.NewStore(session.Options{MaxConcurrency: MaxPerAccount, MaxSessions: maxAccounts})
	cookies := session.Cookies{}
	server := connect.NewServer(session.Interceptor(store, cookies))
	ceiling := slices.Clone(factory.config.Namespaces)
	scope := api.Scope{Candidates: ceiling, Ceiling: ceiling}
	consolev1connect.RegisterScopeServiceHandler(server, api.NewScopeService(factory, scope))
	consolev1connect.RegisterSchemaServiceHandler(server, api.NewSchemaService(factory, scope))
	consolev1connect.RegisterMigrationServiceHandler(server, api.NewMigrationService(factory, scope))
	consolev1connect.RegisterDecisionServiceHandler(server, api.NewDecisionService(factory, scope, decision.NewLedger(), auditLog))
	consolev1connect.RegisterWatchServiceHandler(server, api.NewWatchService(factory, scope, api.WatchOptions{StreamsPerSession: MaxPerAccount}))
	consolev1connect.RegisterDiagnosticsServiceHandler(server, api.NewDiagnosticsService(factory, scope, nil))
	client := connect.NewClient(connectinprocess.New(server))
	return &Dispatcher{
		factory: factory, ceiling: ceiling, now: clock, store: store, cookies: cookies,
		slots: make(chan struct{}, MaxInFlight), seen: newReplays(clock), tokens: make(map[string]string),
		scope:       consolev1connect.NewScopeServiceClient(client),
		schemas:     consolev1connect.NewSchemaServiceClient(client),
		migrations:  consolev1connect.NewMigrationServiceClient(client),
		decisions:   consolev1connect.NewDecisionServiceClient(client),
		watch:       consolev1connect.NewWatchServiceClient(client),
		diagnostics: consolev1connect.NewDiagnosticsServiceClient(client),
	}
}

// Revoke ends the sessions of accounts the allowlist no longer names, which
// ends their requests in flight.
func (d *Dispatcher) Revoke() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.store.RevokeWhere(func(principal kube.Principal) bool { return !d.factory.Allowed(principal.AccountID) })
	for account := range d.tokens {
		if !d.factory.Allowed(account) {
			delete(d.tokens, account)
		}
	}
}

// Serve answers one request through send: once, or for a watch once per
// event and once to end it. It returns an error only when send does.
func (d *Dispatcher) Serve(ctx context.Context, request *agentv1.Request, send func(*agentv1.Result) error) error {
	id := request.GetEnvelope().GetRequestId()
	if err := d.admit(request); err != nil {
		return send(failure(id, err))
	}
	select {
	case d.slots <- struct{}{}:
		defer func() { <-d.slots }()
	default:
		return send(failure(id, connect.Errorf(connect.CodeResourceExhausted, "the agent has %d requests in flight", MaxInFlight)))
	}
	account := request.GetEnvelope().GetAccountId()
	token, err := d.sessionOf(account)
	if err != nil {
		return send(failure(id, connect.NewError(connect.CodeUnavailable, "the agent could not act for the account").WithCause(err)))
	}
	ctx, cancel := context.WithDeadline(ctx, d.expiry(request.GetEnvelope()))
	defer cancel()
	ctx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Cookie", d.cookies.Name()+"="+token)
	info.RequestHeader().Set(session.ActionHeader, session.ActionValue)

	if watch := request.GetWatch(); watch != nil {
		return d.serveWatch(ctx, id, watch, send)
	}
	result, err := d.call(ctx, account, request)
	if err != nil {
		return send(failure(id, err))
	}
	result.RequestId, result.Last = id, true
	return send(result)
}

// expiry is when a request stops being acted on: its deadline from now, and
// never past its deadline from when the console sent it, with the clock skew
// allowed.
func (d *Dispatcher) expiry(envelope *agentv1.Envelope) time.Time {
	deadline := envelope.GetDeadline().AsDuration()
	fromNow, fromSent := d.now().Add(deadline), envelope.GetSentAt().AsTime().Add(deadline+MaxClockSkew)
	if fromSent.Before(fromNow) {
		return fromSent
	}
	return fromNow
}

// admit refuses a request before anything acts on it: no envelope, an
// identifier seen before or not one, a deadline past the bound, a request the
// console sent too long ago or from a clock too far ahead, an account
// the allowlist does not name, an operation the agent does not serve, or a
// namespace outside the ceiling.
func (d *Dispatcher) admit(request *agentv1.Request) error {
	envelope := request.GetEnvelope()
	if envelope == nil {
		return connect.NewError(connect.CodeInvalidArgument, "a request needs an envelope")
	}
	id := envelope.GetRequestId()
	if len(id) > maxRequestID || !requestID.MatchString(id) {
		return connect.NewError(connect.CodeInvalidArgument, "the request ID is not one")
	}
	if !d.seen.add(id) {
		return connect.Errorf(connect.CodeAlreadyExists, "request %s was already received", id)
	}
	deadline := envelope.GetDeadline()
	if deadline == nil || deadline.CheckValid() != nil || deadline.AsDuration() <= 0 || deadline.AsDuration() > MaxDeadline {
		return connect.Errorf(connect.CodeInvalidArgument, "a request needs a deadline of at most %s", MaxDeadline)
	}
	sent := envelope.GetSentAt()
	if sent == nil || sent.CheckValid() != nil {
		return connect.NewError(connect.CodeInvalidArgument, "a request needs the time the console sent it")
	}
	now := d.now()
	switch {
	case sent.AsTime().After(now.Add(MaxClockSkew)):
		return connect.Errorf(connect.CodeFailedPrecondition,
			"the console sent this request %s ahead of this cluster's clock, more than the %s the clocks may disagree",
			sent.AsTime().Sub(now).Round(time.Second), MaxClockSkew)
	case !now.Before(d.expiry(envelope)):
		return connect.Errorf(connect.CodeDeadlineExceeded,
			"the console sent this request %s ago, past its deadline; it is not acted on late",
			now.Sub(sent.AsTime()).Round(time.Second))
	}
	if !d.factory.Allowed(envelope.GetAccountId()) {
		return connect.Errorf(connect.CodePermissionDenied, "account %q is not on this agent's allowlist", envelope.GetAccountId())
	}
	namespaces, ok := namespacesOf(request)
	if !ok {
		return connect.NewError(connect.CodeUnimplemented, "the agent serves no such request")
	}
	for _, namespace := range namespaces {
		if !slices.Contains(d.ceiling, namespace) {
			return connect.Errorf(connect.CodePermissionDenied, "namespace %q is outside what this agent serves", namespace)
		}
	}
	return nil
}

// namespacesOf is what a request names, and whether it is one the agent
// serves.
func namespacesOf(request *agentv1.Request) ([]string, bool) {
	switch action := request.GetAction().(type) {
	case *agentv1.Request_WhoAmI, *agentv1.Request_ListNamespaces:
		return nil, true
	case *agentv1.Request_ListSchemas:
		return action.ListSchemas.GetNamespaces(), true
	case *agentv1.Request_GetSchema:
		return []string{action.GetSchema.GetNamespace()}, true
	case *agentv1.Request_GetSchemaPlan:
		return []string{action.GetSchemaPlan.GetNamespace()}, true
	case *agentv1.Request_ListMigrations:
		return action.ListMigrations.GetNamespaces(), true
	case *agentv1.Request_GetMigration:
		return []string{action.GetMigration.GetNamespace()}, true
	case *agentv1.Request_GetMigrationPlan:
		return []string{action.GetMigrationPlan.GetNamespace()}, true
	case *agentv1.Request_ListDecisions:
		return action.ListDecisions.GetNamespaces(), true
	case *agentv1.Request_GetDecision:
		return []string{action.GetDecision.GetNamespace()}, true
	case *agentv1.Request_ApproveSchemaPlan:
		return []string{action.ApproveSchemaPlan.GetNamespace()}, true
	case *agentv1.Request_ApproveMigrationPlan:
		return []string{action.ApproveMigrationPlan.GetNamespace()}, true
	case *agentv1.Request_AcknowledgeMigrationRun:
		return []string{action.AcknowledgeMigrationRun.GetNamespace()}, true
	case *agentv1.Request_SetSuspended:
		return []string{action.SetSuspended.GetNamespace()}, true
	case *agentv1.Request_GetDiagnostics:
		return []string{action.GetDiagnostics.GetNamespace()}, true
	case *agentv1.Request_Watch:
		return action.Watch.GetNamespaces(), true
	default:
		return nil, false
	}
}

// call runs one request that has one answer.
func (d *Dispatcher) call(ctx context.Context, account string, request *agentv1.Request) (*agentv1.Result, error) {
	switch action := request.GetAction().(type) {
	case *agentv1.Request_WhoAmI:
		return d.whoAmI(ctx, account)
	case *agentv1.Request_ListNamespaces:
		response, err := d.scope.ListNamespaces(ctx, action.ListNamespaces)
		return &agentv1.Result{Outcome: &agentv1.Result_ListNamespaces{ListNamespaces: response}}, err
	case *agentv1.Request_ListSchemas:
		response, err := d.schemas.ListSchemas(ctx, action.ListSchemas)
		return &agentv1.Result{Outcome: &agentv1.Result_ListSchemas{ListSchemas: response}}, err
	case *agentv1.Request_GetSchema:
		response, err := d.schemas.GetSchema(ctx, action.GetSchema)
		return &agentv1.Result{Outcome: &agentv1.Result_GetSchema{GetSchema: response}}, err
	case *agentv1.Request_GetSchemaPlan:
		response, err := d.schemas.GetSchemaPlan(ctx, action.GetSchemaPlan)
		return &agentv1.Result{Outcome: &agentv1.Result_GetSchemaPlan{GetSchemaPlan: response}}, err
	case *agentv1.Request_ListMigrations:
		response, err := d.migrations.ListMigrations(ctx, action.ListMigrations)
		return &agentv1.Result{Outcome: &agentv1.Result_ListMigrations{ListMigrations: response}}, err
	case *agentv1.Request_GetMigration:
		response, err := d.migrations.GetMigration(ctx, action.GetMigration)
		return &agentv1.Result{Outcome: &agentv1.Result_GetMigration{GetMigration: response}}, err
	case *agentv1.Request_GetMigrationPlan:
		response, err := d.migrations.GetMigrationPlan(ctx, action.GetMigrationPlan)
		return &agentv1.Result{Outcome: &agentv1.Result_GetMigrationPlan{GetMigrationPlan: response}}, err
	case *agentv1.Request_ListDecisions:
		response, err := d.decisions.ListDecisions(ctx, action.ListDecisions)
		return &agentv1.Result{Outcome: &agentv1.Result_ListDecisions{ListDecisions: response}}, err
	case *agentv1.Request_GetDecision:
		response, err := d.decisions.GetDecision(ctx, action.GetDecision)
		return &agentv1.Result{Outcome: &agentv1.Result_GetDecision{GetDecision: response}}, err
	case *agentv1.Request_ApproveSchemaPlan:
		response, err := d.decisions.ApproveSchemaPlan(ctx, action.ApproveSchemaPlan)
		return &agentv1.Result{Outcome: &agentv1.Result_ApproveSchemaPlan{ApproveSchemaPlan: response}}, err
	case *agentv1.Request_ApproveMigrationPlan:
		response, err := d.decisions.ApproveMigrationPlan(ctx, action.ApproveMigrationPlan)
		return &agentv1.Result{Outcome: &agentv1.Result_ApproveMigrationPlan{ApproveMigrationPlan: response}}, err
	case *agentv1.Request_AcknowledgeMigrationRun:
		response, err := d.decisions.AcknowledgeMigrationRun(ctx, action.AcknowledgeMigrationRun)
		return &agentv1.Result{Outcome: &agentv1.Result_AcknowledgeMigrationRun{AcknowledgeMigrationRun: response}}, err
	case *agentv1.Request_SetSuspended:
		response, err := d.decisions.SetSuspended(ctx, action.SetSuspended)
		return &agentv1.Result{Outcome: &agentv1.Result_SetSuspended{SetSuspended: response}}, err
	case *agentv1.Request_GetDiagnostics:
		response, err := d.diagnostics.GetDiagnostics(ctx, action.GetDiagnostics)
		return &agentv1.Result{Outcome: &agentv1.Result_GetDiagnostics{GetDiagnostics: response}}, err
	default:
		return nil, connect.NewError(connect.CodeUnimplemented, "the agent serves no such request")
	}
}

// whoAmI asks the API server whom the account's client acts as.
func (d *Dispatcher) whoAmI(ctx context.Context, account string) (*agentv1.Result, error) {
	client, err := d.factory.For(d.factory.Principal(account))
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, "the agent does not act for the account").WithCause(err)
	}
	identity, err := kube.WhoAmI(ctx, client)
	switch {
	case apierrors.IsForbidden(err):
		return nil, connect.NewError(connect.CodePermissionDenied, "the agent may not act as the account's username").WithCause(err)
	case err != nil:
		return nil, connect.NewError(connect.CodeUnavailable, "the API server did not say whom the account acts as").WithCause(err)
	}
	return &agentv1.Result{Outcome: &agentv1.Result_WhoAmI{
		WhoAmI: &agentv1.WhoAmIResponse{Username: identity.Username, Groups: identity.Groups},
	}}, nil
}

// serveWatch relays one watch window, then ends it. A window that runs to
// its deadline ends cleanly.
func (d *Dispatcher) serveWatch(ctx context.Context, id string, watch *consolev1.SubscribeRequest, send func(*agentv1.Result) error) error {
	stream, err := d.watch.Subscribe(ctx, watch)
	if err != nil {
		return send(failure(id, err))
	}
	defer func() { _ = stream.Close() }()
	for {
		event, err := stream.Receive()
		switch {
		case err == nil:
		case errors.Is(err, io.EOF) || ctx.Err() != nil:
			return send(&agentv1.Result{RequestId: id, Last: true})
		default:
			return send(failure(id, err))
		}
		if err := send(&agentv1.Result{RequestId: id, Outcome: &agentv1.Result_Watch{Watch: event}}); err != nil {
			return err
		}
	}
}

// sessionOf returns the token of the account's session, starting one when
// it has none or the one it had ended.
func (d *Dispatcher) sessionOf(account string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if token, ok := d.tokens[account]; ok {
		if _, err := d.store.Lookup(token, session.Background); err == nil {
			return token, nil
		}
	}
	token, _, err := d.store.Create(d.factory.Principal(account))
	if err != nil {
		return "", err
	}
	d.tokens[account] = token
	return token, nil
}

// failure is the last result of a request that ended with err.
func failure(id string, err error) *agentv1.Result {
	message := err.Error()
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		message = connectErr.Message()
	}
	return &agentv1.Result{RequestId: id, Last: true, Outcome: &agentv1.Result_Failure{
		Failure: &agentv1.Failure{Code: connect.CodeOf(err).String(), Message: message},
	}}
}

// replays remembers the request IDs of the last replayWindow, at most
// replayEntries of them.
type replays struct {
	clock func() time.Time

	mu    sync.Mutex
	at    map[string]time.Time
	order []string
}

func newReplays(clock func() time.Time) *replays {
	if clock == nil {
		clock = time.Now
	}
	return &replays{clock: clock, at: make(map[string]time.Time)}
}

// add records id, and reports false when it was already recorded.
func (r *replays) add(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock()
	for len(r.order) > 0 && (len(r.order) >= replayEntries || now.Sub(r.at[r.order[0]]) > replayWindow) {
		delete(r.at, r.order[0])
		r.order = r.order[1:]
	}
	if _, seen := r.at[id]; seen {
		return false
	}
	r.at[id] = now
	r.order = append(r.order, id)
	return true
}
