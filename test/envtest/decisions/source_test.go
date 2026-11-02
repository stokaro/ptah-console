package decisions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/session"
	"github.com/stokaro/ptah-console/internal/decision"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

var (
	pinnedNext  = "oci://registry.example/shop/schema@sha256:" + strings.Repeat("b", 64)
	pinnedOther = "oci://registry.example/shop/schema@sha256:" + strings.Repeat("c", 64)
)

// sourceService serves DecisionService as the user, through the user's own
// client wrapped by funcs, and records the error of every patch it sent.
type sourceService struct {
	decisions consolev1connect.DecisionServiceClient
	ctx       context.Context
	log       *bytes.Buffer
	mu        *sync.Mutex
	patched   *[]error
}

func (s sourceService) patches() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), *s.patched...)
}

// beforePatch runs before each patch reaches the API server, with the
// number of that patch from 1.
type beforePatch func(ctx context.Context, attempt int)

func sourceServiceFor(t *testing.T, user string, before beforePatch) sourceService {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User(user)
	c.Assert(err, qt.IsNil)
	built, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	watching, ok := built.Client.(client.WithWatch)
	c.Assert(ok, qt.IsTrue)
	var (
		mu      sync.Mutex
		patched []error
	)
	wrapped := interceptor.NewClient(watching, interceptor.Funcs{
		Patch: func(ctx context.Context, inner client.WithWatch, object client.Object, patch client.Patch, options ...client.PatchOption) error {
			mu.Lock()
			attempt := len(patched) + 1
			mu.Unlock()
			if before != nil {
				before(ctx, attempt)
			}
			err := inner.Patch(ctx, object, patch, options...)
			mu.Lock()
			patched = append(patched, err)
			mu.Unlock()
			return err
		},
	})
	store := session.NewStore(session.Options{})
	token, _, err := store.Create(principal)
	c.Assert(err, qt.IsNil)
	log := &bytes.Buffer{}
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterDecisionServiceHandler(server,
		api.NewDecisionService(factory{wrapped}, api.Scope{}, decision.NewLedger(), audit.New(log)))
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	return sourceService{
		decisions: consolev1connect.NewDecisionServiceClient(connect.NewClient(connectinprocess.New(server))),
		ctx:       ctx, log: log, mu: &mu, patched: &patched,
	}
}

// beforeFirst runs write before the first patch and before no other.
func beforeFirst(write func()) beforePatch {
	return func(_ context.Context, attempt int) {
		if attempt == 1 {
			write()
		}
	}
}

func changeSource(stored *operatorv1alpha1.PtahSchema, dryRun bool) *consolev1.SetSourceRequest {
	return &consolev1.SetSourceRequest{
		Kind: consolev1.ResourceKind_RESOURCE_KIND_SCHEMA, Namespace: stored.Namespace, Name: stored.Name,
		Uid: string(stored.UID), FromReference: stored.Spec.Desired.OCIRef, Reference: pinnedNext, DryRun: dryRun,
	}
}

func storedSchema(t *testing.T, namespace string) *operatorv1alpha1.PtahSchema {
	t.Helper()
	after := &operatorv1alpha1.PtahSchema{}
	qt.New(t).Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "storefront"}, after), qt.IsNil)
	return after
}

// A change of source sets spec.desired.ociRef and nothing else, and the API
// server records the console as the manager of that field alone. The
// verification policy stays, and the generation moves for the operator to
// observe.
func TestSetSourceChangesOnlyTheReference(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace, stored := arrange(t, "publisher", "patch")
	service := sourceServiceFor(t, "publisher", nil)

	response, err := service.decisions.SetSource(service.ctx, changeSource(stored, false))

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, qt.Commentf("%s", response.GetMessage()))
	c.Assert(response.GetChanged(), qt.IsTrue)
	after := storedSchema(t, namespace)
	before := stored.Spec.DeepCopy()
	before.Desired.OCIRef = pinnedNext
	c.Assert(after.Spec, qt.DeepEquals, *before)
	c.Assert(after.Generation, qt.Equals, stored.Generation+1)
	c.Assert(response.GetGeneration(), qt.Equals, after.Generation)
	c.Assert(response.GetResourceVersion(), qt.Equals, after.ResourceVersion)
	c.Assert(response.GetPrevious(), qt.Equals, stored.Spec.Desired.OCIRef)
	c.Assert(fieldsOwnedBy(t, after, "ptah-console"), qt.DeepEquals, map[string]any{
		"f:spec": map[string]any{"f:desired": map[string]any{"f:ociRef": make(map[string]any)}},
	})
	var line map[string]any
	c.Assert(json.Unmarshal(service.log.Bytes(), &line), qt.IsNil)
	c.Assert(line["action"], qt.Equals, "set-source")
	c.Assert(line["outcome"], qt.Equals, audit.Admitted)
	c.Assert(line["reference"], qt.Equals, pinnedNext)
	c.Assert(line["auditID"], qt.Not(qt.Equals), "")
}

// A check is judged by the API server, which answers with the source as it
// would store it, and stores nothing.
func TestASourceCheckIsJudgedAndStoresNothing(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace, stored := arrange(t, "checker", "patch")
	service := sourceServiceFor(t, "checker", nil)

	response, err := service.decisions.SetSource(service.ctx, changeSource(stored, true))

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, qt.Commentf("%s", response.GetMessage()))
	c.Assert(response.GetDryRun(), qt.IsTrue)
	// The stored source as the API server has it: the typed create stored a
	// transport of plainHTTP false, which the change keeps.
	c.Assert(response.GetSourceYaml(), qt.Equals, "desired:\n  ociRef: "+pinnedNext+
		"\n  transport:\n    plainHTTP: false\n  verificationPolicyFrom:\n    key: policy.yaml\n    name: ptah-verification-policy\n")
	after := storedSchema(t, namespace)
	c.Assert(after.Spec, qt.DeepEquals, stored.Spec)
	c.Assert(after.ResourceVersion, qt.Equals, stored.ResourceVersion)
	c.Assert(service.log.Len(), qt.Equals, 0)
}

// The patch tests the resource version the console read, so the API server
// refuses it once another write landed in between. A status write takes
// nothing from the decision, which is sent again from a fresh read; another
// writer's reference is reported, and stays.
func TestTheAPIServerRefusesAPatchOverAnotherWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		user    string
		write   func(t *testing.T, stored *operatorv1alpha1.PtahSchema)
		outcome consolev1.DecisionOutcome
		stored  string
		patches int
	}{
		{
			name: "the operator wrote status", user: "racer",
			write: func(t *testing.T, stored *operatorv1alpha1.PtahSchema) {
				current := storedSchema(t, stored.Namespace)
				current.Status.Phase = operatorv1alpha1.PhaseResolving
				qt.New(t).Assert(plane.Admin.Status().Update(t.Context(), current), qt.IsNil)
			},
			outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, stored: pinnedNext, patches: 2,
		},
		{
			name: "another writer set its own reference", user: "overtaken",
			write: func(t *testing.T, stored *operatorv1alpha1.PtahSchema) {
				current := storedSchema(t, stored.Namespace)
				current.Spec.Desired.OCIRef = pinnedOther
				qt.New(t).Assert(plane.Admin.Update(t.Context(), current), qt.IsNil)
			},
			outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, stored: pinnedOther, patches: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			namespace, stored := arrange(t, test.user, "patch")
			// Between the console's read and its first patch.
			service := sourceServiceFor(t, test.user, beforeFirst(func() { test.write(t, stored) }))

			response, err := service.decisions.SetSource(service.ctx, changeSource(stored, false))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetOutcome(), qt.Equals, test.outcome, qt.Commentf("%s", response.GetMessage()))
			c.Assert(storedSchema(t, namespace).Spec.Desired.OCIRef, qt.Equals, test.stored)
			patches := service.patches()
			c.Assert(patches, qt.HasLen, test.patches)
			// The API server refused the patch that met the write: the
			// test of the resource version failed.
			c.Assert(apierrors.IsInvalid(patches[0]), qt.IsTrue, qt.Commentf("%v", patches[0]))
		})
	}
}

// An identity RBAC does not let patch is told so in the API server's words,
// and nothing changes.
func TestSetSourceWithoutPatchIsForbidden(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace, stored := arrange(t, "browser")
	service := sourceServiceFor(t, "browser", nil)

	response, err := service.decisions.SetSource(service.ctx, changeSource(stored, false))

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_FORBIDDEN)
	c.Assert(response.GetMessage(), qt.Contains, `User "browser" cannot patch resource "ptahschemas"`)
	c.Assert(storedSchema(t, namespace).Spec.Desired.OCIRef, qt.Equals, stored.Spec.Desired.OCIRef)
}

// A migration's source is spec.artifact, changed the same way.
func TestSetSourceChangesAMigrationsArtifact(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := t.Context()
	namespace := "migrator-shop"
	c.Assert(plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}), qt.IsNil)
	migration := &operatorv1alpha1.PtahMigration{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "billing"},
		Spec: operatorv1alpha1.PtahMigrationSpec{
			Target: schema(namespace, "billing").Spec.Target,
			Artifact: operatorv1alpha1.SourceSpec{
				OCIRef: "oci://registry.example/shop/billing:1.0.0",
				VerificationPolicyFrom: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "ptah-verification-policy"}, Key: "policy.yaml",
				},
			},
			Policy:   operatorv1alpha1.MigrationPolicy{Apply: operatorv1alpha1.ApplyPolicyOnApproval, LockTimeout: metav1.Duration{Duration: 30 * time.Second}},
			Interval: metav1.Duration{Duration: 10 * time.Minute},
			Execution: operatorv1alpha1.ExecutionSpec{
				FailureRetryInterval: metav1.Duration{Duration: 30 * time.Second},
				ConnectTimeout:       metav1.Duration{Duration: 10 * time.Second},
			},
		},
	}
	c.Assert(plane.Admin.Create(ctx, migration), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "ptah"},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{"operator.ptah.run"}, Resources: []string{"ptahmigrations"}, Verbs: []string{"get", "patch"}}},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "ptah"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "ptah"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: "migrator"}},
	}), qt.IsNil)
	service := sourceServiceFor(t, "migrator", nil)

	response, err := service.decisions.SetSource(service.ctx, &consolev1.SetSourceRequest{
		Kind: consolev1.ResourceKind_RESOURCE_KIND_MIGRATION, Namespace: namespace, Name: "billing", Uid: string(migration.UID),
		FromReference: migration.Spec.Artifact.OCIRef, Reference: pinnedNext,
	})

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, qt.Commentf("%s", response.GetMessage()))
	c.Assert(response.GetField(), qt.Equals, "spec.artifact.ociRef")
	after := &operatorv1alpha1.PtahMigration{}
	c.Assert(plane.Admin.Get(ctx, client.ObjectKeyFromObject(migration), after), qt.IsNil)
	before := migration.Spec.DeepCopy()
	before.Artifact.OCIRef = pinnedNext
	c.Assert(after.Spec, qt.DeepEquals, *before)
	c.Assert(fieldsOwnedBy(t, after, "ptah-console"), qt.DeepEquals, map[string]any{
		"f:spec": map[string]any{"f:artifact": map[string]any{"f:ociRef": make(map[string]any)}},
	})
}
