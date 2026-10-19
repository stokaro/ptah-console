package model_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
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
