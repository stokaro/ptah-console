package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
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

const namespace = "application"

// create answers one create the way the API server and admission would.
type create func(ctx context.Context, c client.WithWatch, object client.Object) error

// admit stores the decision the way the operator's webhook stamps it.
func admit(username string) create {
	return func(ctx context.Context, c client.WithWatch, object client.Object) error {
		stampDecision(object, username)
		return c.Create(ctx, object)
	}
}

func stampDecision(object client.Object, username string) {
	identity := operatorv1alpha1.ApprovalIdentity{Username: username, Groups: []string{"system:authenticated"}}
	switch typed := object.(type) {
	case *operatorv1alpha1.PtahSchemaApproval:
		typed.Spec.Approver, typed.Spec.ApprovedAt, typed.Spec.MutationRequestUID = identity, metav1.Now(), "uid-request"
	case *operatorv1alpha1.PtahMigrationApproval:
		typed.Spec.Approver, typed.Spec.ApprovedAt, typed.Spec.MutationRequestUID = identity, metav1.Now(), "uid-request"
	case *operatorv1alpha1.PtahMigrationRunAcknowledgment:
		typed.Spec.AcknowledgedBy, typed.Spec.AcknowledgedAt, typed.Spec.MutationRequestUID = identity, metav1.Now(), "uid-request"
	}
}

// refuse answers with an API status and stores nothing.
func refuse(code int32, reason metav1.StatusReason, message string) create {
	return func(context.Context, client.WithWatch, client.Object) error {
		return &apierrors.StatusError{ErrStatus: metav1.Status{Status: metav1.StatusFailure, Code: code, Reason: reason, Message: message}}
	}
}

// storeThenDrop stores the decision and loses the answer.
func storeThenDrop(username string) create {
	return func(ctx context.Context, c client.WithWatch, object client.Object) error {
		stampDecision(object, username)
		if err := c.Create(ctx, object); err != nil {
			return err
		}
		return errors.New("read tcp 127.0.0.1:52144->127.0.0.1:6443: connection reset by peer")
	}
}

// squat stores another approval under the name first, as someone who
// guessed it would.
func squat(username string) create {
	return func(ctx context.Context, c client.WithWatch, object client.Object) error {
		other := object.DeepCopyObject().(*operatorv1alpha1.PtahSchemaApproval)
		other.Spec.PlanFingerprint = "sha256:ff"
		stampDecision(other, username)
		if err := c.Create(ctx, other); err != nil {
			return err
		}
		return apierrors.NewAlreadyExists(operatorv1alpha1.GroupVersion.WithResource("ptahschemaapprovals").GroupResource(), other.Name)
	}
}

// decisions is the service under test with what it sent and wrote down.
type decisions struct {
	client  consolev1connect.DecisionServiceClient
	ctx     context.Context
	store   client.Client
	audit   *bytes.Buffer
	mu      *sync.Mutex
	creates *[]client.Object
}

func (d decisions) sent() []client.Object {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]client.Object(nil), *d.creates...)
}

// auditLines decodes every audit line written.
func (d decisions) auditLines(t *testing.T) []map[string]any {
	t.Helper()
	var lines []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(d.audit.Bytes()))
	for decoder.More() {
		line := make(map[string]any)
		qt.New(t).Assert(decoder.Decode(&line), qt.IsNil)
		lines = append(lines, line)
	}
	return lines
}

func decisionService(t *testing.T, answer create, objects ...client.Object) decisions {
	t.Helper()
	var mu sync.Mutex
	var creates []client.Object
	scheme, err := kube.Scheme()
	qt.New(t).Assert(err, qt.IsNil)
	built := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, object client.Object, _ ...client.CreateOption) error {
			mu.Lock()
			creates = append(creates, object.DeepCopyObject().(client.Object))
			mu.Unlock()
			return answer(ctx, c, object)
		},
	}).Build()
	log := &bytes.Buffer{}
	store := session.NewStore(session.Options{})
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterDecisionServiceHandler(server,
		api.NewDecisionService(staticFactory{built}, api.Scope{}, decision.NewLedger(), audit.New(log)))
	return decisions{
		client: consolev1connect.NewDecisionServiceClient(connect.NewClient(connectinprocess.New(server))),
		ctx:    signedIn(t, store), store: built, audit: log, mu: &mu, creates: &creates,
	}
}

// waitingSchema is a schema whose current plan waits for approval.
func waitingSchema(contract int32) []client.Object {
	return []client.Object{
		&operatorv1alpha1.PtahSchema{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "storefront", UID: "uid-schema"},
			Status: operatorv1alpha1.PtahSchemaStatus{Plan: &operatorv1alpha1.CurrentPlanStatus{
				Name: "storefront-plan-1", UID: "uid-plan", Fingerprint: "sha256:aa",
			}},
		},
		&operatorv1alpha1.PtahSchemaPlan{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "storefront-plan-1", UID: "uid-plan"},
			Spec:       operatorv1alpha1.PtahSchemaPlanSpec{ContractVersion: contract, Fingerprint: "sha256:aa"},
		},
	}
}

// The plan contracts the console approves: 1, a plan computed from an OCI
// artifact, and 2, one computed from a ConfigMap or Git source.
const (
	ociContract    int32 = 1
	sourceContract int32 = 2
)

func reviewed() *consolev1.ApproveSchemaPlanRequest {
	return &consolev1.ApproveSchemaPlanRequest{
		Namespace: namespace, SchemaName: "storefront", SchemaUid: "uid-schema",
		PlanName: "storefront-plan-1", PlanUid: "uid-plan", PlanFingerprint: "sha256:aa",
		ContractVersion: ociContract,
	}
}

const rbacMessage = `ptahschemaapprovals.operator.ptah.run "ptah-console-0123" is forbidden: ` +
	`User "author" cannot create resource "ptahschemaapprovals" in API group "operator.ptah.run" in the namespace "application"`

const admissionMessage = `admission webhook "vptahschemaapproval.operator.ptah.run" denied the request: ` +
	`the approver "author" may not approve a plan they cannot be distinct from`

// Every way an approval ends is reported as what it is, and only an approval
// of the plan the person reviewed is ever sent.
func TestApproveSchemaPlanReportsEachOutcome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		contract int32
		request  func(*consolev1.ApproveSchemaPlanRequest)
		answer   create
		want     consolev1.DecisionOutcome
		message  string
		sent     int
	}{
		{
			name: "admitted", contract: ociContract, request: func(*consolev1.ApproveSchemaPlanRequest) {},
			answer: admit("alice"), want: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, message: "", sent: 1,
		},
		{
			name: "a newer plan replaced the reviewed one", contract: ociContract,
			request: func(r *consolev1.ApproveSchemaPlanRequest) { r.PlanFingerprint = "sha256:00" },
			answer:  admit("alice"), want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, message: "A newer plan replaced", sent: 0,
		},
		{
			name: "the schema was created again", contract: ociContract,
			request: func(r *consolev1.ApproveSchemaPlanRequest) { r.SchemaUid = "uid-earlier-schema" },
			answer:  admit("alice"), want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, message: "deleted and created again", sent: 0,
		},
		{
			name: "a plan computed from a ConfigMap or Git source", contract: sourceContract,
			request: func(r *consolev1.ApproveSchemaPlanRequest) { r.ContractVersion = sourceContract },
			answer:  admit("alice"), want: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, message: "", sent: 1,
		},
		{
			name: "a plan written under a contract this console does not know", contract: sourceContract + 1,
			request: func(r *consolev1.ApproveSchemaPlanRequest) { r.ContractVersion = sourceContract + 1 },
			answer:  admit("alice"), want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, message: "contract", sent: 0,
		},
		{
			name: "reviewed under another contract", contract: ociContract,
			request: func(r *consolev1.ApproveSchemaPlanRequest) { r.ContractVersion = 0 },
			answer:  admit("alice"), want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, message: "contract", sent: 0,
		},
		{
			name: "RBAC refuses the identity", contract: ociContract, request: func(*consolev1.ApproveSchemaPlanRequest) {},
			answer: refuse(http.StatusForbidden, metav1.StatusReasonForbidden, rbacMessage),
			want:   consolev1.DecisionOutcome_DECISION_OUTCOME_FORBIDDEN, message: rbacMessage, sent: 1,
		},
		{
			name: "admission refuses the decision", contract: ociContract, request: func(*consolev1.ApproveSchemaPlanRequest) {},
			answer: refuse(http.StatusForbidden, metav1.StatusReasonForbidden, admissionMessage),
			want:   consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, message: admissionMessage, sent: 1,
		},
		{
			name: "the schema refuses the decision", contract: ociContract, request: func(*consolev1.ApproveSchemaPlanRequest) {},
			answer: refuse(http.StatusUnprocessableEntity, metav1.StatusReasonInvalid, "spec.planFingerprint: Invalid value"),
			want:   consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, message: "spec.planFingerprint: Invalid value", sent: 1,
		},
		{
			name: "no answer in time", contract: ociContract, request: func(*consolev1.ApproveSchemaPlanRequest) {},
			answer: func(context.Context, client.WithWatch, client.Object) error { return context.DeadlineExceeded },
			want:   consolev1.DecisionOutcome_DECISION_OUTCOME_UNKNOWN, message: "did not answer in time", sent: 1,
		},
		{
			name: "the API server failed", contract: ociContract, request: func(*consolev1.ApproveSchemaPlanRequest) {},
			answer: refuse(http.StatusInternalServerError, metav1.StatusReasonInternalError, "etcdserver: request timed out"),
			want:   consolev1.DecisionOutcome_DECISION_OUTCOME_UNKNOWN, message: "may have been stored", sent: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := decisionService(t, test.answer, waitingSchema(test.contract)...)
			request := reviewed()
			test.request(request)

			response, err := service.client.ApproveSchemaPlan(service.ctx, request)

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetResult().GetOutcome(), qt.Equals, test.want)
			c.Assert(response.GetResult().GetMessage(), qt.Contains, test.message)
			c.Assert(service.sent(), qt.HasLen, test.sent)
			lines := service.auditLines(t)
			c.Assert(lines, qt.HasLen, 1)
			c.Assert(lines[0]["action"], qt.Equals, "approve-schema-plan")
			c.Assert(lines[0]["planFingerprint"], qt.Equals, request.GetPlanFingerprint())
		})
	}
}

// The approval carries the references and the fingerprint and nothing else:
// who approved and when is admission's to write. What comes back is the
// approval as admission stamped it.
func TestAnApprovalSendsOnlyWhatItApproves(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := decisionService(t, admit("alice"), waitingSchema(ociContract)...)

	response, err := service.client.ApproveSchemaPlan(service.ctx, reviewed())

	c.Assert(err, qt.IsNil)
	sent := service.sent()
	c.Assert(sent, qt.HasLen, 1)
	approval := sent[0].(*operatorv1alpha1.PtahSchemaApproval)
	c.Assert(approval.Spec, qt.DeepEquals, operatorv1alpha1.PtahSchemaApprovalSpec{
		SchemaRef:       operatorv1alpha1.ImmutableObjectReference{Name: "storefront", UID: "uid-schema"},
		PlanRef:         operatorv1alpha1.ImmutableObjectReference{Name: "storefront-plan-1", UID: "uid-plan"},
		PlanFingerprint: "sha256:aa",
	})
	c.Assert(approval.Name, qt.Equals, response.GetResult().GetName())
	c.Assert(approval.Namespace, qt.Equals, namespace)
	decided := response.GetResult().GetDecision()
	c.Assert(decided.GetDecidedBy(), qt.Equals, "alice")
	c.Assert(decided.GetPlanFingerprint(), qt.Equals, "sha256:aa")
	c.Assert(service.auditLines(t)[0]["identity"], qt.Equals, "alice")
	c.Assert(service.auditLines(t)[0]["outcome"], qt.Equals, audit.Admitted)
}

// An answer lost after the API server stored the approval is unknown, and
// sending the same approval again finds the stored one instead of making a
// second.
func TestARetryAfterALostAnswerFindsTheStoredApproval(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := decisionService(t, storeThenDrop("alice"), waitingSchema(ociContract)...)

	first, err := service.client.ApproveSchemaPlan(service.ctx, reviewed())
	c.Assert(err, qt.IsNil)
	again, err := service.client.ApproveSchemaPlan(service.ctx, reviewed())
	c.Assert(err, qt.IsNil)
	readBack, err := service.client.GetDecision(service.ctx, &consolev1.GetDecisionRequest{
		Kind: consolev1.DecisionKind_DECISION_KIND_SCHEMA_APPROVAL, Namespace: namespace, Name: first.GetResult().GetName(),
	})
	c.Assert(err, qt.IsNil)

	c.Assert(first.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_UNKNOWN)
	c.Assert(again.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
	c.Assert(again.GetResult().GetName(), qt.Equals, first.GetResult().GetName())
	c.Assert(again.GetResult().GetDecision().GetDecidedBy(), qt.Equals, "alice")
	c.Assert(readBack.GetDecision().GetDecidedBy(), qt.Equals, "alice")
	approvals := &operatorv1alpha1.PtahSchemaApprovalList{}
	c.Assert(service.store.List(t.Context(), approvals), qt.IsNil)
	c.Assert(approvals.Items, qt.HasLen, 1)
}

// A name already holding some other decision is not taken for this one.
func TestANameHoldingAnotherDecisionIsNotThisOne(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := decisionService(t, squat("mallory"), waitingSchema(ociContract)...)

	response, err := service.client.ApproveSchemaPlan(service.ctx, reviewed())

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED)
	c.Assert(response.GetResult().GetDecision(), qt.IsNil)
}

// A request that leaves out what pins it to the reviewed plan is refused
// before anything is read.
func TestAnApprovalMustNameWhatWasReviewed(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := decisionService(t, admit("alice"), waitingSchema(ociContract)...)
	request := reviewed()
	request.PlanFingerprint = ""

	_, err := service.client.ApproveSchemaPlan(service.ctx, request)

	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeInvalidArgument)
	c.Assert(err, qt.ErrorMatches, ".*plan_fingerprint is required")
	c.Assert(service.sent(), qt.HasLen, 0)
}

func waitingMigration() []client.Object {
	return []client.Object{
		&operatorv1alpha1.PtahMigration{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "billing", UID: "uid-migration"},
			Status: operatorv1alpha1.PtahMigrationStatus{
				Plan: &operatorv1alpha1.ImmutableObjectReference{Name: "billing-plan-3", UID: "uid-migration-plan"},
			},
		},
		&operatorv1alpha1.PtahMigrationPlan{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "billing-plan-3", UID: "uid-migration-plan"},
			Spec:       operatorv1alpha1.PtahMigrationPlanSpec{Fingerprint: "sha256:bb"},
		},
		// The plan before it, still stored and no longer the migration's.
		&operatorv1alpha1.PtahMigrationPlan{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "billing-plan-2", UID: "uid-older-plan"},
			Spec:       operatorv1alpha1.PtahMigrationPlanSpec{Fingerprint: "sha256:b2"},
		},
	}
}

// A migration approval is sent only for the plan the migration still names,
// with the fingerprint that plan carries.
func TestApproveMigrationPlanApprovesOnlyTheCurrentPlan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		planName    string
		planUID     string
		fingerprint string
		want        consolev1.DecisionOutcome
		sent        int
	}{
		{
			name: "the current plan", planName: "billing-plan-3", planUID: "uid-migration-plan", fingerprint: "sha256:bb",
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, sent: 1,
		},
		{
			name: "another fingerprint", planName: "billing-plan-3", planUID: "uid-migration-plan", fingerprint: "sha256:00",
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, sent: 0,
		},
		{
			name: "the plan was created again under its name", planName: "billing-plan-3", planUID: "uid-earlier-plan", fingerprint: "sha256:bb",
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, sent: 0,
		},
		{
			name: "a stored plan the migration no longer names", planName: "billing-plan-2", planUID: "uid-older-plan", fingerprint: "sha256:b2",
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, sent: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := decisionService(t, admit("alice"), waitingMigration()...)

			response, err := service.client.ApproveMigrationPlan(service.ctx, &consolev1.ApproveMigrationPlanRequest{
				Namespace: namespace, MigrationName: "billing", MigrationUid: "uid-migration",
				PlanName: test.planName, PlanUid: test.planUID, PlanFingerprint: test.fingerprint,
			})

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetResult().GetOutcome(), qt.Equals, test.want)
			c.Assert(service.sent(), qt.HasLen, test.sent)
		})
	}
}

func unresolvedMigration(unresolved, resolved string) *operatorv1alpha1.PtahMigration {
	migration := &operatorv1alpha1.PtahMigration{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "billing", UID: "uid-migration"},
		Status: operatorv1alpha1.PtahMigrationStatus{
			UnresolvedRun: &operatorv1alpha1.UnresolvedMigrationRunStatus{OperationID: unresolved},
			ResolvedRun:   &operatorv1alpha1.ResolvedMigrationRunStatus{OperationID: resolved},
		},
	}
	return migration
}

// An acknowledgment settles the run the person looked at, and is not sent
// once that run is no longer the one waiting.
func TestAcknowledgeMigrationRunSettlesOnlyTheWaitingRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		migration *operatorv1alpha1.PtahMigration
		want      consolev1.DecisionOutcome
		message   string
		sent      int
	}{
		{name: "the waiting run", migration: unresolvedMigration("op-2", "op-1"), want: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, message: "", sent: 1},
		{name: "another run waits", migration: unresolvedMigration("op-3", "op-2"), want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, message: "no longer waiting", sent: 0},
		{name: "the run was settled", migration: unresolvedMigration("op-2", "op-2"), want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, message: "settled already", sent: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := decisionService(t, admit("oncall"), test.migration)

			response, err := service.client.AcknowledgeMigrationRun(service.ctx, &consolev1.AcknowledgeMigrationRunRequest{
				Namespace: namespace, MigrationName: "billing", MigrationUid: "uid-migration", OperationId: "op-2",
			})

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetResult().GetOutcome(), qt.Equals, test.want)
			c.Assert(response.GetResult().GetMessage(), qt.Contains, test.message)
			c.Assert(service.sent(), qt.HasLen, test.sent)
		})
	}
}

// The acknowledgment names the migration and the operation, and nothing
// about who acknowledged.
func TestAnAcknowledgmentSendsOnlyTheRun(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := decisionService(t, admit("oncall"), unresolvedMigration("op-2", ""))

	response, err := service.client.AcknowledgeMigrationRun(service.ctx, &consolev1.AcknowledgeMigrationRunRequest{
		Namespace: namespace, MigrationName: "billing", MigrationUid: "uid-migration", OperationId: "op-2",
	})

	c.Assert(err, qt.IsNil)
	sent := service.sent()
	c.Assert(sent, qt.HasLen, 1)
	c.Assert(sent[0].(*operatorv1alpha1.PtahMigrationRunAcknowledgment).Spec, qt.DeepEquals, operatorv1alpha1.PtahMigrationRunAcknowledgmentSpec{
		MigrationRef: operatorv1alpha1.ImmutableObjectReference{Name: "billing", UID: "uid-migration"},
		OperationID:  "op-2",
	})
	c.Assert(response.GetResult().GetDecision().GetDecidedBy(), qt.Equals, "oncall")
	c.Assert(response.GetResult().GetDecision().GetOperationId(), qt.Equals, "op-2")
}

// Decisions of every kind are listed, each mapped from what admission
// stamped.
func TestListDecisionsListsEveryKind(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	stamped := func(object client.Object, username string) client.Object {
		stampDecision(object, username)
		return object
	}
	service := decisionService(t, admit("alice"),
		stamped(&operatorv1alpha1.PtahSchemaApproval{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "a"}}, "alice"),
		stamped(&operatorv1alpha1.PtahMigrationApproval{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "b"}}, "bob"),
		stamped(&operatorv1alpha1.PtahMigrationRunAcknowledgment{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "c"}}, "oncall"),
	)

	response, err := service.client.ListDecisions(service.ctx, &consolev1.ListDecisionsRequest{Namespaces: []string{namespace}})

	c.Assert(err, qt.IsNil)
	var who []string
	for _, d := range response.GetDecisions() {
		who = append(who, d.GetKind().String()+" "+d.GetDecidedBy())
	}
	c.Assert(who, qt.ContentEquals, []string{
		"DECISION_KIND_SCHEMA_APPROVAL alice",
		"DECISION_KIND_MIGRATION_APPROVAL bob",
		"DECISION_KIND_RUN_ACKNOWLEDGMENT oncall",
	})
}

// An approval says whether the plan it approved is still stored under the
// UID it recorded: an auditor reads "not retained", never an empty plan.
func TestADecisionSaysWhetherItsPlanIsKept(t *testing.T) {
	t.Parallel()
	// Each row builds its own objects: the fake client writes a resource
	// version into what it is given, and rows run in parallel.
	approval := func() client.Object {
		return &operatorv1alpha1.PtahSchemaApproval{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "approval"},
			Spec: operatorv1alpha1.PtahSchemaApprovalSpec{
				SchemaRef: operatorv1alpha1.ImmutableObjectReference{Name: "storefront", UID: "uid-schema"},
				PlanRef:   operatorv1alpha1.ImmutableObjectReference{Name: "storefront-plan-1", UID: "uid-plan"},
			},
		}
	}
	plan := func(uid string) client.Object {
		return &operatorv1alpha1.PtahSchemaPlan{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "storefront-plan-1", UID: types.UID(uid)}}
	}
	tests := []struct {
		name    string
		objects func() []client.Object
		get     func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error
		want    consolev1.PlanRetention
	}{
		{name: "the plan is stored", objects: func() []client.Object { return []client.Object{approval(), plan("uid-plan")} }, want: consolev1.PlanRetention_PLAN_RETENTION_RETAINED},
		{name: "the plan is gone", objects: func() []client.Object { return []client.Object{approval()} }, want: consolev1.PlanRetention_PLAN_RETENTION_NOT_RETAINED},
		{name: "another plan holds the name", objects: func() []client.Object { return []client.Object{approval(), plan("uid-later-plan")} }, want: consolev1.PlanRetention_PLAN_RETENTION_NOT_RETAINED},
		{
			name: "the identity may not read plans", objects: func() []client.Object { return []client.Object{approval(), plan("uid-plan")} },
			get:  forbidPlans,
			want: consolev1.PlanRetention_PLAN_RETENTION_NOT_GRANTED,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := decisionsWithGet(t, test.get, test.objects()...)

			listed, err := service.client.ListDecisions(service.ctx, &consolev1.ListDecisionsRequest{Namespaces: []string{namespace}})
			c.Assert(err, qt.IsNil)
			read, err := service.client.GetDecision(service.ctx, &consolev1.GetDecisionRequest{
				Kind: consolev1.DecisionKind_DECISION_KIND_SCHEMA_APPROVAL, Namespace: namespace, Name: "approval",
			})
			c.Assert(err, qt.IsNil)

			c.Assert(listed.GetDecisions(), qt.HasLen, 1)
			c.Assert(listed.GetDecisions()[0].GetPlanRetention(), qt.Equals, test.want)
			c.Assert(read.GetDecision().GetPlanRetention(), qt.Equals, test.want)
		})
	}
}

func forbidPlans(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	if _, ok := object.(*operatorv1alpha1.PtahSchemaPlan); ok {
		return apierrors.NewForbidden(operatorv1alpha1.GroupVersion.WithResource("ptahschemaplans").GroupResource(), key.Name, errors.New("no rule"))
	}
	return c.Get(ctx, key, object, options...)
}

// decisionsWithGet is the decision service over objects, with an optional
// answer to every Get.
func decisionsWithGet(t *testing.T, get func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error, objects ...client.Object) decisions {
	t.Helper()
	scheme, err := kube.Scheme()
	qt.New(t).Assert(err, qt.IsNil)
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...)
	if get != nil {
		builder = builder.WithInterceptorFuncs(interceptor.Funcs{Get: get})
	}
	built := builder.Build()
	store := session.NewStore(session.Options{})
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterDecisionServiceHandler(server,
		api.NewDecisionService(staticFactory{built}, api.Scope{}, decision.NewLedger(), audit.Discard()))
	return decisions{
		client: consolev1connect.NewDecisionServiceClient(connect.NewClient(connectinprocess.New(server))),
		ctx:    signedIn(t, store), store: built,
	}
}
