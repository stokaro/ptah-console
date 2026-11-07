package model_test

import (
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/model"
)

// The rows below are statuses built the way the operator's controllers write
// them at v0.2.9, each named after the function that writes it. No reading the
// operator recorded holds a schema that a policy refused.

const (
	blockedKind  = consolev1.AttentionKind_ATTENTION_KIND_BLOCKED
	driftKind    = consolev1.AttentionKind_ATTENTION_KIND_DRIFT
	approvalKind = consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL
	runKind      = consolev1.AttentionKind_ATTENTION_KIND_UNRESOLVED_RUN
)

func setConditions(list *[]metav1.Condition, conditions ...metav1.Condition) {
	for _, condition := range conditions {
		apimeta.SetStatusCondition(list, condition)
	}
}

// heldByPolicy is a schema whose plan harvest published a destructive plan
// that spec.policy.allowDestructive refuses (setPlanPolicyStatus).
func heldByPolicy() *operatorv1alpha1.PtahSchema {
	schema := schemaWith(
		condition("ArtifactResolved", metav1.ConditionTrue, "DigestPinned"),
		condition("ArtifactVerified", metav1.ConditionTrue, "PolicySatisfied"),
		condition("DriftDetected", metav1.ConditionTrue, "ScopedChanges"),
		condition("InSync", metav1.ConditionFalse, "ScopedChanges"),
		condition("PlanReady", metav1.ConditionTrue, "Published"),
		condition("ApprovalRequired", metav1.ConditionFalse, "DestructiveChangesDisabled"),
		condition("Ready", metav1.ConditionFalse, "PolicyBlocked"),
	)
	schema.Status.Phase = operatorv1alpha1.PhaseBlocked
	schema.Status.Plan = &operatorv1alpha1.CurrentPlanStatus{
		Name: "storefront-plan", UID: "uid-plan", Fingerprint: "sha256:plan", Destructive: true, StatementCount: 1,
	}
	return schema
}

// readAgain is a schema as a claim of its interval read leaves it: the phase
// the operation runs in, Ready rewritten, and on Resolve the source, the plan
// and convergence marked unknown until the read ends (claim and
// markSourceRefreshPending).
func readAgain(schema *operatorv1alpha1.PtahSchema, operation operatorv1alpha1.OperationType, phase operatorv1alpha1.ReconciliationPhase) *operatorv1alpha1.PtahSchema {
	schema.Status.Phase = phase
	schema.Status.ActiveOperation = &operatorv1alpha1.ActiveOperationStatus{Type: operation, ID: "sha256:" + string(operation)}
	setConditions(&schema.Status.Conditions, condition("Ready", metav1.ConditionFalse, "OperationInProgress"))
	if operation == operatorv1alpha1.OperationResolve {
		setConditions(&schema.Status.Conditions,
			condition("ArtifactResolved", metav1.ConditionUnknown, "Refreshing"),
			condition("PlanReady", metav1.ConditionUnknown, "SourceRefreshPending"),
			condition("InSync", metav1.ConditionUnknown, "SourceRefreshPending"),
		)
	}
	return schema
}

func blockedSchema(conditions ...metav1.Condition) *operatorv1alpha1.PtahSchema {
	schema := schemaWith(conditions...)
	schema.Status.Phase = operatorv1alpha1.PhaseBlocked
	return schema
}

// Every way the operator blocks a schema brings it to a person, with what
// holds it, and keeps it there while the operator reads the schema again.
func TestEachWayTheOperatorBlocksASchemaNeedsAPerson(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		schema *operatorv1alpha1.PtahSchema
		reason string
	}{
		{
			name:   "a destructive plan the policy refuses",
			schema: heldByPolicy(),
			reason: "The destructive-change policy blocks the plan.",
		},
		{
			name:   "the same schema while its source is resolved again",
			schema: readAgain(heldByPolicy(), operatorv1alpha1.OperationResolve, operatorv1alpha1.PhaseResolving),
			reason: "The destructive-change policy blocks the plan.",
		},
		{
			name:   "the same schema while its artifact is verified again",
			schema: readAgain(heldByPolicy(), operatorv1alpha1.OperationVerify, operatorv1alpha1.PhaseVerifying),
			reason: "The destructive-change policy blocks the plan.",
		},
		{
			name:   "the same schema while its database is observed again",
			schema: readAgain(heldByPolicy(), operatorv1alpha1.OperationObserve, operatorv1alpha1.PhaseObserving),
			reason: "The destructive-change policy blocks the plan.",
		},
		{
			// refuseProtectedTable
			name: "a plan that changes a protected table",
			schema: blockedSchema(
				condition("PlanReady", metav1.ConditionFalse, "ProtectedTable"),
				condition("InSync", metav1.ConditionFalse, "ProtectedTable"),
				condition("Ready", metav1.ConditionFalse, "ProtectedTable"),
			),
			reason: "The artifact changes a table spec.policy.protectedTables fences off.",
		},
		{
			// blockVerification
			name: "an artifact the verification policy refuses",
			schema: blockedSchema(
				condition("ArtifactVerified", metav1.ConditionFalse, "PolicyRefused"),
				condition("PlanReady", metav1.ConditionFalse, "ArtifactUnverified"),
				condition("InSync", metav1.ConditionFalse, "PolicyRefused"),
				condition("Ready", metav1.ConditionFalse, "PolicyRefused"),
			),
			reason: "The artifact did not pass verification.",
		},
		{
			// schemaRealmBlocked
			name: "a database another resource claims",
			schema: blockedSchema(
				condition("Suspended", metav1.ConditionFalse, "Active"),
				condition("ApprovalRequired", metav1.ConditionFalse, "RealmConflict"),
				condition("Applying", metav1.ConditionFalse, "RealmConflict"),
				condition("Ready", metav1.ConditionFalse, "RealmConflict"),
			),
			reason: "Ready RealmConflict",
		},
		{
			// schemaRealmBlocked
			name: "a realm that does not admit the namespace",
			schema: blockedSchema(
				condition("Suspended", metav1.ConditionFalse, "Active"),
				condition("ApprovalRequired", metav1.ConditionFalse, "RealmNotAuthorized"),
				condition("Applying", metav1.ConditionFalse, "RealmNotAuthorized"),
				condition("Ready", metav1.ConditionFalse, "RealmNotAuthorized"),
			),
			reason: "Ready RealmNotAuthorized",
		},
		{
			// reconcileEngineSupport
			name: "an engine the operator does not support",
			schema: blockedSchema(
				condition("EngineSupported", metav1.ConditionFalse, "UnsupportedEngine"),
				condition("PlanReady", metav1.ConditionFalse, "UnsupportedEngine"),
				condition("ApprovalRequired", metav1.ConditionFalse, "UnsupportedEngine"),
				condition("Ready", metav1.ConditionFalse, "UnsupportedEngine"),
			),
			reason: "Ready UnsupportedEngine",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			attention := model.Schema(test.schema).GetAttention()

			c.Assert(kinds(attention), qt.DeepEquals, []consolev1.AttentionKind{blockedKind})
			c.Assert(attention[0].GetReason(), qt.Equals, test.reason)
			c.Assert(attention[0].GetNextStep(), qt.Not(qt.Equals), "")
		})
	}
}

// A schema the policy held once and holds no longer is not blocked, however
// much of the refusal its status still carries.
func TestASchemaThePolicyNoLongerHoldsIsNotBlocked(t *testing.T) {
	t.Parallel()
	// The artifact stopped asking for the destructive change: the plan came
	// back with no changes, which clears the plan and leaves ApprovalRequired
	// as the refusal wrote it.
	converged := func() *operatorv1alpha1.PtahSchema {
		schema := heldByPolicy()
		schema.Status.Phase = operatorv1alpha1.PhaseInSync
		schema.Status.Plan = nil
		setConditions(&schema.Status.Conditions,
			condition("DriftDetected", metav1.ConditionFalse, "ScopedConverged"),
			condition("PlanReady", metav1.ConditionFalse, "NoChanges"),
			condition("InSync", metav1.ConditionTrue, "ScopedConverged"),
			condition("Ready", metav1.ConditionTrue, "InSync"),
		)
		return schema
	}
	// The person set spec.policy.allowDestructive, and the plan published
	// again under it waits for an approval.
	allowed := func() *operatorv1alpha1.PtahSchema {
		schema := heldByPolicy()
		schema.Spec.Policy.AllowDestructive = true
		schema.Status.Phase = operatorv1alpha1.PhaseAwaitingApproval
		setConditions(&schema.Status.Conditions,
			condition("ApprovalRequired", metav1.ConditionTrue, "PlanReady"),
			condition("Ready", metav1.ConditionFalse, "AwaitingApproval"),
		)
		return schema
	}
	// apply: Never records plans and applies none (setPlanPolicyStatus): the
	// phase is Blocked, and what a person sees is the drift it leaves.
	recordedOnly := func() *operatorv1alpha1.PtahSchema {
		schema := heldByPolicy()
		schema.Spec.Policy.Apply = operatorv1alpha1.ApplyPolicyNever
		schema.Status.Plan.Destructive = false
		setConditions(&schema.Status.Conditions,
			condition("ApprovalRequired", metav1.ConditionFalse, "ApplyDisabled"),
			condition("Ready", metav1.ConditionFalse, "ApplyDisabled"),
		)
		return schema
	}
	tests := []struct {
		name   string
		schema *operatorv1alpha1.PtahSchema
		want   []consolev1.AttentionKind
	}{
		{name: "converged after the refusal", schema: converged(), want: make([]consolev1.AttentionKind, 0)},
		{
			name:   "converged after the refusal, read again at its interval",
			schema: readAgain(converged(), operatorv1alpha1.OperationResolve, operatorv1alpha1.PhaseResolving),
			want:   make([]consolev1.AttentionKind, 0),
		},
		{name: "allowed, waiting for an approval", schema: allowed(), want: []consolev1.AttentionKind{approvalKind}},
		{name: "a plan apply: Never only records", schema: recordedOnly(), want: []consolev1.AttentionKind{driftKind}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			got := kinds(model.Schema(test.schema).GetAttention())

			c.Assert(got, qt.DeepEquals, test.want)
		})
	}
}

// blockedMigration is a migration with Blocked true for a reason, as every
// site that blocks one writes it: Ready and Progressing false for the same
// reason (migrationBlocked, migrationRealmBlocked and the history harvest).
func blockedMigration(reason, message string) *operatorv1alpha1.PtahMigration {
	blocked := condition("Blocked", metav1.ConditionTrue, reason)
	blocked.Message = message
	migration := migrationWith(operatorv1alpha1.PtahMigrationStatus{
		Phase: operatorv1alpha1.MigrationPhaseBlocked,
		Conditions: []metav1.Condition{
			blocked,
			condition("Ready", metav1.ConditionFalse, reason),
			condition("Progressing", metav1.ConditionFalse, reason),
		},
	})
	return migration
}

// Every reason the operator blocks a migration for brings it to a person. A
// run nobody accounted for is said once, as the run a person settles.
func TestEachWayTheOperatorBlocksAMigrationNeedsAPerson(t *testing.T) {
	t.Parallel()
	differentDatabase := blockedMigration("ApplyOutcomeUnknown",
		"Nothing is pending here, but this is a different database from the one the Unknown run addressed, so it does not say what that run did")
	differentDatabase.Status.UnresolvedRun = &operatorv1alpha1.UnresolvedMigrationRunStatus{
		OperationID: "op-1", Outcome: operatorv1alpha1.MigrationRunOutcomeUnknown,
	}
	tests := []struct {
		name      string
		migration *operatorv1alpha1.PtahMigration
		kind      consolev1.AttentionKind
		reason    string
	}{
		{
			name:      "a dirty revision row",
			migration: blockedMigration("HistoryDirty", "Revision 3 is recorded dirty"),
			kind:      blockedKind, reason: "A failed or interrupted run left a revision row behind.",
		},
		{
			name:      "an applied migration that no longer matches its file",
			migration: blockedMigration("HistoryModified", "1 applied migrations no longer match their files"),
			kind:      blockedKind, reason: "An applied migration no longer matches the file that recorded it.",
		},
		{
			name:      "a migration below the applied version",
			migration: blockedMigration("HistoryOutOfOrder", "1 migrations sort below applied version 4"),
			kind:      blockedKind, reason: "The artifact carries a migration below a version the database already applied.",
		},
		{
			name:      "a database ahead of the artifact",
			migration: blockedMigration("HistoryAhead", "The database is at version 5 and this artifact ends at version 4"),
			kind:      blockedKind, reason: "The database records a migration the artifact does not carry.",
		},
		{
			name:      "a database another resource claims",
			migration: blockedMigration("RealmConflict", "Database is also claimed by PtahSchema application/storefront"),
			kind:      blockedKind, reason: "Database is also claimed by PtahSchema application/storefront",
		},
		{
			name:      "a realm that does not admit the namespace",
			migration: blockedMigration("RealmNotAuthorized", "PtahRealm shop does not admit namespace application"),
			kind:      blockedKind, reason: "PtahRealm shop does not admit namespace application",
		},
		{
			name:      "an engine the operator does not support",
			migration: blockedMigration("UnsupportedEngine", `Database engine "Oracle" is not supported by this operator`),
			kind:      blockedKind, reason: `Database engine "Oracle" is not supported by this operator`,
		},
		{
			// reconcileUnresolvedRunCopy, when the annotation cannot be decoded:
			// there is no run in the status for a person to acknowledge.
			name: "a run recorded where the operator cannot read it",
			migration: blockedMigration("ApplyOutcomeUnknown",
				"The operator.ptah.run/unresolved-run annotation records a run nobody accounted for, and this manager cannot read it"),
			kind: blockedKind,
			reason: "The operator.ptah.run/unresolved-run annotation records a run nobody accounted for, " +
				"and this manager cannot read it",
		},
		{
			name:      "a run nobody accounted for, read against another database",
			migration: differentDatabase,
			kind:      runKind, reason: "Run op-1 ended without proof of what it applied.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			attention := model.Migration(test.migration).GetAttention()

			c.Assert(kinds(attention), qt.DeepEquals, []consolev1.AttentionKind{test.kind})
			c.Assert(attention[0].GetReason(), qt.Equals, test.reason)
			c.Assert(attention[0].GetNextStep(), qt.Not(qt.Equals), "")
		})
	}
}

// The operator recorded both engines blocked on an Apply whose Job went
// missing: the run is said once, as the run a person settles.
func TestAMigrationBlockedOnAnUnresolvedRunSaysTheRunOnce(t *testing.T) {
	t.Parallel()
	var readings map[string]operatorv1alpha1.PtahMigration
	reading(t, "uncertain-migration-retained-claim.json", &readings)
	qt.New(t).Assert(readings, qt.HasLen, 2)
	for engine, migration := range readings {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			asWritten(&migration.ObjectMeta, migration.Status.ObservedGeneration)
			c.Assert(apimeta.IsStatusConditionTrue(migration.Status.Conditions, "Blocked"), qt.IsTrue)

			attention := model.Migration(&migration).GetAttention()

			c.Assert(kinds(attention), qt.DeepEquals, []consolev1.AttentionKind{runKind})
		})
	}
}

// A migration that was blocked and is not any more is not blocked, whatever
// its journal remembers.
func TestAMigrationNoLongerBlockedIsNotBlocked(t *testing.T) {
	t.Parallel()
	ended := metav1.NewTime(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC))
	acknowledged := metav1.NewTime(ended.Add(10 * time.Minute))
	read := metav1.NewTime(acknowledged.Add(time.Minute))
	partialRun := &operatorv1alpha1.MigrationRunStatus{
		Outcome: operatorv1alpha1.MigrationRunOutcomePartial, Message: "committed some of its statements",
		StartedAt: metav1.NewTime(ended.Add(-time.Minute)), FinishedAt: &ended,
	}
	journal := &operatorv1alpha1.LifecycleJournal{
		CoveredSince: metav1.NewTime(ended.Add(-time.Hour)),
		Entries: []operatorv1alpha1.JournalEntry{
			{Type: operatorv1alpha1.JournalRunFinished, At: ended, Outcome: operatorv1alpha1.JournalPartial},
			{Type: operatorv1alpha1.JournalRunAcknowledged, At: acknowledged},
		},
	}
	resolved := &operatorv1alpha1.ResolvedMigrationRunStatus{
		OperationID: "op-1", Outcome: operatorv1alpha1.MigrationRunOutcomePartial,
		Resolution: operatorv1alpha1.MigrationRunResolvedByAcknowledgment, ResolvedAt: acknowledged,
	}
	tests := []struct {
		name      string
		migration *operatorv1alpha1.PtahMigration
	}{
		{
			// The acknowledgment, and the read of the history it starts.
			name: "a history read again after an acknowledged run",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{
				Phase:           operatorv1alpha1.MigrationPhaseResolving,
				ActiveOperation: &operatorv1alpha1.MigrationOperationStatus{Type: operatorv1alpha1.MigrationOperationResolve, ID: "sha256:resolve"},
				LastRun:         partialRun,
				UnresolvedRun:   &operatorv1alpha1.UnresolvedMigrationRunStatus{OperationID: "op-1", Outcome: operatorv1alpha1.MigrationRunOutcomePartial},
				ResolvedRun:     resolved,
				Journal:         journal,
				Conditions: []metav1.Condition{
					condition("Blocked", metav1.ConditionFalse, "RunAcknowledged"),
					condition("Ready", metav1.ConditionFalse, "ApplyOutcomeUnknown"),
					condition("Progressing", metav1.ConditionTrue, "OperationInProgress"),
				},
			}),
		},
		{
			// The history harvest that found the history matching.
			name: "a history that matches the artifact again",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{
				Phase:       operatorv1alpha1.MigrationPhaseInSync,
				History:     &operatorv1alpha1.MigrationHistoryStatus{ObservedAt: read, CurrentVersion: 3},
				LastRun:     partialRun,
				ResolvedRun: resolved,
				Journal:     journal,
				Conditions: []metav1.Condition{
					condition("Blocked", metav1.ConditionFalse, "HistoryMatched"),
					condition("Ready", metav1.ConditionTrue, "HistoryMatched"),
					condition("Progressing", metav1.ConditionFalse, "HistoryMatched"),
				},
			}),
		},
		{
			name: "a migration with no Blocked condition at all",
			migration: migrationWith(operatorv1alpha1.PtahMigrationStatus{
				Phase:   operatorv1alpha1.MigrationPhaseInSync,
				History: &operatorv1alpha1.MigrationHistoryStatus{ObservedAt: read, CurrentVersion: 3},
				Journal: journal,
				Conditions: []metav1.Condition{
					condition("Ready", metav1.ConditionTrue, "HistoryMatched"),
				},
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			got := kinds(model.Migration(test.migration).GetAttention())

			c.Assert(got, qt.DeepEquals, make([]consolev1.AttentionKind, 0))
		})
	}
}
