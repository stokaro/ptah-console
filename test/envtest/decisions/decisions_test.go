package decisions_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/session"
	"github.com/stokaro/ptah-console/internal/decision"
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

type factory struct{ client client.Client }

func (f factory) For(kube.Principal) (*kube.Client, error) {
	return &kube.Client{Client: f.client}, nil
}

// arrange creates a namespace holding a schema and grants the user the
// verbs given on schemas, and get on schemas and plans.
func arrange(t *testing.T, user string, verbs ...string) (string, *operatorv1alpha1.PtahSchema) {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	namespace := user + "-shop"
	c.Assert(plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}), qt.IsNil)
	stored := schema(namespace, "storefront")
	c.Assert(plane.Admin.Create(ctx, stored), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "ptah"},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"operator.ptah.run"}, Resources: []string{"ptahschemas", "ptahschemaplans"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{"operator.ptah.run"}, Resources: []string{"ptahschemas"}, Verbs: append([]string{"get"}, verbs...)},
		},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "ptah"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "ptah"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: user}},
	}), qt.IsNil)
	return namespace, stored
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
			Desired: operatorv1alpha1.SourceSpec{
				OCIRef: "oci://registry.example/shop/schema:1.0.0",
				VerificationPolicyFrom: &corev1.ConfigMapKeySelector{
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
	}
}

// service serves DecisionService as the user, and returns the client, a
// signed-in context and the audit stream.
func service(t *testing.T, user string) (consolev1connect.DecisionServiceClient, context.Context, *bytes.Buffer) {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User(user)
	c.Assert(err, qt.IsNil)
	built, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	store := session.NewStore(session.Options{})
	token, _, err := store.Create(principal)
	c.Assert(err, qt.IsNil)
	log := &bytes.Buffer{}
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterDecisionServiceHandler(server,
		api.NewDecisionService(factory{built.Client}, api.Scope{}, decision.NewLedger(), audit.New(log)))
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	return consolev1connect.NewDecisionServiceClient(connect.NewClient(connectinprocess.New(server))), ctx, log
}

func suspend(stored *operatorv1alpha1.PtahSchema, suspended bool) *consolev1.SetSuspendedRequest {
	return &consolev1.SetSuspendedRequest{
		Kind: consolev1.ResourceKind_RESOURCE_KIND_SCHEMA, Namespace: stored.Namespace, Name: stored.Name,
		Uid: string(stored.UID), ResourceVersion: stored.ResourceVersion, Suspended: suspended,
	}
}

// Suspending changes spec.suspend and nothing else, and the API server
// records the console as the manager of that field alone.
func TestSuspendChangesOnlySpecSuspend(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace, stored := arrange(t, "suspender", "patch")
	decisions, ctx, log := service(t, "suspender")

	response, err := decisions.SetSuspended(ctx, suspend(stored, true))

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
	after := &operatorv1alpha1.PtahSchema{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "storefront"}, after), qt.IsNil)
	c.Assert(after.Spec.Suspend, qt.IsTrue)
	c.Assert(response.GetResourceVersion(), qt.Equals, after.ResourceVersion)
	c.Assert(fieldsOwnedBy(t, after, "ptah-console"), qt.DeepEquals, map[string]any{"f:spec": map[string]any{"f:suspend": make(map[string]any)}})
	before := stored.Spec.DeepCopy()
	before.Suspend = true
	c.Assert(after.Spec, qt.DeepEquals, *before)
	var line map[string]any
	c.Assert(json.Unmarshal(log.Bytes(), &line), qt.IsNil)
	c.Assert(line["action"], qt.Equals, "suspend")
	c.Assert(line["outcome"], qt.Equals, audit.Admitted)
	c.Assert(line["auditID"], qt.Not(qt.Equals), "")
}

// A suspend sent for an object that changed, or that is another object under
// the same name now, changes nothing and says so.
func TestSuspendOfAChangedObjectChangesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		user   string
		change func(t *testing.T, stored *operatorv1alpha1.PtahSchema)
	}{
		{
			name: "edited since it was read", user: "editor",
			change: func(t *testing.T, stored *operatorv1alpha1.PtahSchema) {
				edited := stored.DeepCopy()
				edited.Spec.Interval = metav1.Duration{Duration: 20 * time.Minute}
				qt.New(t).Assert(plane.Admin.Update(t.Context(), edited), qt.IsNil)
			},
		},
		{
			name: "deleted and created again", user: "recreator",
			change: func(t *testing.T, stored *operatorv1alpha1.PtahSchema) {
				c := qt.New(t)
				c.Assert(plane.Admin.Delete(t.Context(), stored.DeepCopy()), qt.IsNil)
				c.Assert(plane.Admin.Create(t.Context(), schema(stored.Namespace, stored.Name)), qt.IsNil)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			namespace, stored := arrange(t, test.user, "patch")
			decisions, ctx, _ := service(t, test.user)
			test.change(t, stored)

			response, err := decisions.SetSuspended(ctx, suspend(stored, true))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED)
			after := &operatorv1alpha1.PtahSchema{}
			c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "storefront"}, after), qt.IsNil)
			c.Assert(after.Spec.Suspend, qt.IsFalse)
		})
	}
}

// An identity RBAC does not let patch is told so in the API server's words,
// and nothing changes.
func TestSuspendWithoutPatchIsForbidden(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace, stored := arrange(t, "watcher")
	decisions, ctx, _ := service(t, "watcher")

	response, err := decisions.SetSuspended(ctx, suspend(stored, true))

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_FORBIDDEN)
	c.Assert(response.GetMessage(), qt.Contains, `User "watcher" cannot patch resource "ptahschemas"`)
	after := &operatorv1alpha1.PtahSchema{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "storefront"}, after), qt.IsNil)
	c.Assert(after.Spec.Suspend, qt.IsFalse)
}

// fieldsOwnedBy returns the fields one manager owns on the object.
func fieldsOwnedBy(t *testing.T, object client.Object, manager string) map[string]any {
	t.Helper()
	for _, entry := range object.GetManagedFields() {
		if entry.Manager != manager {
			continue
		}
		fields := make(map[string]any)
		qt.New(t).Assert(json.Unmarshal(entry.FieldsV1.GetRawBytes(), &fields), qt.IsNil)
		return fields
	}
	return nil
}
