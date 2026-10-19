package resources_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

var principal = kube.Principal{Mode: kube.ModeLocal, Context: "envtest", Generation: 1}

// recording is a client that remembers every kind it was asked for, so a
// test can hold the console to the kinds it is allowed to read.
type recording struct {
	client.Client
	mu    sync.Mutex
	kinds map[string]bool
}

func (r *recording) note(object any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds[fmt.Sprintf("%T", object)] = true
}

func (r *recording) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	r.note(object)
	return r.Client.Get(ctx, key, object, options...)
}

func (r *recording) List(ctx context.Context, list client.ObjectList, options ...client.ListOption) error {
	r.note(list)
	return r.Client.List(ctx, list, options...)
}

type factory struct{ client client.Client }

func (f factory) For(kube.Principal) (*kube.Client, error) {
	return &kube.Client{Client: f.client}, nil
}

// arrange creates two namespaces with a schema each and lets the viewer read
// schemas and migrations in the first only.
func arrange(t *testing.T, viewer string) (allowed, forbidden string) {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	allowed, forbidden = viewer+"-shop", viewer+"-billing"
	for _, namespace := range []string{allowed, forbidden} {
		c.Assert(plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}), qt.IsNil)
		c.Assert(plane.Admin.Create(ctx, schema(namespace, "storefront")), qt.IsNil)
	}
	c.Assert(plane.Admin.Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: allowed, Name: "ptah-reader"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"operator.ptah.run"},
			Resources: []string{"ptahschemas", "ptahmigrations", "ptahschemaplans", "ptahmigrationplans"},
			Verbs:     []string{"get", "list", "watch"},
		}},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: allowed, Name: "ptah-reader"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "ptah-reader"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: viewer}},
	}), qt.IsNil)
	return allowed, forbidden
}

func schema(namespace, name string) *operatorv1alpha1.PtahSchema {
	return &operatorv1alpha1.PtahSchema{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: operatorv1alpha1.PtahSchemaSpec{
			Target: operatorv1alpha1.DatabaseTargetSpec{
				Engine:          operatorv1alpha1.DatabaseEnginePostgreSQL,
				CoordinationKey: "envtest/" + namespace,
				URLFrom: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: name + "-database"}, Key: "url",
				},
			},
			Desired: operatorv1alpha1.OCIArtifactSourceSpec{
				OCIRef: "oci://registry.example/shop/schema:1.0.0",
				VerificationPolicyFrom: corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "ptah-verification-policy"}, Key: "policy.yaml",
				},
			},
			// Typed Go sends a zero duration as "0s", which the CRD refuses;
			// the values are the operator example's.
			Policy:   operatorv1alpha1.ReconciliationPolicy{Apply: operatorv1alpha1.ApplyPolicyOnApproval, LockTimeout: metav1.Duration{Duration: 30 * time.Second}},
			Interval: metav1.Duration{Duration: 10 * time.Minute},
			Execution: operatorv1alpha1.ExecutionSpec{
				FailureRetryInterval: metav1.Duration{Duration: 30 * time.Second},
				ConnectTimeout:       metav1.Duration{Duration: 10 * time.Second},
			},
		},
	}
}

// signedIn serves the services behind a session for the principal, and
// returns a context carrying its cookie.
func signedIn(t *testing.T, register func(*connect.Server)) (*connect.Client, context.Context) {
	t.Helper()
	store := session.NewStore(session.Options{})
	token, _, err := store.Create(principal)
	qt.New(t).Assert(err, qt.IsNil)
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	register(server)
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	return connect.NewClient(connectinprocess.New(server)), ctx
}

func viewerClient(t *testing.T, name string) *recording {
	t.Helper()
	config, err := plane.User(name)
	qt.New(t).Assert(err, qt.IsNil)
	built, err := kube.NewClient(config)
	qt.New(t).Assert(err, qt.IsNil)
	return &recording{Client: built.Client, kinds: make(map[string]bool)}
}

// A namespace the identity may not read is reported as forbidden, never as a
// namespace without schemas, and what was read is the identity's alone.
func TestListSchemasReadsAsTheIdentityAndSaysWhatItCouldNotRead(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	allowed, forbidden := arrange(t, "list-viewer")
	reader := viewerClient(t, "list-viewer")
	transport, ctx := signedIn(t, func(server *connect.Server) {
		consolev1connect.RegisterSchemaServiceHandler(server, api.NewSchemaService(factory{reader}, api.Scope{}))
	})
	schemas := consolev1connect.NewSchemaServiceClient(transport)

	response, err := schemas.ListSchemas(ctx, &consolev1.ListSchemasRequest{Namespaces: []string{allowed, forbidden}})

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetSchemas(), qt.HasLen, 1)
	c.Assert(response.GetSchemas()[0].GetRef().GetNamespace(), qt.Equals, allowed)
	access := make(map[string]consolev1.Access)
	for _, result := range response.GetNamespaces() {
		access[result.GetNamespace()] = result.GetAccess()
	}
	c.Assert(access, qt.DeepEquals, map[string]consolev1.Access{
		allowed:   consolev1.Access_ACCESS_OK,
		forbidden: consolev1.Access_ACCESS_FORBIDDEN,
	})
	c.Assert(reader.kinds, qt.DeepEquals, map[string]bool{"*v1alpha1.PtahSchemaList": true})
}

// The hints are the API server's answers for this identity, namespace by
// namespace.
func TestListNamespacesHintsWhatTheIdentityMayDo(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	allowed, forbidden := arrange(t, "scope-viewer")
	reader := viewerClient(t, "scope-viewer")
	transport, ctx := signedIn(t, func(server *connect.Server) {
		consolev1connect.RegisterScopeServiceHandler(server, api.NewScopeService(factory{reader}, api.Scope{Candidates: []string{allowed, forbidden}}))
	})
	scope := consolev1connect.NewScopeServiceClient(transport)

	response, err := scope.ListNamespaces(ctx, &consolev1.ListNamespacesRequest{})

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetNamespaces(), qt.HasLen, 2)
	byName := make(map[string]*consolev1.NamespaceScope)
	for _, namespace := range response.GetNamespaces() {
		byName[namespace.GetName()] = namespace
	}
	c.Assert(byName[allowed].GetAccess(), qt.Equals, consolev1.Access_ACCESS_OK)
	c.Assert(byName[allowed].GetCapabilities().GetListSchemas(), qt.Equals, consolev1.Answer_ANSWER_ALLOWED)
	c.Assert(byName[allowed].GetCapabilities().GetApproveSchemaPlans(), qt.Equals, consolev1.Answer_ANSWER_DENIED)
	c.Assert(byName[allowed].GetCapabilities().GetReadPlanSql(), qt.Equals, consolev1.Answer_ANSWER_DENIED)
	c.Assert(byName[forbidden].GetAccess(), qt.Equals, consolev1.Access_ACCESS_FORBIDDEN)
}
