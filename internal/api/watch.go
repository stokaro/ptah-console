package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"google.golang.org/protobuf/types/known/timestamppb"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// Watch defaults.
const (
	// DefaultWatchWindow bounds one watch. The next window is a new request,
	// which the API server authenticates and authorizes again.
	DefaultWatchWindow = 30 * time.Second
	// DefaultHeartbeat is how often a live stream says so.
	DefaultHeartbeat = 15 * time.Second
	// DefaultStreamsPerSession bounds the streams one session holds open.
	DefaultStreamsPerSession = 2
)

// WatchOptions tune the watch service; zero values take the defaults.
type WatchOptions struct {
	Window            time.Duration
	Heartbeat         time.Duration
	StreamsPerSession int
}

// WatchService serves WatchService.
type WatchService struct {
	consolev1connect.UnimplementedWatchServiceHandler

	factory kube.ClientFactory
	scope   Scope
	options WatchOptions

	mu      sync.Mutex
	streams map[*session.Session]int
}

// NewWatchService returns the WatchService.
func NewWatchService(factory kube.ClientFactory, scope Scope, options WatchOptions) *WatchService {
	if options.Window <= 0 {
		options.Window = DefaultWatchWindow
	}
	if options.Heartbeat <= 0 {
		options.Heartbeat = DefaultHeartbeat
	}
	if options.StreamsPerSession <= 0 {
		options.StreamsPerSession = DefaultStreamsPerSession
	}
	return &WatchService{factory: factory, scope: scope, options: options, streams: make(map[*session.Session]int)}
}

// watchedKinds are the kinds a page shows. Plans change only with their
// owner's status, so the owner's change covers them.
var watchedKinds = []struct {
	kind consolev1.WatchedKind
	list func() client.ObjectList
}{
	{consolev1.WatchedKind_WATCHED_KIND_SCHEMA, func() client.ObjectList { return &operatorv1alpha1.PtahSchemaList{} }},
	{consolev1.WatchedKind_WATCHED_KIND_MIGRATION, func() client.ObjectList { return &operatorv1alpha1.PtahMigrationList{} }},
	{consolev1.WatchedKind_WATCHED_KIND_SCHEMA_APPROVAL, func() client.ObjectList { return &operatorv1alpha1.PtahSchemaApprovalList{} }},
	{consolev1.WatchedKind_WATCHED_KIND_MIGRATION_APPROVAL, func() client.ObjectList { return &operatorv1alpha1.PtahMigrationApprovalList{} }},
	{consolev1.WatchedKind_WATCHED_KIND_RUN_ACKNOWLEDGMENT, func() client.ObjectList { return &operatorv1alpha1.PtahMigrationRunAcknowledgmentList{} }},
}

// Subscribe streams changes to the namespaces as the session's identity.
func (w *WatchService) Subscribe(ctx context.Context, request *consolev1.SubscribeRequest, stream consolev1connect.WatchServiceSubscribeServerStream) error {
	namespaces := request.GetNamespaces()
	if len(namespaces) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, "name at least one namespace")
	}
	if err := w.scope.check(namespaces); err != nil {
		return err
	}
	current, ok := session.FromContext(ctx)
	if !ok {
		return connect.NewError(connect.CodeUnauthenticated, "sign in first")
	}
	release, ok := w.admit(current)
	if !ok {
		return connect.NewError(connect.CodeResourceExhausted, "this session has as many streams open as it may")
	}
	defer release()
	c, _, err := actor(ctx, w.factory)
	if err != nil {
		return err
	}
	watcher, ok := c.Client.(client.WithWatch)
	if !ok {
		return connect.NewError(connect.CodeInternal, "the client cannot watch")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := make(chan *consolev1.SubscribeResponse)
	opened := make(chan struct{})
	stopped := make(chan error, len(namespaces)*len(watchedKinds))
	followers := 0
	for _, namespace := range namespaces {
		for _, kind := range watchedKinds {
			followers++
			f := follower{watcher: watcher, namespace: namespace, kind: kind.kind, list: kind.list, window: w.options.Window, events: events, opened: opened}
			go func() { stopped <- f.run(ctx) }()
		}
	}
	return w.relay(ctx, current, stream, followers, events, opened, stopped)
}

// relay sends what the followers report, and a heartbeat once every watch is
// open and at each tick after. It ends when the session or the caller does,
// or when nothing is left that the identity may watch.
func (w *WatchService) relay(ctx context.Context, current *session.Session, stream consolev1connect.WatchServiceSubscribeServerStream,
	followers int, events <-chan *consolev1.SubscribeResponse, opened <-chan struct{}, stopped <-chan error,
) error {
	ticker := time.NewTicker(w.options.Heartbeat)
	defer ticker.Stop()
	waiting, active := followers, followers
	var last error
	for {
		var heartbeat bool
		select {
		case <-ctx.Done():
			if current.Context().Err() != nil {
				return connect.NewError(connect.CodeUnauthenticated, "the session ended")
			}
			return nil
		case event := <-events:
			if err := stream.Send(event); err != nil {
				return err
			}
		case <-opened:
			waiting--
			heartbeat = waiting == 0
		case err := <-stopped:
			active--
			last = err
			if active == 0 {
				return connect.NewError(connect.CodePermissionDenied, "this identity may no longer watch anything here").WithCause(last)
			}
		case <-ticker.C:
			heartbeat = waiting <= 0
		}
		if heartbeat {
			beat := &consolev1.SubscribeResponse{Event: &consolev1.SubscribeResponse_Heartbeat{Heartbeat: &consolev1.Heartbeat{ObservedAt: timestamppb.Now()}}}
			if err := stream.Send(beat); err != nil {
				return err
			}
		}
	}
}

// admit counts the session's streams.
func (w *WatchService) admit(current *session.Session) (release func(), ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.streams[current] >= w.options.StreamsPerSession {
		return nil, false
	}
	w.streams[current]++
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.streams[current]--
		if w.streams[current] == 0 {
			delete(w.streams, current)
		}
	}, true
}

// follower watches one kind in one namespace, window after window.
type follower struct {
	watcher   client.WithWatch
	namespace string
	kind      consolev1.WatchedKind
	list      func() client.ObjectList
	window    time.Duration
	events    chan<- *consolev1.SubscribeResponse
	opened    chan<- struct{}
}

// run follows until the context ends, or until the identity may not watch
// the kind here, which it returns.
func (f follower) run(ctx context.Context) error {
	// The relay waits for every follower to open its first watch, or to
	// stop for good, before it says the stream is live.
	announce := sync.OnceFunc(func() {
		select {
		case f.opened <- struct{}{}:
		case <-ctx.Done():
		}
	})
	defer announce()
	version, stale := "", false
	for ctx.Err() == nil {
		listed := func(version string) {
			if stale {
				// Whatever happened while the watch was down, the page
				// reads again.
				f.invalidate(ctx, "", version)
			}
		}
		next, err := f.follow(ctx, version, listed, announce)
		switch {
		case err == nil:
			version, stale = next, next == ""
		case ctx.Err() != nil:
			return ctx.Err()
		case permanent(err):
			f.lose(ctx, refusal(err), false)
			return err
		default:
			// The page may now be behind; the next open window says so.
			f.lose(ctx, "The watch stopped and is being opened again.", true)
			version, stale = "", true
			if !sleep(ctx, f.window) {
				return ctx.Err()
			}
		}
	}
	return ctx.Err()
}

// follow resumes from version, or starts from a fresh list when there is
// none, and runs one window. listed hears the version a fresh list read.
func (f follower) follow(ctx context.Context, version string, listed func(string), opened func()) (string, error) {
	if version != "" {
		return f.watchOnce(ctx, version, opened)
	}
	list := f.list()
	if err := f.watcher.List(ctx, list, client.InNamespace(f.namespace), client.Limit(1)); err != nil {
		return "", err
	}
	listed(list.GetResourceVersion())
	return f.watchOnce(ctx, list.GetResourceVersion(), opened)
}

// watchOnce runs one watch window from version and returns the version to
// resume from: empty when the watch has to start over.
func (f follower) watchOnce(ctx context.Context, version string, opened func()) (string, error) {
	// The window ends with this context: the request is cancelled and the
	// next window is a new one.
	windowCtx, cancel := context.WithTimeout(ctx, f.window)
	defer cancel()
	watching, err := f.watcher.Watch(windowCtx, f.list(), &client.ListOptions{
		Namespace: f.namespace,
		Raw:       &metav1.ListOptions{ResourceVersion: version, AllowWatchBookmarks: true},
	})
	if err != nil {
		return "", err
	}
	defer watching.Stop()
	opened()
	return f.drain(windowCtx, watching, version)
}

// drain reports each change until the window closes.
func (f follower) drain(ctx context.Context, watching watch.Interface, version string) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return version, nil
		case event, ok := <-watching.ResultChan():
			if !ok {
				return version, nil
			}
			if event.Type == watch.Error {
				status := apierrors.FromObject(event.Object)
				if apierrors.IsResourceExpired(status) || apierrors.IsGone(status) {
					// Too far behind to resume: start over from a list.
					return "", nil
				}
				return "", status
			}
			object, err := meta.Accessor(event.Object)
			if err != nil {
				continue
			}
			version = object.GetResourceVersion()
			if event.Type != watch.Bookmark {
				f.invalidate(ctx, object.GetName(), version)
			}
		}
	}
}

func (f follower) invalidate(ctx context.Context, name, version string) {
	f.send(ctx, &consolev1.SubscribeResponse{Event: &consolev1.SubscribeResponse_Invalidate{Invalidate: &consolev1.Invalidate{
		Kind: f.kind, Namespace: f.namespace, Name: name, ResourceVersion: version,
	}}})
}

func (f follower) lose(ctx context.Context, reason string, reconnecting bool) {
	f.send(ctx, &consolev1.SubscribeResponse{Event: &consolev1.SubscribeResponse_Lost{Lost: &consolev1.Lost{
		Kind: f.kind, Namespace: f.namespace, Reason: reason, Reconnecting: reconnecting,
	}}})
}

// send delivers an event unless the stream ended first.
func (f follower) send(ctx context.Context, event *consolev1.SubscribeResponse) {
	select {
	case f.events <- event:
	case <-ctx.Done():
	}
}

// permanent says an error will not go away by trying again: the identity
// may not watch the kind, or the kind is not served.
func permanent(err error) bool {
	return apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) || apierrors.IsNotFound(err) || meta.IsNoMatchError(err)
}

func refusal(err error) string {
	var status apierrors.APIStatus
	switch {
	case errors.As(err, &status) && status.Status().Code == http.StatusForbidden:
		return status.Status().Message
	case apierrors.IsUnauthorized(err):
		return "The API server no longer accepts this identity's credentials."
	default:
		return "The operator's API for this kind is not served here."
	}
}

// sleep waits for d, or reports that the context ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
