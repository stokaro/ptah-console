package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
	oldReference   = "oci://registry.example/shop/storefront@sha256:" + strings.Repeat("a", 64)
	newReference   = "oci://registry.example/shop/storefront@sha256:" + strings.Repeat("b", 64)
	otherReference = "oci://registry.example/shop/storefront@sha256:" + strings.Repeat("c", 64)
)

// sourced is a resource of the kind at rest on oldReference, with something
// in every source setting a change of reference must leave alone.
func sourced(kind consolev1.ResourceKind) client.Object {
	source := operatorv1alpha1.SourceSpec{
		OCIRef:                 oldReference,
		VerificationPolicyFrom: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "ptah-verification-policy"}, Key: "policy.yaml"},
		RegistryAuthFrom:       &operatorv1alpha1.RegistryAuthSource{Name: "regcred", Mode: operatorv1alpha1.RegistryAuthDockerConfigJSON},
		Transport: operatorv1alpha1.OCITransportSpec{PlainHTTP: true,
			CAFrom: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "registry-ca"}, Key: "ca.crt"}},
	}
	meta := metav1.ObjectMeta{Namespace: namespace, Name: "storefront", UID: "uid-resource", Generation: 4}
	if kind == consolev1.ResourceKind_RESOURCE_KIND_MIGRATION {
		return &operatorv1alpha1.PtahMigration{ObjectMeta: meta, Spec: operatorv1alpha1.PtahMigrationSpec{
			Artifact: source, Policy: operatorv1alpha1.MigrationPolicy{Apply: operatorv1alpha1.ApplyPolicyOnApproval},
		}}
	}
	return &operatorv1alpha1.PtahSchema{ObjectMeta: meta, Spec: operatorv1alpha1.PtahSchemaSpec{
		Desired: source, Policy: operatorv1alpha1.ReconciliationPolicy{Apply: operatorv1alpha1.ApplyPolicyOnApproval},
	}}
}

// withSource is the resource with its source replaced.
func withSource(kind consolev1.ResourceKind, change func(*operatorv1alpha1.SourceSpec)) client.Object {
	object := sourced(kind)
	switch typed := object.(type) {
	case *operatorv1alpha1.PtahSchema:
		change(&typed.Spec.Desired)
	case *operatorv1alpha1.PtahMigration:
		change(&typed.Spec.Artifact)
	}
	return object
}

// applying is the resource with an Apply claimed, its dispatch begun or not.
func applying(kind consolev1.ResourceKind, dispatched bool) client.Object {
	object := sourced(kind)
	switch typed := object.(type) {
	case *operatorv1alpha1.PtahSchema:
		typed.Status.ActiveOperation = &operatorv1alpha1.ActiveOperationStatus{
			Type: operatorv1alpha1.OperationApply, ID: "apply-1", JobName: "ptah-apply", DispatchStarted: dispatched}
	case *operatorv1alpha1.PtahMigration:
		typed.Status.ActiveOperation = &operatorv1alpha1.MigrationOperationStatus{
			Type: operatorv1alpha1.MigrationOperationApply, ID: "apply-1", JobName: "ptah-apply", DispatchStarted: dispatched}
	}
	return object
}

// sentPatch is one patch the console sent, as the client saw it.
type sentPatch struct {
	Operations []map[string]any
	Manager    string
	DryRun     []string
}

// sourceService is the service under test over a fake API server, with every
// read and patch it sent.
type sourceService struct {
	client  consolev1connect.DecisionServiceClient
	ctx     context.Context
	store   client.Client
	audit   *bytes.Buffer
	mu      *sync.Mutex
	patches *[]sentPatch
	reads   *int
}

func (s sourceService) sent() []sentPatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sentPatch(nil), *s.patches...)
}

func (s sourceService) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.reads
}

// patchAnswer answers one patch: nil lets the fake store it.
type patchAnswer func(ctx context.Context, c client.WithWatch, object client.Object, attempt int) error

func setSourceService(t *testing.T, answer patchAnswer, objects ...client.Object) sourceService {
	t.Helper()
	return setSourceServiceReading(t, answer, nil, objects...)
}

// readAnswer answers the nth read, from 1: nil lets the fake read it.
type readAnswer func(read int) error

func setSourceServiceReading(t *testing.T, answer patchAnswer, readFails readAnswer, objects ...client.Object) sourceService {
	t.Helper()
	var (
		mu      sync.Mutex
		patches []sentPatch
		reads   int
	)
	scheme, err := kube.Scheme()
	qt.New(t).Assert(err, qt.IsNil)
	built := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			mu.Lock()
			reads++
			read := reads
			mu.Unlock()
			if readFails != nil {
				if err := readFails(read); err != nil {
					return err
				}
			}
			return c.Get(ctx, key, object, options...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, object client.Object, patch client.Patch, options ...client.PatchOption) error {
			data, err := patch.Data(object)
			if err != nil {
				return err
			}
			var operations []map[string]any
			if err := json.Unmarshal(data, &operations); err != nil {
				return err
			}
			resolved := &client.PatchOptions{}
			resolved.ApplyOptions(options)
			mu.Lock()
			patches = append(patches, sentPatch{Operations: operations, Manager: resolved.FieldManager, DryRun: resolved.DryRun})
			attempt := len(patches)
			mu.Unlock()
			if answer != nil {
				if err := answer(ctx, c, object, attempt); err != nil {
					return err
				}
			}
			return c.Patch(ctx, object, patch, options...)
		},
	}).Build()
	log := &bytes.Buffer{}
	store := session.NewStore(session.Options{})
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterDecisionServiceHandler(server,
		api.NewDecisionService(staticFactory{built}, api.Scope{}, decision.NewLedger(), audit.New(log)))
	return sourceService{
		client: consolev1connect.NewDecisionServiceClient(connect.NewClient(connectinprocess.New(server))),
		ctx:    signedIn(t, store), store: built, audit: log, mu: &mu, patches: &patches, reads: &reads,
	}
}

func setSource(kind consolev1.ResourceKind, change func(*consolev1.SetSourceRequest)) *consolev1.SetSourceRequest {
	request := &consolev1.SetSourceRequest{
		Kind: kind, Namespace: namespace, Name: "storefront", Uid: "uid-resource",
		FromReference: oldReference, Reference: newReference,
	}
	change(request)
	return request
}

// sourceOf reads the source the store holds now.
func sourceOf(t *testing.T, store client.Client, kind consolev1.ResourceKind) operatorv1alpha1.SourceSpec {
	t.Helper()
	c := qt.New(t)
	key := types.NamespacedName{Namespace: namespace, Name: "storefront"}
	if kind == consolev1.ResourceKind_RESOURCE_KIND_MIGRATION {
		migration := &operatorv1alpha1.PtahMigration{}
		c.Assert(store.Get(t.Context(), key, migration), qt.IsNil)
		return migration.Spec.Artifact
	}
	schema := &operatorv1alpha1.PtahSchema{}
	c.Assert(store.Get(t.Context(), key, schema), qt.IsNil)
	return schema.Spec.Desired
}

var families = []struct {
	name    string
	kind    consolev1.ResourceKind
	pointer string
}{
	{name: "a schema", kind: consolev1.ResourceKind_RESOURCE_KIND_SCHEMA, pointer: "/spec/desired/ociRef"},
	{name: "a migration", kind: consolev1.ResourceKind_RESOURCE_KIND_MIGRATION, pointer: "/spec/artifact/ociRef"},
}

// The change is one JSON patch under the console's field manager: a test of
// the UID and of the resource version read, and a replace of the reference.
// Every other source setting stays as it was, and the line in the audit says
// who set which reference.
func TestSetSourceSendsOneGuardedFieldChange(t *testing.T) {
	t.Parallel()
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := setSourceService(t, nil, sourced(family.kind))
			before := sourceOf(t, service.store, family.kind)
			stored := sourced(family.kind)
			c.Assert(service.store.Get(t.Context(), client.ObjectKeyFromObject(stored), stored), qt.IsNil)

			response, err := service.client.SetSource(service.ctx, setSource(family.kind, func(*consolev1.SetSourceRequest) {}))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
			c.Assert(response.GetChanged(), qt.IsTrue)
			c.Assert(response.GetPrevious(), qt.Equals, oldReference)
			c.Assert(response.GetField(), qt.Equals, strings.ReplaceAll(strings.TrimPrefix(family.pointer, "/"), "/", "."))
			c.Assert(service.sent(), qt.DeepEquals, []sentPatch{{Manager: "ptah-console", Operations: []map[string]any{
				{"op": "test", "path": "/metadata/uid", "value": "uid-resource"},
				{"op": "test", "path": "/metadata/resourceVersion", "value": stored.GetResourceVersion()},
				{"op": "replace", "path": family.pointer, "value": newReference},
			}}})
			after := sourceOf(t, service.store, family.kind)
			before.OCIRef = newReference
			c.Assert(after, qt.DeepEquals, before)
			lines := auditLines(t, service.audit)
			c.Assert(lines, qt.HasLen, 1)
			c.Assert(lines[0]["action"], qt.Equals, "set-source")
			c.Assert(lines[0]["outcome"], qt.Equals, audit.Admitted)
			c.Assert(lines[0]["reference"], qt.Equals, newReference)
		})
	}
}

// A check with the API server sends the same patch as a dry run, stores
// nothing and writes no audit line, as a checked draft does not.
func TestASourceCheckStoresNothing(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := setSourceService(t, nil, sourced(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA))

	response, err := service.client.SetSource(service.ctx, setSource(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA,
		func(r *consolev1.SetSourceRequest) { r.DryRun = true }))

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
	c.Assert(response.GetDryRun(), qt.IsTrue)
	c.Assert(response.GetChanged(), qt.IsFalse)
	c.Assert(service.sent(), qt.HasLen, 1)
	c.Assert(service.sent()[0].DryRun, qt.DeepEquals, []string{metav1.DryRunAll})
	c.Assert(sourceOf(t, service.store, consolev1.ResourceKind_RESOURCE_KIND_SCHEMA).OCIRef, qt.Equals, oldReference)
	c.Assert(service.audit.Len(), qt.Equals, 0)
}

// A reference the operator would refuse is refused before the cluster is
// read, in the operator's words, and nothing is sent or audited.
func TestARefusedReferenceReadsAndSendsNothing(t *testing.T) {
	t.Parallel()
	rows := []struct{ name, reference, says string }{
		{name: "a tag", reference: "oci://registry.example/shop/storefront:1.5.0", says: "names tag 1.5.0, not a digest"},
		{name: "a tag beside the digest", reference: "oci://registry.example/shop/storefront:1.5.0@sha256:" + strings.Repeat("b", 64),
			says: "names tag 1.5.0 beside its digest"},
		{name: "a nested scheme", reference: "oci://registry.example/oci://other.example/shop@sha256:" + strings.Repeat("b", 64),
			says: "a repository path carries no scheme"},
		{name: "no scheme", reference: "registry.example/shop/storefront@sha256:" + strings.Repeat("b", 64), says: "oci:// scheme"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := setSourceService(t, nil, sourced(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA))

			_, err := service.client.SetSource(service.ctx, setSource(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA,
				func(r *consolev1.SetSourceRequest) { r.Reference = row.reference }))

			c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeInvalidArgument)
			c.Assert(err, qt.ErrorMatches, `.*`+regexp.QuoteMeta(row.says)+`.*Nothing was sent\.`)
			c.Assert(service.readCount(), qt.Equals, 0)
			c.Assert(service.sent(), qt.HasLen, 0)
			c.Assert(service.audit.Len(), qt.Equals, 0)
		})
	}
}

// Each state the console will not send the change to is refused after the
// read and before any patch: the object is not the one the person saw, its
// spec names another reference or another kind of source, or an Apply whose
// Job may exist is running. An Apply claimed with no Job yet is not refused:
// the operator drops it when it sees the change.
func TestSetSourceRefusesWhatThePersonDidNotSee(t *testing.T) {
	t.Parallel()
	schema := consolev1.ResourceKind_RESOURCE_KIND_SCHEMA
	migration := consolev1.ResourceKind_RESOURCE_KIND_MIGRATION
	rows := []struct {
		name    string
		kind    consolev1.ResourceKind
		stored  []client.Object
		request func(*consolev1.SetSourceRequest)
		says    string
	}{
		{name: "gone", kind: schema, request: func(*consolev1.SetSourceRequest) {}, says: "The schema no longer exists. Nothing was changed."},
		{name: "another object under the name", kind: schema, stored: []client.Object{sourced(schema)},
			request: func(r *consolev1.SetSourceRequest) { r.Uid = "uid-earlier" },
			says:    "The schema was deleted and created again since you looked at it. Nothing was changed."},
		{name: "a ConfigMap source", kind: schema, stored: []client.Object{withSource(schema, func(s *operatorv1alpha1.SourceSpec) {
			*s = operatorv1alpha1.SourceSpec{ConfigMap: &operatorv1alpha1.ConfigMapSourceSpec{Name: "storefront-schema"}}
		})}, request: func(*consolev1.SetSourceRequest) {}, says: "spec.desired of storefront names a ConfigMap source, not an OCI artifact. Nothing was changed."},
		{name: "a Git source", kind: migration, stored: []client.Object{withSource(migration, func(s *operatorv1alpha1.SourceSpec) {
			*s = operatorv1alpha1.SourceSpec{Git: &operatorv1alpha1.GitSourceSpec{URL: "https://git.example/shop.git"}}
		})}, request: func(*consolev1.SetSourceRequest) {}, says: "spec.artifact of storefront names a Git source, not an OCI artifact."},
		{name: "another writer's reference", kind: migration, stored: []client.Object{withSource(migration, func(s *operatorv1alpha1.SourceSpec) {
			s.OCIRef = otherReference
		})}, request: func(*consolev1.SetSourceRequest) {},
			says: "spec.artifact.ociRef changed since you looked: it named " + oldReference + " and now names " + otherReference},
		{name: "a schema Apply whose Job may exist", kind: schema, stored: []client.Object{applying(schema, true)},
			request: func(*consolev1.SetSourceRequest) {}, says: "An Apply is running on schema storefront (Job ptah-apply). Nothing was changed."},
		{name: "a migration Apply whose Job may exist", kind: migration, stored: []client.Object{applying(migration, true)},
			request: func(*consolev1.SetSourceRequest) {}, says: "An Apply is running on migration storefront (Job ptah-apply). Nothing was changed."},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := setSourceService(t, nil, row.stored...)

			response, err := service.client.SetSource(service.ctx, setSource(row.kind, row.request))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED)
			c.Assert(response.GetMessage(), qt.Contains, row.says)
			c.Assert(service.sent(), qt.HasLen, 0)
			c.Assert(auditLines(t, service.audit)[0]["outcome"], qt.Equals, audit.PlanChanged)
		})
	}
}

// An Apply that is claimed and has no Job yet does not hold the change back,
// in either family.
func TestAnApplyWithNoJobYetTakesTheChange(t *testing.T) {
	t.Parallel()
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := setSourceService(t, nil, applying(family.kind, false))

			response, err := service.client.SetSource(service.ctx, setSource(family.kind, func(*consolev1.SetSourceRequest) {}))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
			c.Assert(service.sent(), qt.HasLen, 1)
			c.Assert(sourceOf(t, service.store, family.kind).OCIRef, qt.Equals, newReference)
		})
	}
}

// A spec that names the reference already needs nothing sent, and the answer
// says whether another writer set it after the person looked.
func TestAReferenceAlreadySetSendsNothing(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		from string
		says string
	}{
		{name: "asked for what the person saw", from: newReference, says: "spec.desired.ociRef already names this reference. Nothing was sent."},
		{name: "set by another writer", from: oldReference,
			says: "spec.desired.ociRef names this reference already: another writer set it after you looked. Nothing was sent."},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := setSourceService(t, nil, withSource(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA,
				func(s *operatorv1alpha1.SourceSpec) { s.OCIRef = newReference }))

			response, err := service.client.SetSource(service.ctx, setSource(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA,
				func(r *consolev1.SetSourceRequest) { r.FromReference = row.from }))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
			c.Assert(response.GetChanged(), qt.IsFalse)
			c.Assert(response.GetMessage(), qt.Equals, row.says)
			c.Assert(service.sent(), qt.HasLen, 0)
		})
	}
}

// A write between the console's read and its patch makes the API server
// refuse the patch: the test of the resource version fails with 422, as
// test/envtest/decisions shows the real API server does, and a 409 Conflict
// is taken the same way. The console reads again and decides from that read:
// the operator's own status write changes nothing the change rests on, so it
// is sent again with the new resource version; another writer's reference is
// reported rather than replaced; and an object that never holds still for
// three tries is left alone.
func TestAConflictIsDecidedAgainFromAFreshRead(t *testing.T) {
	t.Parallel()
	conflict := apierrors.NewConflict(operatorv1alpha1.GroupVersion.WithResource("ptahschemas").GroupResource(), "storefront",
		errors.New("the object has been modified"))
	rows := []struct {
		name    string
		answer  patchAnswer
		outcome consolev1.DecisionOutcome
		says    string
		patches int
		stored  string
	}{
		{name: "the operator wrote status", answer: func(ctx context.Context, c client.WithWatch, _ client.Object, attempt int) error {
			return conflictOnce(ctx, c, attempt, statusWrite, testFailed())
		}, outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, patches: 2, stored: newReference},
		{name: "the operator wrote status, answered with a conflict", answer: func(ctx context.Context, c client.WithWatch, _ client.Object, attempt int) error {
			return conflictOnce(ctx, c, attempt, statusWrite, conflict)
		}, outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, patches: 2, stored: newReference},
		{name: "another writer set another reference", answer: func(ctx context.Context, c client.WithWatch, _ client.Object, attempt int) error {
			return conflictOnce(ctx, c, attempt, otherWriter, testFailed())
		}, outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, patches: 1, stored: otherReference,
			says: "spec.desired.ociRef changed since you looked"},
		{name: "the object never holds still", answer: func(ctx context.Context, c client.WithWatch, _ client.Object, _ int) error {
			return errors.Join(statusWrite(ctx, c), testFailed())
		}, outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, patches: 3, stored: oldReference,
			says: "kept changing while the console sent the change, 3 times over. Nothing was changed"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := setSourceService(t, row.answer, sourced(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA))

			response, err := service.client.SetSource(service.ctx, setSource(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA,
				func(*consolev1.SetSourceRequest) {}))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetOutcome(), qt.Equals, row.outcome)
			c.Assert(response.GetMessage(), qt.Contains, row.says)
			c.Assert(service.sent(), qt.HasLen, row.patches)
			c.Assert(sourceOf(t, service.store, consolev1.ResourceKind_RESOURCE_KIND_SCHEMA).OCIRef, qt.Equals, row.stored)
			c.Assert(distinctVersionsTested(service.sent()), qt.Equals, row.patches)
		})
	}
}

// statusWrite stands for the operator writing status between the read and
// the patch, which moves the resource version.
func statusWrite(ctx context.Context, c client.WithWatch) error {
	return rewrite(ctx, c, func(schema *operatorv1alpha1.PtahSchema) { schema.Status.Phase = operatorv1alpha1.PhaseInSync })
}

// otherWriter sets another reference between the read and the patch.
func otherWriter(ctx context.Context, c client.WithWatch) error {
	return rewrite(ctx, c, func(schema *operatorv1alpha1.PtahSchema) { schema.Spec.Desired.OCIRef = otherReference })
}

func rewrite(ctx context.Context, c client.WithWatch, change func(*operatorv1alpha1.PtahSchema)) error {
	schema := &operatorv1alpha1.PtahSchema{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "storefront"}, schema); err != nil {
		return err
	}
	change(schema)
	return c.Update(ctx, schema)
}

// testFailed is the API server's answer to a JSON patch whose test failed.
func testFailed() error {
	return apierrors.NewGenericServerResponse(http.StatusUnprocessableEntity, "patch",
		operatorv1alpha1.GroupVersion.WithResource("ptahschemas").GroupResource(), "storefront",
		`testing value /metadata/resourceVersion failed: test failed`, 0, false)
}

// A refused patch is the answer to the change: when the read that would
// decide again fails, the console says the API server refused it, and the
// line in the audit says so too.
func TestARefusedChangeWhoseRereadFailsIsReportedAsRefused(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := setSourceServiceReading(t, func(ctx context.Context, c client.WithWatch, _ client.Object, _ int) error {
		return errors.Join(statusWrite(ctx, c), testFailed())
	}, failAfterFirstRead, sourced(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA))

	response, err := service.client.SetSource(service.ctx, setSource(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA,
		func(*consolev1.SetSourceRequest) {}))

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED)
	c.Assert(response.GetMessage(), qt.Contains, "reading the schema again to see why failed: connection reset by peer")
	c.Assert(service.sent(), qt.HasLen, 1)
	c.Assert(auditLines(t, service.audit)[0]["outcome"], qt.Equals, audit.Refused)
}

// failAfterFirstRead lets the console's first read decide, and fails the
// moved check and every later read.
func failAfterFirstRead(read int) error {
	if read > 1 {
		return errors.New("connection reset by peer")
	}
	return nil
}

// conflictOnce answers the first patch with a conflict after write moved the
// object, and lets every later one through.
func conflictOnce(ctx context.Context, c client.WithWatch, attempt int, write func(context.Context, client.WithWatch) error, conflict error) error {
	if attempt > 1 {
		return nil
	}
	return errors.Join(write(ctx, c), conflict)
}

// distinctVersionsTested counts the resource versions the patches tested:
// each retry tests the version of its own read.
func distinctVersionsTested(patches []sentPatch) int {
	versions := make(map[any]bool)
	for _, patch := range patches {
		versions[patch.Operations[1]["value"]] = true
	}
	return len(versions)
}

// The API server's and admission's answers are reported as what they are,
// in their own words, and a patch that got no answer may have been stored.
func TestSetSourceReportsTheAPIServersAnswer(t *testing.T) {
	t.Parallel()
	refuse := func(code int32, reason metav1.StatusReason, message string) patchAnswer {
		return func(context.Context, client.WithWatch, client.Object, int) error {
			return &apierrors.StatusError{ErrStatus: metav1.Status{Status: metav1.StatusFailure, Code: code, Reason: reason, Message: message}}
		}
	}
	rows := []struct {
		name    string
		answer  patchAnswer
		outcome consolev1.DecisionOutcome
		says    string
		audited string
	}{
		{name: "RBAC", answer: refuse(403, metav1.StatusReasonForbidden,
			`ptahschemas.operator.ptah.run "storefront" is forbidden: User "viewer" cannot patch resource "ptahschemas" in API group "operator.ptah.run" in the namespace "application"`),
			outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_FORBIDDEN, says: `cannot patch resource "ptahschemas"`, audited: audit.Forbidden},
		{name: "admission", answer: refuse(403, metav1.StatusReasonForbidden,
			`admission webhook "mschemawriter.operator.ptah.run" denied the request: no`),
			outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, says: `denied the request: no`, audited: audit.Refused},
		{name: "the schema, with the object where it was", answer: refuse(422, metav1.StatusReasonInvalid,
			`PtahSchema.operator.ptah.run "storefront" is invalid: spec.desired.ociRef: Invalid value`),
			outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, says: `spec.desired.ociRef: Invalid value`, audited: audit.Refused},
		{name: "no answer", answer: func(context.Context, client.WithWatch, client.Object, int) error {
			return errors.New("read tcp 127.0.0.1:52144->127.0.0.1:6443: connection reset by peer")
		}, outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_UNKNOWN,
			says: "The answer was lost. The change may have been stored; look at spec.desired.ociRef before changing it again.", audited: audit.Unknown},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := setSourceService(t, row.answer, sourced(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA))

			response, err := service.client.SetSource(service.ctx, setSource(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA,
				func(*consolev1.SetSourceRequest) {}))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetOutcome(), qt.Equals, row.outcome)
			c.Assert(response.GetMessage(), qt.Contains, row.says)
			c.Assert(service.sent(), qt.HasLen, 1)
			c.Assert(auditLines(t, service.audit)[0]["outcome"], qt.Equals, row.audited)
		})
	}
}

// A console started read-only refuses the change before it reaches the
// handler: SetSource is not declared free of side effects.
func TestAReadOnlyConsoleRefusesAChangeOfSource(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	scheme, err := kube.Scheme()
	c.Assert(err, qt.IsNil)
	built := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sourced(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA)).Build()
	store := session.NewStore(session.Options{})
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}), api.ReadOnly())
	consolev1connect.RegisterDecisionServiceHandler(server,
		api.NewDecisionService(staticFactory{built}, api.Scope{}, decision.NewLedger(), audit.Discard()))
	decisions := consolev1connect.NewDecisionServiceClient(connect.NewClient(connectinprocess.New(server)))

	_, err = decisions.SetSource(signedIn(t, store), setSource(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA,
		func(*consolev1.SetSourceRequest) {}))

	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodePermissionDenied)
	c.Assert(err, qt.ErrorMatches, ".*--read-only.*")
	c.Assert(sourceOf(t, built, consolev1.ResourceKind_RESOURCE_KIND_SCHEMA).OCIRef, qt.Equals, oldReference)
}

func auditLines(t *testing.T, log *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(log.Bytes()))
	for decoder.More() {
		line := make(map[string]any)
		qt.New(t).Assert(decoder.Decode(&line), qt.IsNil)
		lines = append(lines, line)
	}
	return lines
}

// A check that gets no answer stored nothing, whatever became of it, and
// says so rather than asking the person to look for a change.
func TestACheckWithoutAnAnswerStoredNothing(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := setSourceService(t, func(context.Context, client.WithWatch, client.Object, int) error {
		return errors.New("read tcp 127.0.0.1:52144->127.0.0.1:6443: connection reset by peer")
	}, sourced(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA))

	response, err := service.client.SetSource(service.ctx, setSource(consolev1.ResourceKind_RESOURCE_KIND_SCHEMA,
		func(r *consolev1.SetSourceRequest) { r.DryRun = true }))

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_UNKNOWN)
	c.Assert(response.GetMessage(), qt.Equals, "The API server did not answer the check. Nothing was stored; check again.")
}
