package watch_test

import (
	"context"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{OperatorCRDs: true}, &plane))
}

type factory struct{ client *kube.Client }

func (f factory) For(kube.Principal) (*kube.Client, error) { return f.client, nil }

// arrange creates a namespace with a schema and lets the user watch every
// kind the stream follows there.
func arrange(t *testing.T, user string) string {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	namespace := user + "-shop"
	c.Assert(plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &operatorv1alpha1.PtahSchema{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "storefront"},
		Spec: operatorv1alpha1.PtahSchemaSpec{
			Target: operatorv1alpha1.DatabaseTargetSpec{
				Engine: operatorv1alpha1.DatabaseEnginePostgreSQL, CoordinationKey: "envtest/" + namespace,
				URLFrom: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "database"}, Key: "url"},
			},
			Desired: operatorv1alpha1.OCIArtifactSourceSpec{
				OCIRef: "oci://registry.example/shop/schema:1.0.0",
				VerificationPolicyFrom: corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "ptah-verification-policy"}, Key: "policy.yaml",
				},
			},
			Policy:   operatorv1alpha1.ReconciliationPolicy{Apply: operatorv1alpha1.ApplyPolicyOnApproval, LockTimeout: metav1.Duration{Duration: 30 * time.Second}},
			Interval: metav1.Duration{Duration: 10 * time.Minute},
			Execution: operatorv1alpha1.ExecutionSpec{
				FailureRetryInterval: metav1.Duration{Duration: 30 * time.Second},
				ConnectTimeout:       metav1.Duration{Duration: 10 * time.Second},
			},
		},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "watcher"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"operator.ptah.run"},
			Resources: []string{"ptahschemas", "ptahmigrations", "ptahschemaapprovals", "ptahmigrationapprovals", "ptahmigrationrunacknowledgments"},
			Verbs:     []string{"get", "list", "watch"},
		}},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, binding(namespace, user)), qt.IsNil)
	return namespace
}

func binding(namespace, user string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "watcher"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "watcher"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: user}},
	}
}

// watching serves WatchService as the user with one-second windows and
// returns the client, a signed-in context, the session store and the token.
func watching(t *testing.T, user string) (consolev1connect.WatchServiceClient, context.Context, *session.Store, string) {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User(user)
	c.Assert(err, qt.IsNil)
	built, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	store := session.NewStore(session.Options{})
	token, _, err := store.Create(kube.Principal{Mode: kube.ModeLocal, Context: "envtest", Generation: 1})
	c.Assert(err, qt.IsNil)
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterWatchServiceHandler(server, api.NewWatchService(factory{built}, api.Scope{},
		api.WatchOptions{Window: time.Second, Heartbeat: 100 * time.Millisecond}))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	ctx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	return consolev1connect.NewWatchServiceClient(connect.NewClient(connectinprocess.New(server))), ctx, store, token
}

// until receives events until one matches, and returns those received; the
// stream's own end comes back as the error.
func until(stream consolev1connect.WatchServiceSubscribeClientStream, match func(*consolev1.SubscribeResponse) bool) ([]*consolev1.SubscribeResponse, error) {
	var seen []*consolev1.SubscribeResponse
	for {
		event, err := stream.Receive()
		if err != nil {
			return seen, err
		}
		seen = append(seen, event)
		if match(event) {
			return seen, nil
		}
	}
}

func isHeartbeat(event *consolev1.SubscribeResponse) bool { return event.GetHeartbeat() != nil }

func never(*consolev1.SubscribeResponse) bool { return false }

func lost(events []*consolev1.SubscribeResponse) []consolev1.WatchedKind {
	var kinds []consolev1.WatchedKind
	for _, event := range events {
		if event.GetLost() != nil {
			kinds = append(kinds, event.GetLost().GetKind())
		}
	}
	return kinds
}

// A change made after the stream says it is live reaches it, across
// windows.
func TestAChangeReachesTheStream(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := arrange(t, "follower")
	watch, ctx, _, _ := watching(t, "follower")
	stream, err := watch.Subscribe(ctx, &consolev1.SubscribeRequest{Namespaces: []string{namespace}})
	c.Assert(err, qt.IsNil)
	_, err = until(stream, isHeartbeat)
	c.Assert(err, qt.IsNil)
	// Past the first window, so the change lands in a window opened again.
	time.Sleep(1500 * time.Millisecond)

	c.Assert(plane.Admin.Patch(t.Context(), &operatorv1alpha1.PtahSchema{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "storefront"}},
		client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"touched":"yes"}}}`))), qt.IsNil)
	events, err := until(stream, func(event *consolev1.SubscribeResponse) bool { return event.GetInvalidate() != nil })

	c.Assert(err, qt.IsNil)
	invalidate := events[len(events)-1].GetInvalidate()
	c.Assert(invalidate.GetKind(), qt.Equals, consolev1.WatchedKind_WATCHED_KIND_SCHEMA)
	c.Assert(invalidate.GetNamespace(), qt.Equals, namespace)
	c.Assert(invalidate.GetName(), qt.Equals, "storefront")
	c.Assert(invalidate.GetResourceVersion(), qt.Not(qt.Equals), "")
}

// Once the identity may no longer watch, the next window is refused, each
// kind is reported lost, and the stream ends.
func TestTheStreamEndsWhenTheGrantDoes(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := arrange(t, "revoked")
	watch, ctx, _, _ := watching(t, "revoked")
	stream, err := watch.Subscribe(ctx, &consolev1.SubscribeRequest{Namespaces: []string{namespace}})
	c.Assert(err, qt.IsNil)
	_, err = until(stream, isHeartbeat)
	c.Assert(err, qt.IsNil)

	c.Assert(plane.Admin.Delete(t.Context(), binding(namespace, "revoked")), qt.IsNil)
	events, err := until(stream, never)

	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodePermissionDenied)
	c.Assert(lost(events), qt.ContentEquals, []consolev1.WatchedKind{
		consolev1.WatchedKind_WATCHED_KIND_SCHEMA,
		consolev1.WatchedKind_WATCHED_KIND_MIGRATION,
		consolev1.WatchedKind_WATCHED_KIND_SCHEMA_APPROVAL,
		consolev1.WatchedKind_WATCHED_KIND_MIGRATION_APPROVAL,
		consolev1.WatchedKind_WATCHED_KIND_RUN_ACKNOWLEDGMENT,
	})
}

// Signing out ends the stream at once, not at the end of a window.
func TestSigningOutEndsTheStream(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := arrange(t, "leaving")
	watch, ctx, store, token := watching(t, "leaving")
	stream, err := watch.Subscribe(ctx, &consolev1.SubscribeRequest{Namespaces: []string{namespace}})
	c.Assert(err, qt.IsNil)
	_, err = until(stream, isHeartbeat)
	c.Assert(err, qt.IsNil)

	store.Revoke(token)
	_, err = until(stream, never)

	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeUnauthenticated)
}

// A session holds a bounded number of streams.
func TestASessionHoldsBoundedStreams(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := arrange(t, "greedy")
	watch, ctx, _, _ := watching(t, "greedy")
	request := &consolev1.SubscribeRequest{Namespaces: []string{namespace}}
	first, err := watch.Subscribe(ctx, request)
	c.Assert(err, qt.IsNil)
	_, err = until(first, isHeartbeat)
	c.Assert(err, qt.IsNil)
	second, err := watch.Subscribe(ctx, request)
	c.Assert(err, qt.IsNil)
	_, err = until(second, isHeartbeat)
	c.Assert(err, qt.IsNil)

	third, err := watch.Subscribe(ctx, request)
	c.Assert(err, qt.IsNil)
	_, err = until(third, never)

	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeResourceExhausted)
}
