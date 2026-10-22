// Package route sends a person's request to the cluster it names. A request
// that names no connection, or the cluster the console runs in, is served by
// the console itself as before; one that names an external connection goes
// down that connection's agent channel, as the same person, and its answer
// comes back as if the console had served it.
//
// The connection is named by its immutable UID, never by a name: two
// clusters with the same names inside them cannot be confused, and a
// connection the person was not granted reads the same as one that does not
// exist.
package route

import (
	"context"
	"errors"
	"os"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/agent"
	"github.com/stokaro/ptah-console/internal/agentlink"
	"github.com/stokaro/ptah-console/internal/auth/session"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/registry"
)

// Header names the connection a request is for, by connection UID.
const Header = "Ptah-Connection"

// Router routes requests by connection.
type Router struct {
	hub          *agentlink.Hub
	housekeeping client.Reader
	users        kube.ClientFactory
	namespace    string
	thisCluster  string
	instance     string
}

// New returns a router for the registry in namespace. thisCluster is the
// connection UID of the cluster the console runs in.
func New(hub *agentlink.Hub, housekeeping client.Reader, users kube.ClientFactory, namespace, thisCluster string) *Router {
	instance, _ := os.Hostname()
	return &Router{hub: hub, housekeeping: housekeeping, users: users, namespace: namespace, thisCluster: thisCluster, instance: instance}
}

// Interceptor routes each call. It runs after the session interceptor, so a
// call it routes is already a signed-in person's.
func (r *Router) Interceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			info, _ := connect.CallInfoForServerContext(ctx)
			connection := info.RequestHeader().Get(Header)
			procedure, routed := procedures[spec.Procedure]
			if connection == "" || connection == r.thisCluster || !routed {
				// The console's own procedures, such as the session, are the
				// console's whichever cluster a page shows.
				return next(ctx, spec, stream)
			}
			current, ok := session.FromContext(ctx)
			if !ok {
				return connect.NewError(connect.CodeUnauthenticated, "sign in first")
			}
			principal := current.Principal()
			if err := r.check(ctx, principal, connection); err != nil {
				return err
			}
			if procedure.watch {
				return r.watch(ctx, principal, connection, stream)
			}
			return r.unary(ctx, principal, connection, procedure, stream)
		}
	}
}

// check refuses a connection the person may not see, and one that is not an
// agent's.
func (r *Router) check(ctx context.Context, principal kube.Principal, connection string) error {
	user, err := r.users.For(principal)
	if err != nil {
		return connect.NewError(connect.CodeFailedPrecondition, "this session's identity is no longer the console's").WithCause(err)
	}
	entry, err := registry.Find(ctx, r.housekeeping, user, r.namespace, connection)
	if err != nil {
		return connect.NewError(connect.CodeNotFound, "no connection with that UID is visible to you").WithCause(err)
	}
	if entry.Spec.Profile != consolev1alpha1.ProfileAgent {
		return connect.Errorf(connect.CodeFailedPrecondition, "connection %s is not reached through an agent", connection)
	}
	fence, err := agentlink.FenceOf(ctx, r.housekeeping, r.namespace, connection)
	if err != nil {
		return connect.NewError(connect.CodeUnavailable, "the console could not read its revocations").WithCause(err)
	}
	switch {
	case fence.Removed:
		return connect.Errorf(connect.CodeFailedPrecondition, "connection %s was removed", connection)
	case entry.Status.Channel != nil && fence.Fences(entry.Status.Channel.Generation):
		return connect.Errorf(connect.CodeFailedPrecondition, "connection %s is quarantined", connection)
	}
	disabled, err := r.disabled(ctx, connection)
	if err != nil {
		return connect.NewError(connect.CodeUnavailable, "the console could not read the connection's preferences").WithCause(err)
	}
	if disabled {
		return connect.Errorf(connect.CodeFailedPrecondition, "connection %s is disabled", connection)
	}
	return nil
}

// disabled reports whether the connection's preferences turn it off.
func (r *Router) disabled(ctx context.Context, connection string) (bool, error) {
	preferences := &consolev1alpha1.ConsoleClusterPreferencesList{}
	if err := r.housekeeping.List(ctx, preferences, client.InNamespace(r.namespace)); err != nil {
		return false, err
	}
	for _, item := range preferences.Items {
		if item.Spec.ClusterRef.ConnectionUID == connection && item.Spec.Enabled != nil && !*item.Spec.Enabled {
			return true, nil
		}
	}
	return false, nil
}

// envelope is who a routed request is for, and how long it may take: the
// caller's deadline, bounded by the agent's.
func (r *Router) envelope(ctx context.Context, principal kube.Principal) *agentv1.Envelope {
	deadline := agent.MaxDeadline
	if at, ok := ctx.Deadline(); ok && time.Until(at) < deadline {
		deadline = time.Until(at)
	}
	return &agentv1.Envelope{
		ConsoleInstance: r.instance, SessionGeneration: principal.Generation, AccountId: principal.AccountID,
		Deadline: durationpb.New(deadline), SentAt: timestamppb.Now(),
	}
}

// unary sends one request down the channel and answers with its result.
func (r *Router) unary(ctx context.Context, principal kube.Principal, connection string, procedure procedure, stream connect.ServerStream) error {
	request := procedure.request()
	if err := stream.Receive(request); err != nil {
		return err
	}
	routed := procedure.wrap(request)
	routed.Envelope = r.envelope(ctx, principal)
	pending, err := r.hub.Send(ctx, connection, routed)
	if err != nil {
		return notSent(err)
	}
	var last *agentv1.Result
	for result := range pending.Results {
		if result.GetLast() {
			last = result
		}
	}
	switch {
	case last == nil && procedure.decision:
		return connect.NewError(connect.CodeUnknown,
			"the channel to the cluster ended before its answer arrived: the decision may or may not have been made; read it back before sending it again")
	case last == nil:
		return connect.NewError(connect.CodeUnavailable, "the channel to the cluster ended before its answer arrived")
	case last.GetFailure() != nil:
		return failure(last.GetFailure())
	}
	response := procedure.unwrap(last)
	if response == nil {
		return connect.NewError(connect.CodeInternal, "the agent answered with another kind of result")
	}
	return stream.Send(response)
}

// watch relays the agent's watch windows, one after another, until the
// caller goes away or one fails. Between windows a change could pass
// unreported, so each window after the first starts by saying that anything
// it watches may have changed.
func (r *Router) watch(ctx context.Context, principal kube.Principal, connection string, stream connect.ServerStream) error {
	request := &consolev1.SubscribeRequest{}
	if err := stream.Receive(request); err != nil {
		return err
	}
	for window := 0; ; window++ {
		if window > 0 {
			if err := invalidateAll(stream, request.GetNamespaces()); err != nil {
				return err
			}
		}
		routed := &agentv1.Request{
			Envelope: r.envelope(ctx, principal),
			Action:   &agentv1.Request_Watch{Watch: request},
		}
		routed.Envelope.Deadline = durationpb.New(agent.MaxDeadline)
		pending, err := r.hub.Send(ctx, connection, routed)
		if err != nil {
			return notSent(err)
		}
		var last *agentv1.Result
		for result := range pending.Results {
			if result.GetLast() {
				last = result
				continue
			}
			if event := result.GetWatch(); event != nil {
				if err := stream.Send(event); err != nil {
					return err
				}
			}
		}
		switch {
		case ctx.Err() != nil:
			return nil
		case last == nil:
			return connect.NewError(connect.CodeUnavailable, "the channel to the cluster ended")
		case last.GetFailure() != nil:
			return failure(last.GetFailure())
		}
	}
}

// watchedKinds are the kinds a watch reports.
var watchedKinds = []consolev1.WatchedKind{
	consolev1.WatchedKind_WATCHED_KIND_SCHEMA,
	consolev1.WatchedKind_WATCHED_KIND_MIGRATION,
	consolev1.WatchedKind_WATCHED_KIND_SCHEMA_APPROVAL,
	consolev1.WatchedKind_WATCHED_KIND_MIGRATION_APPROVAL,
	consolev1.WatchedKind_WATCHED_KIND_RUN_ACKNOWLEDGMENT,
}

func invalidateAll(stream connect.ServerStream, namespaces []string) error {
	for _, namespace := range namespaces {
		for _, kind := range watchedKinds {
			event := &consolev1.SubscribeResponse{Event: &consolev1.SubscribeResponse_Invalidate{
				Invalidate: &consolev1.Invalidate{Kind: kind, Namespace: namespace},
			}}
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
	return nil
}

// notSent is a request the hub could not send.
func notSent(err error) error {
	if errors.Is(err, agentlink.ErrNotConnected) {
		return connect.NewError(connect.CodeUnavailable, "the cluster's agent is not connected").WithCause(err)
	}
	return connect.NewError(connect.CodeUnavailable, "the request could not be sent to the cluster's agent").WithCause(err)
}

// failure is the agent's failure, as the console's own service would have
// returned it.
func failure(f *agentv1.Failure) error {
	var code connect.Code
	if err := code.UnmarshalText([]byte(f.GetCode())); err != nil {
		code = connect.CodeUnknown
	}
	return connect.NewError(code, f.GetMessage())
}

// procedure is how one of the console's procedures crosses the channel.
type procedure struct {
	request  func() proto.Message
	wrap     func(proto.Message) *agentv1.Request
	unwrap   func(*agentv1.Result) proto.Message
	decision bool
	watch    bool
}

// Routed reports whether a procedure follows the connection a request names.
func Routed(procedure string) bool {
	_, ok := procedures[procedure]
	return ok
}
