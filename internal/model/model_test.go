package model_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/model"
)

const (
	done    = consolev1.StageState_STAGE_STATE_DONE
	active  = consolev1.StageState_STAGE_STATE_ACTIVE
	waiting = consolev1.StageState_STAGE_STATE_WAITING
	failed  = consolev1.StageState_STAGE_STATE_FAILED
	unknown = consolev1.StageState_STAGE_STATE_UNKNOWN
	na      = consolev1.StageState_STAGE_STATE_NOT_APPLICABLE
)

func reading(t *testing.T, name string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "readings", name))
	qt.New(t).Assert(err, qt.IsNil)
	qt.New(t).Assert(json.Unmarshal(data, into), qt.IsNil)
}

func states(stages []*consolev1.Stage) []consolev1.StageState {
	out := make([]consolev1.StageState, 0, len(stages))
	for _, s := range stages {
		out = append(out, s.GetState())
	}
	return out
}

func kinds(attention []*consolev1.Attention) []consolev1.AttentionKind {
	out := make([]consolev1.AttentionKind, 0, len(attention))
	for _, a := range attention {
		out = append(out, a.GetKind())
	}
	return out
}

// What the operator wrote during its own acceptance runs, read the way the
// console reads it.
func TestSchemaReadingsFromTheOperator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file      string
		stages    []consolev1.StageState
		attention []consolev1.AttentionKind
	}{
		{
			file:      "alert-schema-recovered.json",
			stages:    []consolev1.StageState{done, done, waiting, waiting, waiting},
			attention: []consolev1.AttentionKind{consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL},
		},
		{
			// Generation 3 against a status written for 1: the reading itself
			// is a status for an older spec, and says so.
			file:   "alert-schema-recovery-wait.json",
			stages: []consolev1.StageState{done, done, done, unknown, waiting},
			attention: []consolev1.AttentionKind{
				consolev1.AttentionKind_ATTENTION_KIND_OUTCOME_UNKNOWN,
				consolev1.AttentionKind_ATTENTION_KIND_SUSPENDED,
				consolev1.AttentionKind_ATTENTION_KIND_STALE_STATUS,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			var schema operatorv1alpha1.PtahSchema
			reading(t, test.file, &schema)
			asWritten(&schema.ObjectMeta, schema.Status.ObservedGeneration)

			out := model.Schema(&schema)

			c.Assert(states(out.GetStages()), qt.DeepEquals, test.stages)
			c.Assert(kinds(out.GetAttention()), qt.DeepEquals, test.attention)
		})
	}
}

func TestMigrationReadingsFromTheOperator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file      string
		stages    []consolev1.StageState
		attention []consolev1.AttentionKind
	}{
		{
			// The run committed part of a file: the Apply failed, and the dirty
			// row it left is the block, said once.
			file:      "partial-run-left-a-dirty-revision.json",
			stages:    []consolev1.StageState{done, failed, waiting, done, failed, waiting},
			attention: []consolev1.AttentionKind{consolev1.AttentionKind_ATTENTION_KIND_BLOCKED},
		},
		{
			file:      "isolated-node-durable-migration.json",
			stages:    []consolev1.StageState{done, done, done, done, done, done},
			attention: make([]consolev1.AttentionKind, 0),
		},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			var migration operatorv1alpha1.PtahMigration
			reading(t, test.file, &migration)
			asWritten(&migration.ObjectMeta, migration.Status.ObservedGeneration)

			out := model.Migration(&migration)

			c.Assert(states(out.GetStages()), qt.DeepEquals, test.stages)
			c.Assert(kinds(out.GetAttention()), qt.DeepEquals, test.attention)
		})
	}
}

// asWritten gives a reading without metadata the generation its status was
// written for; a reading with metadata keeps its own.
func asWritten(meta *metav1.ObjectMeta, observed int64) {
	if meta.Generation == 0 {
		meta.Generation = observed
	}
}

func condition(kind string, status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{Type: kind, Status: status, Reason: reason, Message: kind + " " + reason, ObservedGeneration: 1}
}

func schemaWith(conditions ...metav1.Condition) *operatorv1alpha1.PtahSchema {
	schema := &operatorv1alpha1.PtahSchema{}
	schema.Generation = 1
	schema.Status.ObservedGeneration = 1
	schema.Status.Conditions = conditions
	return schema
}

// Each kind of attention is shown for the state it names and not for the
// state next to it that looks alike.
func TestEachSchemaAttentionKindRefusesItsLookAlike(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		schema *operatorv1alpha1.PtahSchema
		kind   consolev1.AttentionKind
		shown  bool
	}{
		{
			name:   "an approval the plan needs",
			schema: schemaWith(condition("ApprovalRequired", metav1.ConditionTrue, "AwaitingApproval")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL, shown: true,
		},
		{
			name:   "a plan the policy needs no approval for",
			schema: schemaWith(condition("ApprovalRequired", metav1.ConditionFalse, "NotRequired")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL,
		},
		{
			name:   "an Apply whose outcome is unknown",
			schema: schemaWith(condition("Applying", metav1.ConditionFalse, "OutcomeUnknown")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_OUTCOME_UNKNOWN, shown: true,
		},
		{
			name:   "an Apply whose proof is pending",
			schema: schemaWith(condition("Applying", metav1.ConditionFalse, "JobCompleted")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_OUTCOME_UNKNOWN,
		},
		{
			name:   "a failed operation",
			schema: schemaWith(condition("ReconciliationFailed", metav1.ConditionTrue, "OperationFailed")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_FAILED, shown: true,
		},
		{
			name:   "a failure cleared by a later operation",
			schema: schemaWith(condition("ReconciliationFailed", metav1.ConditionFalse, "Succeeded")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_FAILED,
		},
		{
			name:   "a protected table",
			schema: schemaWith(condition("PlanReady", metav1.ConditionFalse, "ProtectedTable")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_BLOCKED, shown: true,
		},
		{
			name:   "an observation that has to be repeated",
			schema: schemaWith(condition("PlanReady", metav1.ConditionFalse, "StaleObservation")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_BLOCKED,
		},
		{
			name:   "suspension",
			schema: schemaWith(condition("Suspended", metav1.ConditionTrue, "Requested")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_SUSPENDED, shown: true,
		},
		{
			name:   "active reconciliation",
			schema: schemaWith(condition("Suspended", metav1.ConditionFalse, "Active")),
			kind:   consolev1.AttentionKind_ATTENTION_KIND_SUSPENDED,
		},
		{
			name: "drift under apply: Never",
			schema: schemaWith(condition("InSync", metav1.ConditionFalse, "ScopedChanges"),
				condition("ApprovalRequired", metav1.ConditionFalse, "ApplyDisabled")),
			kind: consolev1.AttentionKind_ATTENTION_KIND_DRIFT, shown: true,
		},
		{
			name: "drift a plan is waiting to fix",
			schema: schemaWith(condition("InSync", metav1.ConditionFalse, "ScopedChanges"),
				condition("ApprovalRequired", metav1.ConditionTrue, "AwaitingApproval")),
			kind: consolev1.AttentionKind_ATTENTION_KIND_DRIFT,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			got := kinds(model.Schema(test.schema).GetAttention())

			c.Assert(contains(got, test.kind), qt.Equals, test.shown, qt.Commentf("attention: %v", got))
		})
	}
}

func TestAStatusWrittenForAnOlderSpecIsSaidToBe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		generation int64
		observed   int64
		stale      bool
	}{
		{name: "the spec changed since", generation: 2, observed: 1, stale: true},
		{name: "the status is current", generation: 2, observed: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			schema := schemaWith()
			schema.Generation = test.generation
			schema.Status.ObservedGeneration = test.observed

			out := model.Schema(schema)

			c.Assert(out.GetStatusCurrent(), qt.Equals, !test.stale)
			c.Assert(contains(kinds(out.GetAttention()), consolev1.AttentionKind_ATTENTION_KIND_STALE_STATUS), qt.Equals, test.stale)
		})
	}
}

func migrationWith(status operatorv1alpha1.PtahMigrationStatus) *operatorv1alpha1.PtahMigration {
	migration := &operatorv1alpha1.PtahMigration{Status: status}
	migration.Generation = 1
	migration.Status.ObservedGeneration = 1
	return migration
}

func TestEachMigrationAttentionKindRefusesItsLookAlike(t *testing.T) {
	t.Parallel()
	unresolvedRun := &operatorv1alpha1.UnresolvedMigrationRunStatus{OperationID: "op-1", Outcome: operatorv1alpha1.MigrationRunOutcomeUnknown}
	failedRun := &operatorv1alpha1.MigrationRunStatus{Outcome: operatorv1alpha1.MigrationRunOutcomeFailed, Message: "duplicate key"}
	// The run ends between two resolutions, so each row's order is its own.
	before := metav1.NewTime(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC))
	ended := metav1.NewTime(before.Add(time.Minute))
	after := metav1.NewTime(ended.Add(time.Minute))
	partialRun := &operatorv1alpha1.MigrationRunStatus{
		Outcome: operatorv1alpha1.MigrationRunOutcomePartial, Message: "committed some of its statements",
		StartedAt: before, FinishedAt: &ended,
	}
	tests := []struct {
		name      string
		migration *operatorv1alpha1.PtahMigration
		kind      consolev1.AttentionKind
		shown     bool
	}{
		{
			name:      "a run nothing settled",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{UnresolvedRun: unresolvedRun}),
			kind:      consolev1.AttentionKind_ATTENTION_KIND_UNRESOLVED_RUN, shown: true,
		},
		{
			name: "a run an acknowledgment settled",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{
				UnresolvedRun: unresolvedRun,
				ResolvedRun:   &operatorv1alpha1.ResolvedMigrationRunStatus{OperationID: "op-1"},
			}),
			kind: consolev1.AttentionKind_ATTENTION_KIND_UNRESOLVED_RUN,
		},
		{
			name: "a modified history",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{Conditions: []metav1.Condition{
				condition("Blocked", metav1.ConditionTrue, "HistoryModified"),
			}}),
			kind: consolev1.AttentionKind_ATTENTION_KIND_BLOCKED, shown: true,
		},
		{
			name: "a history read again after an acknowledgment",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{Conditions: []metav1.Condition{
				condition("Blocked", metav1.ConditionFalse, "RunAcknowledged"),
			}}),
			kind: consolev1.AttentionKind_ATTENTION_KIND_BLOCKED,
		},
		{
			name:      "a failed run",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{LastRun: failedRun}),
			kind:      consolev1.AttentionKind_ATTENTION_KIND_FAILED, shown: true,
		},
		{
			name: "a partial run a person settled after it ended",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{
				LastRun:     partialRun,
				ResolvedRun: &operatorv1alpha1.ResolvedMigrationRunStatus{OperationID: "op-1", ResolvedAt: after},
			}),
			kind: consolev1.AttentionKind_ATTENTION_KIND_FAILED,
		},
		{
			name: "a partial run that ended after an older settlement",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{
				LastRun:     partialRun,
				ResolvedRun: &operatorv1alpha1.ResolvedMigrationRunStatus{OperationID: "op-0", ResolvedAt: before},
			}),
			kind: consolev1.AttentionKind_ATTENTION_KIND_FAILED, shown: true,
		},
		{
			name: "a failed run the history has since caught up with",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{LastRun: failedRun, Conditions: []metav1.Condition{
				condition("Ready", metav1.ConditionTrue, "HistoryMatched"),
			}}),
			kind: consolev1.AttentionKind_ATTENTION_KIND_FAILED,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			got := kinds(model.Migration(test.migration).GetAttention())

			c.Assert(contains(got, test.kind), qt.Equals, test.shown, qt.Commentf("attention: %v", got))
		})
	}
}

// A run a person accounted for is not a failed Apply any more; a run that
// failed after an older settlement still is.
func TestASettledRunIsNotAFailedApply(t *testing.T) {
	t.Parallel()
	before := metav1.NewTime(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC))
	ended := metav1.NewTime(before.Add(time.Minute))
	after := metav1.NewTime(ended.Add(time.Minute))
	run := &operatorv1alpha1.MigrationRunStatus{
		Outcome: operatorv1alpha1.MigrationRunOutcomePartial, StartedAt: before, FinishedAt: &ended,
	}
	tests := []struct {
		name       string
		resolvedAt metav1.Time
		apply      consolev1.StageState
	}{
		{name: "settled after the run ended", resolvedAt: after, apply: consolev1.StageState_STAGE_STATE_WAITING},
		{name: "settled before the run started", resolvedAt: before, apply: consolev1.StageState_STAGE_STATE_FAILED},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			out := model.Migration(migrationWith(operatorv1alpha1.PtahMigrationStatus{
				LastRun:     run,
				ResolvedRun: &operatorv1alpha1.ResolvedMigrationRunStatus{OperationID: "op-1", ResolvedAt: test.resolvedAt},
			}))

			c.Assert(stateOf(out.GetStages(), model.StageApply), qt.Equals, test.apply)
		})
	}
}

func stateOf(stages []*consolev1.Stage, name string) consolev1.StageState {
	for _, s := range stages {
		if s.GetName() == name {
			return s.GetState()
		}
	}
	return consolev1.StageState_STAGE_STATE_UNSPECIFIED
}

// A Secret is named, never read: the target carries the reference only.
func TestTheTargetCarriesTheSecretReferenceOnly(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	schema := schemaWith()
	schema.Spec.Target.Engine = operatorv1alpha1.DatabaseEnginePostgreSQL
	schema.Spec.Target.URLFrom.Name = "storefront-database"
	schema.Spec.Target.URLFrom.Key = "url"

	target := model.Schema(schema).GetTarget()

	c.Assert(target.GetSecretName(), qt.Equals, "storefront-database")
	c.Assert(target.GetSecretKey(), qt.Equals, "url")
	c.Assert(target.GetEngine(), qt.Equals, "PostgreSQL")
}

func TestTheSpecIsShownAsYAMLWithoutTheStatus(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	schema := schemaWith()
	schema.Spec.Target.URLFrom.Name = "storefront-database"
	schema.Spec.Target.URLFrom.Key = "url"
	schema.Status.Phase = "AwaitingApproval"

	spec := model.Schema(schema).GetSpecYaml()

	c.Assert(spec, qt.Contains, "name: storefront-database")
	c.Assert(spec, qt.Contains, "key: url")
	// The control: the status the same object carries is not in it.
	c.Assert(spec, qt.Not(qt.Contains), "AwaitingApproval")
	c.Assert(spec, qt.Not(qt.Contains), "status")
}

func TestGitOpsControllersAreRecognized(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		controller  string
	}{
		{name: "Argo CD by label", labels: map[string]string{"argocd.argoproj.io/instance": "storefront"}, controller: "Argo CD"},
		{name: "Argo CD by tracking id", annotations: map[string]string{"argocd.argoproj.io/tracking-id": "storefront:operator.ptah.run/PtahSchema:application/storefront"}, controller: "Argo CD"},
		{name: "Flux", labels: map[string]string{"kustomize.toolkit.fluxcd.io/name": "apps"}, controller: "Flux"},
		{name: "a label that only looks like it", labels: map[string]string{"app.kubernetes.io/managed-by": "Helm"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			schema := schemaWith()
			schema.Labels, schema.Annotations = test.labels, test.annotations

			c.Assert(model.Schema(schema).GetGitops().GetController(), qt.Equals, test.controller)
		})
	}
}

func contains(list []consolev1.AttentionKind, kind consolev1.AttentionKind) bool {
	return slices.Contains(list, kind)
}

// Each interval the operator refreshes the source and plans again: for a while
// ApprovalRequired stays true and status.plan is empty. The schema still waits
// for an approval, and the next step says the plan comes back first; while the
// plan is still published, it says the read has to end first. The conditions
// are the ones the operator wrote in the demo lab (run 39330236712).
func TestAWaitingSchemaBeingPlannedAgainSaysSo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan *operatorv1alpha1.CurrentPlanStatus
		next string
	}{
		{name: "with its plan still published", plan: &operatorv1alpha1.CurrentPlanStatus{Name: "ptah-plan-32d8"}, next: "The operator is reading the schema again"},
		{name: "while the plan is prepared again", plan: nil, next: "The operator is planning again"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			schema := schemaWith(
				condition("ApprovalRequired", metav1.ConditionTrue, "Waiting"),
				condition("PlanReady", metav1.ConditionUnknown, "SourceRefreshPending"),
				condition("Ready", metav1.ConditionFalse, "OperationInProgress"),
			)
			schema.Status.Plan = test.plan

			attention := model.Schema(schema).GetAttention()

			c.Assert(kinds(attention), qt.DeepEquals, []consolev1.AttentionKind{consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL})
			c.Assert(attention[0].GetNextStep(), qt.Contains, test.next)
		})
	}
}

// A decision's state comes from its own conditions, with the condition's
// message as the reason; a stale approval says why it went stale.
func TestADecisionSaysWhatTheOperatorMadeOfIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		conditions []metav1.Condition
		state      consolev1.DecisionState
		reason     string
	}{
		{name: "nothing read yet", conditions: nil, state: consolev1.DecisionState_DECISION_STATE_PENDING},
		{
			name:       "accepted",
			conditions: []metav1.Condition{condition("Accepted", metav1.ConditionTrue, "Accepted")},
			state:      consolev1.DecisionState_DECISION_STATE_ACCEPTED, reason: "Accepted Accepted",
		},
		{
			name:       "refused by the operator",
			conditions: []metav1.Condition{condition("Accepted", metav1.ConditionFalse, "PlanNoLongerCurrent")},
			state:      consolev1.DecisionState_DECISION_STATE_REFUSED, reason: "Accepted PlanNoLongerCurrent",
		},
		{
			name: "stale because a newer plan replaced it",
			conditions: []metav1.Condition{
				condition("Accepted", metav1.ConditionTrue, "Accepted"),
				condition("Stale", metav1.ConditionTrue, "SupersededApproval"),
			},
			state: consolev1.DecisionState_DECISION_STATE_STALE, reason: "Stale SupersededApproval",
		},
		{
			name: "consumed by an Apply",
			conditions: []metav1.Condition{
				condition("Accepted", metav1.ConditionTrue, "Accepted"),
				condition("Consumed", metav1.ConditionTrue, "DispatchCommitted"),
			},
			state: consolev1.DecisionState_DECISION_STATE_CONSUMED, reason: "Consumed DispatchCommitted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			approval := &operatorv1alpha1.PtahSchemaApproval{}
			approval.Status.Conditions = test.conditions

			decision := model.SchemaApproval(approval)

			c.Assert(decision.GetState(), qt.Equals, test.state)
			c.Assert(decision.GetStateReason(), qt.Equals, test.reason)
		})
	}
}

// Each attention carries the operator plugin's command that shows the same
// state: the plan for a person deciding on it, the resource otherwise.
func TestAttentionNamesTheCommandThatShowsIt(t *testing.T) {
	t.Parallel()
	waiting := schemaWith(condition("ApprovalRequired", metav1.ConditionTrue, "Waiting"))
	waiting.Name, waiting.Namespace = "storefront", "application"
	waiting.Status.Plan = &operatorv1alpha1.CurrentPlanStatus{Name: "ptah-plan-1"}
	replanning := waiting.DeepCopy()
	replanning.Status.Plan = nil
	failed := schemaWith(condition("ReconciliationFailed", metav1.ConditionTrue, "OperationFailed"))
	failed.Name, failed.Namespace = "storefront", "application"
	unresolved := &operatorv1alpha1.PtahMigration{}
	unresolved.Name, unresolved.Namespace = "billing", "application"
	unresolved.Generation, unresolved.Status.ObservedGeneration = 1, 1
	unresolved.Status.UnresolvedRun = &operatorv1alpha1.UnresolvedMigrationRunStatus{OperationID: "op-2"}
	tests := []struct {
		name      string
		attention []*consolev1.Attention
		want      string
	}{
		{name: "a plan waiting for approval", attention: model.Schema(waiting).GetAttention(), want: "kubectl ptah plan storefront -n application"},
		{name: "a schema planning again", attention: model.Schema(replanning).GetAttention(), want: "kubectl ptah schema storefront -n application"},
		{name: "a failed schema", attention: model.Schema(failed).GetAttention(), want: "kubectl ptah schema storefront -n application"},
		{name: "an unresolved migration run", attention: model.Migration(unresolved).GetAttention(), want: "kubectl ptah migration billing -n application"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			c.Assert(test.attention, qt.Not(qt.HasLen), 0)
			c.Assert(test.attention[0].GetCommand(), qt.Equals, test.want)
		})
	}
}

// The revision rows reach the console as the operator published them, newest
// first, and a duration the revision table did not record stays absent: a
// migration that took no measurable time and one nobody timed are not the
// same row.
func TestMigrationRevisionsAreCarriedAsPublished(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	written := metav1.NewTime(time.Date(2026, 10, 24, 7, 42, 30, 0, time.UTC))
	instant := int64(0)
	migration := &operatorv1alpha1.PtahMigration{}
	migration.Status.History = &operatorv1alpha1.MigrationHistoryStatus{
		CurrentVersion: 2,
		Revisions: []operatorv1alpha1.MigrationRevisionStatus{
			{Version: 2, Description: "add invoices", Checksum: "h1:two", State: "applied", AppliedAt: &written, ExecutionMillis: &instant},
			{Version: 1, Description: "create accounts", Checksum: "h1:one", State: "modified", AppliedAt: &written},
		},
	}

	rows := model.Migration(migration).GetHistory().GetRevisions()

	c.Assert(rows, qt.HasLen, 2)
	c.Assert(rows[0].GetVersion(), qt.Equals, int64(2))
	c.Assert(rows[0].GetAppliedAt().AsTime(), qt.Equals, written.Time)
	c.Assert(rows[0].ExecutionMillis, qt.IsNotNil)
	c.Assert(rows[0].GetExecutionMillis(), qt.Equals, int64(0))
	c.Assert(rows[1].GetState(), qt.Equals, "modified")
	c.Assert(rows[1].ExecutionMillis, qt.IsNil)

	migration.Status.History.Revisions = nil
	c.Assert(model.Migration(migration).GetHistory().GetRevisions(), qt.IsNil)
}

// Admission admits an approval only while the operator waits for one: the
// phase is AwaitingApproval and no operation runs. Each interval the operator
// reads a waiting resource again, its plan still current and ApprovalRequired
// still true, and admission refuses an approval until the read ends. The
// migration readings are deliveries in the console's acceptance lab, before
// and during that read, and one changed to run an operation; the schema rows
// change the recovered reading in the one field each mistake would miss.
func TestApprovalOpensOnlyWhileTheOperatorWaitsForIt(t *testing.T) {
	t.Parallel()
	recovered := func(change func(*operatorv1alpha1.PtahSchema)) *operatorv1alpha1.PtahSchema {
		schema := &operatorv1alpha1.PtahSchema{}
		reading(t, "alert-schema-recovered.json", schema)
		change(schema)
		return schema
	}
	schemas := []struct {
		name   string
		schema *operatorv1alpha1.PtahSchema
		open   bool
		next   string
	}{
		{name: "waiting for approval", schema: recovered(func(*operatorv1alpha1.PtahSchema) {}), open: true, next: "Review the plan and approve it"},
		{name: "read again at its interval", schema: recovered(func(s *operatorv1alpha1.PtahSchema) {
			s.Status.Phase = operatorv1alpha1.PhaseResolving
		}), next: "The operator is reading the schema again"},
		{name: "between two operations of the read", schema: recovered(func(s *operatorv1alpha1.PtahSchema) {
			s.Status.Phase = operatorv1alpha1.PhasePlanning
		}), next: "The operator is reading the schema again"},
		{name: "with an operation running", schema: recovered(func(s *operatorv1alpha1.PtahSchema) {
			s.Status.ActiveOperation = &operatorv1alpha1.ActiveOperationStatus{Type: operatorv1alpha1.OperationObserve, ID: "observe-1"}
		}), next: "The operator is reading the schema again"},
	}
	for _, test := range schemas {
		t.Run("a schema "+test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			schema := model.Schema(test.schema)

			c.Assert(schema.GetApprovalOpen(), qt.Equals, test.open)
			waiting := attentionOf(schema.GetAttention(), consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL)
			c.Assert(waiting, qt.IsNotNil)
			c.Assert(waiting.GetNextStep(), qt.Contains, test.next)
		})
	}
	t.Run("a schema whose plan has its approval recorded", func(t *testing.T) {
		t.Parallel()
		schema := recovered(func(s *operatorv1alpha1.PtahSchema) {
			s.Status.Plan.Approval = &operatorv1alpha1.ConsumedApprovalStatus{Name: "approve-1"}
		})
		qt.New(t).Assert(model.Schema(schema).GetApprovalOpen(), qt.IsFalse)
	})
	t.Run("a schema whose plan needs no approval", func(t *testing.T) {
		t.Parallel()
		schema := recovered(func(s *operatorv1alpha1.PtahSchema) {
			apimeta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
				Type: operatorv1alpha1.ConditionApprovalRequired, Status: metav1.ConditionFalse, Reason: "NotRequired",
			})
		})
		qt.New(t).Assert(model.Schema(schema).GetApprovalOpen(), qt.IsFalse)
	})

	unchanged := func(*operatorv1alpha1.PtahMigration) {}
	running := func(m *operatorv1alpha1.PtahMigration) {
		m.Status.ActiveOperation = &operatorv1alpha1.MigrationOperationStatus{Type: operatorv1alpha1.MigrationOperationHistory, ID: "history-1"}
	}
	migrations := []struct {
		name   string
		file   string
		change func(*operatorv1alpha1.PtahMigration)
		open   bool
		next   string
	}{
		{name: "waiting for approval", file: "migration-awaiting-approval.json", change: unchanged, open: true, next: "Review the steps and approve them"},
		{name: "read again at its interval", file: "migration-reading-again.json", change: unchanged, next: "The operator is reading the history again"},
		{name: "between two operations of the read", file: "migration-between-read-operations.json", change: unchanged, next: "The operator is reading the history again"},
		{name: "with an operation running", file: "migration-awaiting-approval.json", change: running, next: "The operator is reading the history again"},
	}
	for _, test := range migrations {
		t.Run("a migration "+test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			read := &operatorv1alpha1.PtahMigration{}
			reading(t, test.file, read)
			test.change(read)
			migration := model.Migration(read)

			c.Assert(migration.GetApprovalOpen(), qt.Equals, test.open)
			waiting := attentionOf(migration.GetAttention(), consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL)
			c.Assert(waiting, qt.IsNotNil)
			c.Assert(waiting.GetNextStep(), qt.Contains, test.next)
		})
	}
}

func attentionOf(attention []*consolev1.Attention, kind consolev1.AttentionKind) *consolev1.Attention {
	for _, a := range attention {
		if a.GetKind() == kind {
			return a
		}
	}
	return nil
}
