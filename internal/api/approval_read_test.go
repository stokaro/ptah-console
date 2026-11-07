package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect/v2"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"github.com/stokaro/ptah-operator/pkg/planread"
	"github.com/stokaro/ptah-operator/pkg/planread/planreadtest"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/audit"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// reviewedOf is what a page that read the schema's current plan sends. A
// state whose status names no plan gets references of a plan reviewed earlier.
func reviewedOf(objects []client.Object) *consolev1.ApproveSchemaPlanRequest {
	request := &consolev1.ApproveSchemaPlanRequest{
		Namespace: planreadtest.Namespace, SchemaName: "storefront",
		PlanName: "reviewed-earlier", PlanUid: "uid-reviewed-earlier", PlanFingerprint: "sha256:reviewed-earlier",
	}
	plans := make(map[string]*operatorv1alpha1.PtahSchemaPlan)
	for _, object := range objects {
		if plan, ok := object.(*operatorv1alpha1.PtahSchemaPlan); ok {
			plans[plan.Name] = plan
		}
	}
	for _, object := range objects {
		schema, ok := object.(*operatorv1alpha1.PtahSchema)
		if !ok || schema.Name != request.GetSchemaName() {
			continue
		}
		request.SchemaUid = string(schema.UID)
		if current := schema.Status.Plan; current != nil {
			request.PlanName, request.PlanUid, request.PlanFingerprint = current.Name, string(current.UID), current.Fingerprint
			if plan := plans[current.Name]; plan != nil {
				request.ContractVersion = plan.Spec.ContractVersion
			}
		}
	}
	return request
}

// approvalEnd is how an approval ends.
type approvalEnd struct {
	outcome consolev1.DecisionOutcome
	audited string
	says    string
	sent    int
}

// endFor is how an approval ends for each answer the operator's plan reader
// gives about the current plan.
func endFor(want planreadtest.Want) approvalEnd {
	switch {
	case want.Err == nil:
		return approvalEnd{outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, audited: audit.Admitted, sent: 1}
	case errors.Is(want.Err, planread.ErrNoPlan):
		return approvalEnd{outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, audited: audit.PlanChanged, says: "Nothing was approved"}
	case errors.Is(want.Err, planread.ErrUnsupported):
		return approvalEnd{outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, audited: audit.Refused, says: "does not read"}
	default:
		return approvalEnd{outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, audited: audit.Refused, says: "failed verification"}
	}
}

// approvalCases are the operator's cases an approval can meet: the current plan
// of a schema that exists.
func approvalCases(t *testing.T) []planreadtest.Case {
	t.Helper()
	var cases []planreadtest.Case
	for _, test := range planreadtest.Cases(t) {
		if test.Selection == planread.Current && !errors.Is(test.Want.Err, planread.ErrSchemaNotFound) {
			cases = append(cases, test)
		}
	}
	return cases
}

// answering counts the cases the plan reader answers with an error, or with a
// plan when want is nil.
func answering(cases []planreadtest.Case, want error) int {
	count := 0
	for _, test := range cases {
		if errors.Is(test.Want.Err, want) {
			count++
		}
	}
	return count
}

// An approval goes out only for a plan the operator's own reader read and
// verified, for every stored state that reader has a case for. The corpus is
// the operator's: a state it adds later is held here without a change.
func TestAnApprovalWaitsForThePlanRead(t *testing.T) {
	t.Parallel()
	cases := approvalCases(t)
	// A corpus that lost its readable or its refused plans would pass by having
	// nothing left to hold, or by holding everything.
	for _, want := range []error{nil, planread.ErrIntegrity, planread.ErrUnsupported, planread.ErrNoPlan} {
		qt.New(t).Assert(answering(cases, want) > 0, qt.IsTrue, qt.Commentf("no case answers %v", want))
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			want := endFor(test.Want)
			service := decisionService(t, admit("alice"), test.Objects...)

			response, err := service.client.ApproveSchemaPlan(service.ctx, reviewedOf(test.Objects))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetResult().GetOutcome(), qt.Equals, want.outcome, qt.Commentf("%s", response.GetResult().GetMessage()))
			c.Assert(response.GetResult().GetMessage(), qt.Contains, want.says)
			c.Assert(service.sent(), qt.HasLen, want.sent)
			lines := service.auditLines(t)
			c.Assert(lines, qt.HasLen, 1)
			c.Assert(lines[0]["outcome"], qt.Equals, want.audited)
			c.Assert(lines[0]["reason"], qt.Equals, response.GetResult().GetMessage())
		})
	}
}

// storedPlan is a readable plan waiting for an approval, as the operator
// stores it.
func storedPlan(t *testing.T) []client.Object {
	t.Helper()
	lab := planreadtest.NewLab(t, "storefront")
	plan, _ := lab.Publish(t, []string{"CREATE TABLE customers (id BIGINT PRIMARY KEY)"}, "v1")
	lab.SetCurrent(t, plan)
	return lab.Objects(t)
}

// planWithScope is the plan an executor of Ptah v0.19.0 or later writes: a
// document at the same contract version with a field this build does not
// declare. The contract check passes, and the plan reader refuses it.
func planWithScope(t *testing.T) []client.Object {
	t.Helper()
	decoded := make(map[string]any)
	qt.New(t).Assert(json.Unmarshal(planreadtest.Document(t, []string{"CREATE TABLE customers (id BIGINT PRIMARY KEY)"}), &decoded), qt.IsNil)
	decoded["scope"] = map[string]any{"schemas": []string{"public"}}
	document, err := json.Marshal(decoded)
	qt.New(t).Assert(err, qt.IsNil)
	lab := planreadtest.NewLab(t, "storefront")
	lab.SetCurrent(t, lab.PublishDocument(t, document, "v1"))
	return lab.Objects(t)
}

// failReading answers the read of one kind the way the API server does.
func failReading[T client.Object](answer error) func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
	return func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
		if _, ok := object.(T); ok {
			return answer
		}
		return c.Get(ctx, key, object, options...)
	}
}

func forbiddenRead(resource string) error {
	return apierrors.NewForbidden(operatorv1alpha1.GroupVersion.WithResource(resource).GroupResource(), "stored", errors.New("no rule"))
}

func failedRead() error {
	return &apierrors.StatusError{ErrStatus: metav1.Status{
		Status: metav1.StatusFailure, Code: http.StatusInternalServerError, Reason: metav1.StatusReasonInternalError,
		Message: "etcdserver: request timed out",
	}}
}

// What the operator's cases leave out: a read the API server refused or failed,
// and the document operator 0.2.10's executor writes. Each is told apart, none
// sends anything, and each leaves its refusal in the audit.
func TestAnApprovalOfAPlanThatFailedToLoadIsRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		objects func(*testing.T) []client.Object
		get     func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error
		says    string
	}{
		{
			name: "a document at the same contract with a field this build does not declare", objects: planWithScope,
			says: "does not read",
		},
		{
			name: "the identity may not read the chunks", objects: storedPlan,
			get:  failReading[*operatorv1alpha1.PtahSchemaPlanChunk](forbiddenRead("ptahschemaplanchunks")),
			says: "may not read",
		},
		{
			name: "the identity may not read the plan", objects: storedPlan,
			get:  failReading[*operatorv1alpha1.PtahSchemaPlan](forbiddenRead("ptahschemaplans")),
			says: "may not read",
		},
		{
			name: "the API server fails a chunk read", objects: storedPlan,
			get:  failReading[*operatorv1alpha1.PtahSchemaPlanChunk](failedRead()),
			says: "could not read",
		},
		{
			name: "the API server fails the plan read", objects: storedPlan,
			get:  failReading[*operatorv1alpha1.PtahSchemaPlan](failedRead()),
			says: "could not read",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			objects := test.objects(t)
			service := decisionServiceReading(t, test.get, admit("alice"), objects...)

			response, err := service.client.ApproveSchemaPlan(service.ctx, reviewedOf(objects))

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED,
				qt.Commentf("%s", response.GetResult().GetMessage()))
			c.Assert(response.GetResult().GetMessage(), qt.Contains, test.says)
			c.Assert(response.GetResult().GetMessage(), qt.Contains, "Nothing was approved")
			c.Assert(service.sent(), qt.HasLen, 0)
			lines := service.auditLines(t)
			c.Assert(lines, qt.HasLen, 1)
			c.Assert(lines[0]["outcome"], qt.Equals, audit.Refused)
			c.Assert(lines[0]["action"], qt.Equals, "approve-schema-plan")
			c.Assert(lines[0]["reason"], qt.Equals, response.GetResult().GetMessage())
		})
	}
}

// schemaNamesAtFirst answers the first read of a schema as if its status still
// named the plan the request names, and every later read as stored.
func schemaNamesAtFirst(reads *atomic.Int32, request *consolev1.ApproveSchemaPlanRequest) func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
	return func(ctx context.Context, w client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
		if err := w.Get(ctx, key, object, options...); err != nil {
			return err
		}
		schema, ok := object.(*operatorv1alpha1.PtahSchema)
		if ok && reads.Add(1) == 1 {
			schema.Status.Plan = &operatorv1alpha1.CurrentPlanStatus{
				Name: request.GetPlanName(), UID: types.UID(request.GetPlanUid()), Fingerprint: request.GetPlanFingerprint(),
			}
		}
		return nil
	}
}

// The schema's status can move between the read that compares it with the
// request and the plan reader's own read. What the reader verified is then
// another plan, and the plan the request names was not verified: nothing goes
// out for it.
func TestAPlanVerifiedInPlaceOfTheOneReviewedIsNotApproved(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	lab := planreadtest.NewLab(t, "storefront")
	reviewedPlan, _ := lab.Publish(t, []string{"CREATE TABLE customers (id BIGINT PRIMARY KEY)"}, "v1")
	lab.SetCurrent(t, reviewedPlan)
	objects := lab.Objects(t)
	request := reviewedOf(objects)
	newer, _ := lab.Publish(t, []string{"ALTER TABLE customers ADD COLUMN email TEXT"}, "v2")
	lab.SetCurrent(t, newer)

	var schemaReads atomic.Int32
	service := decisionServiceReading(t, schemaNamesAtFirst(&schemaReads, request), admit("alice"), lab.Objects(t)...)

	response, err := service.client.ApproveSchemaPlan(service.ctx, request)

	c.Assert(err, qt.IsNil)
	c.Assert(schemaReads.Load(), qt.Equals, int32(2))
	c.Assert(response.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED,
		qt.Commentf("%s", response.GetResult().GetMessage()))
	c.Assert(service.sent(), qt.HasLen, 0)
}

// A plan the schema still names and the store no longer holds is not a plan
// that failed a check: there is nothing there to review, and the person reads
// the plan that comes next.
func TestAnApprovalOfAPlanNoLongerStoredIsChanged(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	lab := planreadtest.NewLab(t, "storefront")
	plan, _ := lab.Publish(t, []string{"CREATE TABLE customers (id BIGINT PRIMARY KEY)"}, "v1")
	lab.SetCurrent(t, plan)
	request := reviewedOf(lab.Objects(t))
	lab.DeletePlan(t, plan)
	service := decisionService(t, admit("alice"), lab.Objects(t)...)

	response, err := service.client.ApproveSchemaPlan(service.ctx, request)

	c.Assert(err, qt.IsNil)
	c.Assert(response.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED)
	c.Assert(response.GetResult().GetMessage(), qt.Contains, "no longer stored")
	c.Assert(service.sent(), qt.HasLen, 0)
	lines := service.auditLines(t)
	c.Assert(lines, qt.HasLen, 1)
	c.Assert(lines[0]["outcome"], qt.Equals, audit.PlanChanged)
}

// A failed read of the owner stays an error the browser can read, and is
// written down: nothing was sent and the person was told so.
func TestAFailedReadOfTheOwnerIsWrittenDown(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	objects := storedPlan(t)
	service := decisionServiceReading(t, failReading[*operatorv1alpha1.PtahSchema](failedRead()), admit("alice"), objects...)

	_, err := service.client.ApproveSchemaPlan(service.ctx, reviewedOf(objects))

	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeUnavailable)
	c.Assert(service.sent(), qt.HasLen, 0)
	lines := service.auditLines(t)
	c.Assert(lines, qt.HasLen, 1)
	c.Assert(lines[0]["outcome"], qt.Equals, audit.Failed)
	c.Assert(lines[0]["action"], qt.Equals, "approve-schema-plan")
}

// waitingMigrationEdited is waitingMigration with its current plan edited.
func waitingMigrationEdited(edit func(*operatorv1alpha1.PtahMigrationPlan)) []client.Object {
	objects := waitingMigration()
	for _, object := range objects {
		if plan, ok := object.(*operatorv1alpha1.PtahMigrationPlan); ok && plan.Name == "billing-plan-3" {
			edit(plan)
		}
	}
	return objects
}

// waitingMigrationWithout is waitingMigration without the object of that name.
func waitingMigrationWithout(name string) []client.Object {
	var kept []client.Object
	for _, object := range waitingMigration() {
		if object.GetName() != name {
			kept = append(kept, object)
		}
	}
	return kept
}

// A migration plan is read as the plan object it is: there is no SQL and no
// chunk to verify, so the states of a schema plan do not arise. The plan page
// refuses a plan bound to another migration, a read that fails and a read that
// is forbidden, and the approval holds to the same line. The plan that passes
// is the control: the refusals are not a refusal of every plan.
func TestAnApprovalOfAMigrationPlanWaitsForItsRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		objects []client.Object
		get     func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error
		want    consolev1.DecisionOutcome
		audited string
		says    string
		sent    int
	}{
		{
			name: "a plan the console read", objects: waitingMigration(),
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, audited: audit.Admitted, sent: 1,
		},
		{
			name: "a plan of another migration",
			objects: waitingMigrationEdited(func(plan *operatorv1alpha1.PtahMigrationPlan) {
				plan.Spec.MigrationRef = operatorv1alpha1.ImmutableObjectReference{Name: "billing", UID: "uid-earlier-migration"}
			}),
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, audited: audit.Refused, says: "another migration",
		},
		{
			name: "a plan written under a contract this console does not know",
			objects: waitingMigrationEdited(func(plan *operatorv1alpha1.PtahMigrationPlan) {
				plan.Spec.ContractVersion = sourceContract + 1
			}),
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, audited: audit.Refused, says: "does not read",
		},
		{
			name: "the identity may not read the plan", objects: waitingMigration(),
			get:  failReading[*operatorv1alpha1.PtahMigrationPlan](forbiddenRead("ptahmigrationplans")),
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, audited: audit.Refused, says: "may not read",
		},
		{
			name: "the API server fails the plan read", objects: waitingMigration(),
			get:  failReading[*operatorv1alpha1.PtahMigrationPlan](failedRead()),
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, audited: audit.Refused, says: "could not read",
		},
		{
			name: "the plan is gone", objects: waitingMigrationWithout("billing-plan-3"),
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, audited: audit.PlanChanged, says: "no longer stored",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := decisionServiceReading(t, test.get, admit("alice"), test.objects...)

			response, err := service.client.ApproveMigrationPlan(service.ctx, &consolev1.ApproveMigrationPlanRequest{
				Namespace: namespace, MigrationName: "billing", MigrationUid: "uid-migration",
				PlanName: "billing-plan-3", PlanUid: "uid-migration-plan", PlanFingerprint: "sha256:bb",
			})

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetResult().GetOutcome(), qt.Equals, test.want, qt.Commentf("%s", response.GetResult().GetMessage()))
			c.Assert(response.GetResult().GetMessage(), qt.Contains, test.says)
			c.Assert(service.sent(), qt.HasLen, test.sent)
			lines := service.auditLines(t)
			c.Assert(lines, qt.HasLen, 1)
			c.Assert(lines[0]["action"], qt.Equals, "approve-migration-plan")
			c.Assert(lines[0]["outcome"], qt.Equals, test.audited)
		})
	}
}
