package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"time"

	"connectrpc.com/connect/v2"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"github.com/stokaro/ptah-operator/pkg/planread"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/decision"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/model"
)

// fieldOwner is the field manager of the console's writes.
const fieldOwner = "ptah-console"

// submitTimeout bounds one write. A write that does not answer in time is
// reported as unknown, never as failed: it may have been stored.
const submitTimeout = 20 * time.Second

// DecisionService serves DecisionService.
type DecisionService struct {
	consolev1connect.UnimplementedDecisionServiceHandler

	factory kube.ClientFactory
	scope   Scope
	ledger  *decision.Ledger
	audit   *audit.Logger
}

// NewDecisionService returns the DecisionService.
func NewDecisionService(factory kube.ClientFactory, scope Scope, ledger *decision.Ledger, auditLog *audit.Logger) *DecisionService {
	if auditLog == nil {
		auditLog = audit.Discard()
	}
	return &DecisionService{factory: factory, scope: scope, ledger: ledger, audit: auditLog}
}

// ListDecisions lists the approvals and acknowledgments of each namespace.
func (d *DecisionService) ListDecisions(ctx context.Context, request *consolev1.ListDecisionsRequest) (*consolev1.ListDecisionsResponse, error) {
	decisions, results, err := listEach(ctx, d.factory, d.scope, request.GetNamespaces(), listDecisions, (*consolev1.Decision).GetRef)
	if err != nil {
		return nil, err
	}
	return &consolev1.ListDecisionsResponse{Decisions: decisions, Namespaces: results}, nil
}

func listDecisions(ctx context.Context, c client.Client, namespace string, bound int) ([]*consolev1.Decision, bool, error) {
	var out []*consolev1.Decision
	truncated, err := pages(ctx, c, namespace, bound,
		func() *operatorv1alpha1.PtahSchemaApprovalList { return &operatorv1alpha1.PtahSchemaApprovalList{} },
		func(page *operatorv1alpha1.PtahSchemaApprovalList) {
			for i := range page.Items {
				approval := model.SchemaApproval(&page.Items[i])
				approval.PlanRetention = planRetention(ctx, c, namespace, &operatorv1alpha1.PtahSchemaPlan{}, page.Items[i].Spec.PlanRef)
				out = append(out, approval)
			}
		})
	if err != nil {
		return nil, false, err
	}
	// A reader may list schema approvals and not the migration family; what
	// it may list still comes back.
	if more, err := pages(ctx, c, namespace, bound,
		func() *operatorv1alpha1.PtahMigrationApprovalList {
			return &operatorv1alpha1.PtahMigrationApprovalList{}
		},
		func(page *operatorv1alpha1.PtahMigrationApprovalList) {
			for i := range page.Items {
				approval := model.MigrationApproval(&page.Items[i])
				approval.PlanRetention = planRetention(ctx, c, namespace, &operatorv1alpha1.PtahMigrationPlan{}, page.Items[i].Spec.PlanRef)
				out = append(out, approval)
			}
		}); err == nil {
		truncated = truncated || more
	}
	if more, err := pages(ctx, c, namespace, bound,
		func() *operatorv1alpha1.PtahMigrationRunAcknowledgmentList {
			return &operatorv1alpha1.PtahMigrationRunAcknowledgmentList{}
		},
		func(page *operatorv1alpha1.PtahMigrationRunAcknowledgmentList) {
			for i := range page.Items {
				out = append(out, model.RunAcknowledgment(&page.Items[i]))
			}
		}); err == nil {
		truncated = truncated || more
	}
	return out, truncated, nil
}

// planRetention says whether the plan an approval names is still stored
// under the UID the approval recorded.
func planRetention(ctx context.Context, c client.Client, namespace string, plan client.Object, ref operatorv1alpha1.ImmutableObjectReference) consolev1.PlanRetention {
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, plan)
	switch {
	case err == nil && plan.GetUID() == ref.UID:
		return consolev1.PlanRetention_PLAN_RETENTION_RETAINED
	case err == nil, apierrors.IsNotFound(err):
		return consolev1.PlanRetention_PLAN_RETENTION_NOT_RETAINED
	case apierrors.IsForbidden(err):
		return consolev1.PlanRetention_PLAN_RETENTION_NOT_GRANTED
	default:
		return consolev1.PlanRetention_PLAN_RETENTION_UNKNOWN
	}
}

// GetDecision reads one decision back by kind and name.
func (d *DecisionService) GetDecision(ctx context.Context, request *consolev1.GetDecisionRequest) (*consolev1.GetDecisionResponse, error) {
	if err := d.scope.check([]string{request.GetNamespace()}); err != nil {
		return nil, err
	}
	c, _, err := actor(ctx, d.factory)
	if err != nil {
		return nil, err
	}
	found, err := readDecision(ctx, c, request.GetKind(), types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetName()})
	if err != nil {
		return nil, readError(err, "decision "+request.GetName())
	}
	return &consolev1.GetDecisionResponse{Decision: found}, nil
}

func readDecision(ctx context.Context, c client.Client, kind consolev1.DecisionKind, key types.NamespacedName) (*consolev1.Decision, error) {
	switch kind {
	case consolev1.DecisionKind_DECISION_KIND_SCHEMA_APPROVAL:
		object := &operatorv1alpha1.PtahSchemaApproval{}
		if err := c.Get(ctx, key, object); err != nil {
			return nil, err
		}
		read := model.SchemaApproval(object)
		read.PlanRetention = planRetention(ctx, c, key.Namespace, &operatorv1alpha1.PtahSchemaPlan{}, object.Spec.PlanRef)
		return read, nil
	case consolev1.DecisionKind_DECISION_KIND_MIGRATION_APPROVAL:
		object := &operatorv1alpha1.PtahMigrationApproval{}
		if err := c.Get(ctx, key, object); err != nil {
			return nil, err
		}
		read := model.MigrationApproval(object)
		read.PlanRetention = planRetention(ctx, c, key.Namespace, &operatorv1alpha1.PtahMigrationPlan{}, object.Spec.PlanRef)
		return read, nil
	case consolev1.DecisionKind_DECISION_KIND_RUN_ACKNOWLEDGMENT:
		object := &operatorv1alpha1.PtahMigrationRunAcknowledgment{}
		if err := c.Get(ctx, key, object); err != nil {
			return nil, err
		}
		return model.RunAcknowledgment(object), nil
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, "name the kind of decision")
	}
}

// submission is one decision on its way: what it is, who sends it, and the
// object that carries it.
type submission struct {
	action    string
	kind      consolev1.DecisionKind
	key       decision.Key
	object    client.Object
	principal kube.Principal
	event     audit.Event
}

// approval is one plan approval on its way: the owner and plan the person
// reviewed, and how to tell whether they are still current.
type approval struct {
	kind        consolev1.DecisionKind
	namespace   string
	owner, plan operatorv1alpha1.ImmutableObjectReference
	fingerprint string
	changed     func(context.Context, client.Client) (string, error)
}

// approvalKinds names, for each kind of approval, the action, the owner's
// kind, and the request field that names the owner.
var approvalKinds = map[consolev1.DecisionKind]struct{ action, ownerKind, ownerField string }{
	consolev1.DecisionKind_DECISION_KIND_SCHEMA_APPROVAL:    {"approve-schema-plan", "PtahSchema", "schema"},
	consolev1.DecisionKind_DECISION_KIND_MIGRATION_APPROVAL: {"approve-migration-plan", "PtahMigration", "migration"},
}

// object is the approval as sent: the references and the fingerprint, and
// nothing admission stamps.
func (a approval) object() client.Object {
	if a.kind == consolev1.DecisionKind_DECISION_KIND_SCHEMA_APPROVAL {
		return &operatorv1alpha1.PtahSchemaApproval{Spec: operatorv1alpha1.PtahSchemaApprovalSpec{
			SchemaRef: a.owner, PlanRef: a.plan, PlanFingerprint: a.fingerprint,
		}}
	}
	return &operatorv1alpha1.PtahMigrationApproval{Spec: operatorv1alpha1.PtahMigrationApprovalSpec{
		MigrationRef: a.owner, PlanRef: a.plan, PlanFingerprint: a.fingerprint,
	}}
}

func ref(name, uid string) operatorv1alpha1.ImmutableObjectReference {
	return operatorv1alpha1.ImmutableObjectReference{Name: name, UID: types.UID(uid)}
}

// approve reads the owner again and sends the approval only if the plan the
// person reviewed is still the current one.
func (d *DecisionService) approve(ctx context.Context, a approval) (*consolev1.DecisionResult, error) {
	names := approvalKinds[a.kind]
	if err := d.scope.check([]string{a.namespace}); err != nil {
		return nil, err
	}
	if err := required([]field{
		{names.ownerField + "_name", a.owner.Name}, {names.ownerField + "_uid", string(a.owner.UID)},
		{"plan_name", a.plan.Name}, {"plan_uid", string(a.plan.UID)}, {"plan_fingerprint", a.fingerprint},
	}); err != nil {
		return nil, err
	}
	c, principal, err := actor(ctx, d.factory)
	if err != nil {
		return nil, err
	}
	event := audit.Event{
		Action: names.action, Context: principal.Context, AccountID: principal.AccountID,
		Namespace: a.namespace, Kind: names.ownerKind, Name: a.owner.Name, UID: string(a.owner.UID),
		PlanName: a.plan.Name, PlanUID: string(a.plan.UID), PlanFingerprint: a.fingerprint,
	}
	if changed, err := a.changed(ctx, c); err != nil {
		return nil, readError(err, "the "+names.ownerField)
	} else if changed != "" {
		return d.changed(ctx, event, changed), nil
	}
	return d.submit(ctx, c, submission{
		action: names.action, kind: a.kind, principal: principal, event: event, object: a.object(),
		key: decision.Key{Action: names.action, Namespace: a.namespace, TargetUID: string(a.owner.UID), Payload: string(a.plan.UID) + "/" + a.fingerprint},
	}), nil
}

// ApproveSchemaPlan approves exactly the plan the person reviewed. The schema
// is read again first; if its current plan is not that plan, nothing is sent.
func (d *DecisionService) ApproveSchemaPlan(ctx context.Context, request *consolev1.ApproveSchemaPlanRequest) (*consolev1.ApproveSchemaPlanResponse, error) {
	result, err := d.approve(ctx, approval{
		kind: consolev1.DecisionKind_DECISION_KIND_SCHEMA_APPROVAL, namespace: request.GetNamespace(),
		owner: ref(request.GetSchemaName(), request.GetSchemaUid()), plan: ref(request.GetPlanName(), request.GetPlanUid()),
		fingerprint: request.GetPlanFingerprint(),
		changed:     func(ctx context.Context, c client.Client) (string, error) { return schemaPlanChanged(ctx, c, request) },
	})
	if err != nil {
		return nil, err
	}
	return &consolev1.ApproveSchemaPlanResponse{Result: result}, nil
}

// schemaPlanChanged says why the schema's current plan is not the one the
// request names, or nothing when it is.
func schemaPlanChanged(ctx context.Context, c client.Client, request *consolev1.ApproveSchemaPlanRequest) (string, error) {
	schema := &operatorv1alpha1.PtahSchema{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetSchemaName()}, schema); err != nil {
		return "", err
	}
	current := schema.Status.Plan
	switch {
	case string(schema.UID) != request.GetSchemaUid():
		return "The schema was deleted and created again since you reviewed it. Nothing was approved.", nil
	case current == nil:
		return "The schema no longer has a plan waiting. Nothing was approved.", nil
	case current.Name != request.GetPlanName() || string(current.UID) != request.GetPlanUid() || current.Fingerprint != request.GetPlanFingerprint():
		return "A newer plan replaced the one you reviewed. Nothing was approved.", nil
	}
	plan := &operatorv1alpha1.PtahSchemaPlan{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetPlanName()}, plan); err != nil {
		return "", err
	}
	switch {
	case string(plan.UID) != request.GetPlanUid():
		return "The plan under that name is a different object now. Nothing was approved.", nil
	case !planread.SupportedContract(plan.Spec.ContractVersion) || plan.Spec.ContractVersion != request.GetContractVersion():
		return fmt.Sprintf("The plan was written under contract %d, which this console does not approve. Nothing was approved.", plan.Spec.ContractVersion), nil
	}
	return "", nil
}

// ApproveMigrationPlan approves exactly the plan the person reviewed.
func (d *DecisionService) ApproveMigrationPlan(ctx context.Context, request *consolev1.ApproveMigrationPlanRequest) (*consolev1.ApproveMigrationPlanResponse, error) {
	result, err := d.approve(ctx, approval{
		kind: consolev1.DecisionKind_DECISION_KIND_MIGRATION_APPROVAL, namespace: request.GetNamespace(),
		owner: ref(request.GetMigrationName(), request.GetMigrationUid()), plan: ref(request.GetPlanName(), request.GetPlanUid()),
		fingerprint: request.GetPlanFingerprint(),
		changed: func(ctx context.Context, c client.Client) (string, error) {
			return migrationPlanChanged(ctx, c, request)
		},
	})
	if err != nil {
		return nil, err
	}
	return &consolev1.ApproveMigrationPlanResponse{Result: result}, nil
}

func migrationPlanChanged(ctx context.Context, c client.Client, request *consolev1.ApproveMigrationPlanRequest) (string, error) {
	migration := &operatorv1alpha1.PtahMigration{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetMigrationName()}, migration); err != nil {
		return "", err
	}
	current := migration.Status.Plan
	switch {
	case string(migration.UID) != request.GetMigrationUid():
		return "The migration was deleted and created again since you reviewed it. Nothing was approved.", nil
	case current == nil || current.Name != request.GetPlanName() || string(current.UID) != request.GetPlanUid():
		return "A newer plan replaced the one you reviewed. Nothing was approved.", nil
	}
	plan := &operatorv1alpha1.PtahMigrationPlan{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetPlanName()}, plan); err != nil {
		return "", err
	}
	if string(plan.UID) != request.GetPlanUid() || plan.Spec.Fingerprint != request.GetPlanFingerprint() {
		return "The plan under that name is not the one you reviewed. Nothing was approved.", nil
	}
	return "", nil
}

// AcknowledgeMigrationRun settles exactly the run the person looked at.
func (d *DecisionService) AcknowledgeMigrationRun(ctx context.Context, request *consolev1.AcknowledgeMigrationRunRequest) (*consolev1.AcknowledgeMigrationRunResponse, error) {
	if err := d.scope.check([]string{request.GetNamespace()}); err != nil {
		return nil, err
	}
	if err := required([]field{
		{"migration_name", request.GetMigrationName()}, {"migration_uid", request.GetMigrationUid()}, {"operation_id", request.GetOperationId()},
	}); err != nil {
		return nil, err
	}
	c, principal, err := actor(ctx, d.factory)
	if err != nil {
		return nil, err
	}
	event := audit.Event{
		Action: "acknowledge-migration-run", Context: principal.Context, AccountID: principal.AccountID,
		Namespace: request.GetNamespace(), Kind: "PtahMigration", Name: request.GetMigrationName(), UID: request.GetMigrationUid(),
		Reason: "operation " + request.GetOperationId(),
	}
	migration := &operatorv1alpha1.PtahMigration{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetMigrationName()}, migration); err != nil {
		return nil, readError(err, "the migration")
	}
	run := migration.Status.UnresolvedRun
	switch {
	case string(migration.UID) != request.GetMigrationUid():
		return &consolev1.AcknowledgeMigrationRunResponse{Result: d.changed(ctx, event,
			"The migration was deleted and created again since you looked at it. Nothing was acknowledged.")}, nil
	case run == nil || run.OperationID != request.GetOperationId():
		return &consolev1.AcknowledgeMigrationRunResponse{Result: d.changed(ctx, event,
			"The migration is no longer waiting on that run. Nothing was acknowledged.")}, nil
	case migration.Status.ResolvedRun != nil && migration.Status.ResolvedRun.OperationID == run.OperationID:
		return &consolev1.AcknowledgeMigrationRunResponse{Result: d.changed(ctx, event,
			"That run was settled already. Nothing was acknowledged.")}, nil
	}
	result := d.submit(ctx, c, submission{
		action: event.Action, kind: consolev1.DecisionKind_DECISION_KIND_RUN_ACKNOWLEDGMENT, principal: principal, event: event,
		key: decision.Key{Action: event.Action, Namespace: request.GetNamespace(), TargetUID: request.GetMigrationUid(), Payload: request.GetOperationId()},
		object: &operatorv1alpha1.PtahMigrationRunAcknowledgment{Spec: operatorv1alpha1.PtahMigrationRunAcknowledgmentSpec{
			MigrationRef: ref(request.GetMigrationName(), request.GetMigrationUid()),
			OperationID:  request.GetOperationId(),
		}},
	})
	return &consolev1.AcknowledgeMigrationRunResponse{Result: result}, nil
}

// changed reports a decision that was not sent because what the person
// reviewed is no longer current.
func (d *DecisionService) changed(ctx context.Context, event audit.Event, message string) *consolev1.DecisionResult {
	event.Outcome = audit.PlanChanged
	event.Reason = message
	d.audit.Record(ctx, event)
	return &consolev1.DecisionResult{Outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED, Message: message}
}

// submit creates the decision under its ledger name and says how it ended.
// A create that gets no answer is unknown; sending it again creates nothing
// new, because the name is the same and the API server says it exists.
func (d *DecisionService) submit(ctx context.Context, c client.Client, s submission) *consolev1.DecisionResult {
	entry := d.ledger.Begin(s.principal, s.key, "ptah-console-")
	s.object.SetName(entry.Name)
	s.object.SetNamespace(s.key.Namespace)
	s.event.RequestID = entry.RequestID
	result := &consolev1.DecisionResult{Name: entry.Name, RequestId: entry.RequestID}
	sent := asDecision(s.object)

	sendCtx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()
	sendCtx, ids := kube.CollectAuditIDs(sendCtx)
	err := c.Create(sendCtx, s.object, client.FieldOwner(fieldOwner))
	s.event.AuditID = ids.Last()

	switch {
	case err == nil:
		// The create returns the object as admission stamped it.
		result.Outcome, result.Decision = consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, asDecision(s.object)
	case apierrors.IsAlreadyExists(err):
		// A retry after a lost answer, or the same decision made twice: what
		// is stored under this name is the answer, if it is this decision.
		stored, readErr := readDecision(ctx, c, s.kind, types.NamespacedName{Namespace: s.key.Namespace, Name: entry.Name})
		switch {
		case readErr != nil:
			result.Outcome = consolev1.DecisionOutcome_DECISION_OUTCOME_UNKNOWN
			result.Message = "A decision under this name exists and could not be read back."
		case !sameDecision(stored, sent):
			result.Outcome = consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED
			result.Message = "Another decision already holds this name. Nothing was approved."
		default:
			result.Outcome, result.Decision = consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, stored
			result.Message = "This decision was made already; this is the one admission recorded."
		}
	default:
		result.Outcome, result.Message = classify(err)
	}
	s.event.Identity = result.GetDecision().GetDecidedBy()
	s.event.Outcome = auditOutcome(result.Outcome)
	s.event.Reason = result.Message
	d.audit.Record(ctx, s.event)
	return result
}

// asDecision maps a decision object the console sent or read.
func asDecision(object client.Object) *consolev1.Decision {
	switch typed := object.(type) {
	case *operatorv1alpha1.PtahSchemaApproval:
		return model.SchemaApproval(typed)
	case *operatorv1alpha1.PtahMigrationApproval:
		return model.MigrationApproval(typed)
	case *operatorv1alpha1.PtahMigrationRunAcknowledgment:
		return model.RunAcknowledgment(typed)
	default:
		return nil
	}
}

// sameDecision says whether a stored decision decides what was sent.
func sameDecision(stored, sent *consolev1.Decision) bool {
	return stored.GetKind() == sent.GetKind() &&
		stored.GetOwnerName() == sent.GetOwnerName() && stored.GetOwnerUid() == sent.GetOwnerUid() &&
		stored.GetPlanName() == sent.GetPlanName() && stored.GetPlanUid() == sent.GetPlanUid() &&
		stored.GetPlanFingerprint() == sent.GetPlanFingerprint() && stored.GetOperationId() == sent.GetOperationId()
}

// classify turns the API server's answer to a write into an outcome. The
// API server's and admission's messages are passed on: they are the answer
// this identity would get from kubectl.
func classify(err error) (consolev1.DecisionOutcome, string) {
	var status apierrors.APIStatus
	switch {
	case err == nil:
		return consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, ""
	case errors.As(err, &status) && apierrors.IsForbidden(err) && rbacRefusal.MatchString(status.Status().Message):
		return consolev1.DecisionOutcome_DECISION_OUTCOME_FORBIDDEN, status.Status().Message
	case errors.As(err, &status) && (apierrors.IsForbidden(err) || apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)):
		return consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, status.Status().Message
	default:
		// No answer, or one that does not say whether the object was
		// stored: a timeout, a dropped connection, a server error.
		return consolev1.DecisionOutcome_DECISION_OUTCOME_UNKNOWN, unknownMessage(err)
	}
}

// rbacRefusal is the authorizer's refusal, which names the user and the verb
// it may not use. Admission refuses with 403 too, in its own words.
var rbacRefusal = regexp.MustCompile(`is forbidden: User ".*" cannot [a-z]+ resource "`)

func unknownMessage(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout(), apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		return "The API server did not answer in time. The decision may have been stored; read it back before doing anything else."
	default:
		return "The answer was lost. The decision may have been stored; read it back before doing anything else."
	}
}

func auditOutcome(outcome consolev1.DecisionOutcome) string {
	switch outcome {
	case consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED:
		return audit.Admitted
	case consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED:
		return audit.PlanChanged
	case consolev1.DecisionOutcome_DECISION_OUTCOME_FORBIDDEN:
		return audit.Forbidden
	case consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED:
		return audit.Refused
	default:
		return audit.Unknown
	}
}

// SetSuspended suspends or resumes exactly the object the person saw, at the
// version they saw, and changes nothing else.
func (d *DecisionService) SetSuspended(ctx context.Context, request *consolev1.SetSuspendedRequest) (*consolev1.SetSuspendedResponse, error) {
	if err := d.scope.check([]string{request.GetNamespace()}); err != nil {
		return nil, err
	}
	c, principal, err := actor(ctx, d.factory)
	if err != nil {
		return nil, err
	}
	var object client.Object
	kind := ""
	switch request.GetKind() {
	case consolev1.ResourceKind_RESOURCE_KIND_SCHEMA:
		object, kind = &operatorv1alpha1.PtahSchema{}, "PtahSchema"
	case consolev1.ResourceKind_RESOURCE_KIND_MIGRATION:
		object, kind = &operatorv1alpha1.PtahMigration{}, "PtahMigration"
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, "name a schema or a migration")
	}
	if err := required([]field{{"name", request.GetName()}, {"uid", request.GetUid()}, {"resource_version", request.GetResourceVersion()}}); err != nil {
		return nil, err
	}
	action := "resume"
	if request.GetSuspended() {
		action = "suspend"
	}
	event := audit.Event{
		Action: action, Context: principal.Context, AccountID: principal.AccountID,
		Namespace: request.GetNamespace(), Kind: kind, Name: request.GetName(), UID: request.GetUid(),
	}
	// The test makes the change apply only at the version the person saw. A
	// resource version is the store's revision, so an object deleted and
	// created again under the name never carries the one they saw. "add" sets
	// a field that is absent while false.
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/resourceVersion", "value": request.GetResourceVersion()},
		{"op": "add", "path": "/spec/suspend", "value": request.GetSuspended()},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "build the change").WithCause(err)
	}
	object.SetNamespace(request.GetNamespace())
	object.SetName(request.GetName())

	sendCtx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()
	sendCtx, ids := kube.CollectAuditIDs(sendCtx)
	err = c.Patch(sendCtx, object, client.RawPatch(types.JSONPatchType, patch), client.FieldOwner(fieldOwner))
	event.AuditID = ids.Last()

	response := &consolev1.SetSuspendedResponse{}
	switch {
	case err == nil:
		response.Outcome, response.ResourceVersion = consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, object.GetResourceVersion()
	case apierrors.IsNotFound(err) || ((apierrors.IsInvalid(err) || apierrors.IsConflict(err)) && movedOn(ctx, c, object, request)):
		// A failed test: the object changed, or is another object now.
		response.Outcome = consolev1.DecisionOutcome_DECISION_OUTCOME_CHANGED
		response.Message = "The object changed since you looked at it. Nothing was changed; look again."
	default:
		response.Outcome, response.Message = classify(err)
	}
	event.Outcome, event.Reason = auditOutcome(response.Outcome), response.Message
	d.audit.Record(ctx, event)
	return response, nil
}

// movedOn says whether the object is no longer the one the person saw. A
// failed patch test and a validation refusal both come back as Invalid; only
// reading the object again tells them apart.
func movedOn(ctx context.Context, c client.Client, object client.Object, request *consolev1.SetSuspendedRequest) bool {
	err := c.Get(ctx, types.NamespacedName{Namespace: request.GetNamespace(), Name: request.GetName()}, object)
	if err != nil {
		return apierrors.IsNotFound(err)
	}
	return string(object.GetUID()) != request.GetUid() || object.GetResourceVersion() != request.GetResourceVersion()
}

// required refuses a request that leaves out what pins the decision to what
// the person saw.
func required(fields []field) error {
	for _, f := range fields {
		if f.value == "" {
			return connect.NewError(connect.CodeInvalidArgument, f.name+" is required")
		}
	}
	return nil
}

type field struct{ name, value string }
