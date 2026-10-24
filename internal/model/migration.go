package model

import (
	"fmt"

	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// Migration stage names. A migration advances: it reads the history, plans
// the versions the database has not applied, applies them, and reads the
// history again.
const (
	StageReadHistory   = "Read history"
	StageVerifyHistory = "Verify history"
)

// historyBlockers are the Blocked reasons a person has to resolve in the
// database; the operator never writes the revision table to clear them.
var historyBlockers = map[string]string{
	string(operatorv1alpha1.ReasonHistoryDirty):      "A failed or interrupted run left a revision row behind.",
	string(operatorv1alpha1.ReasonHistoryModified):   "An applied migration no longer matches the file that recorded it.",
	string(operatorv1alpha1.ReasonHistoryOutOfOrder): "The artifact carries a migration below a version the database already applied.",
	string(operatorv1alpha1.ReasonHistoryAhead):      "The database records a migration the artifact does not carry.",
}

// Migration turns a PtahMigration into the console's message.
func Migration(migration *operatorv1alpha1.PtahMigration) *consolev1.Migration {
	status := migration.Status
	c := index(status.Conditions)
	out := &consolev1.Migration{
		Ref:                objectRef(migration.ObjectMeta),
		Phase:              string(status.Phase),
		StatusCurrent:      statusCurrent(migration.Generation, status.ObservedGeneration),
		Suspended:          migration.Spec.Suspend,
		ApplyPolicy:        string(migration.Spec.Policy.Apply),
		Conditions:         toConditions(status.Conditions),
		Source:             migrationSource(migration, c),
		Target:             &consolev1.Target{Engine: string(migration.Spec.Target.Engine), SecretName: migration.Spec.Target.URLFrom.Name, SecretKey: migration.Spec.Target.URLFrom.Key},
		NextReconciliation: timestamp(status.NextReconciliationTime),
		Gitops:             gitOps(migration.ObjectMeta),
		SpecYaml:           specYAML(migration.Spec),
		Journal:            journal(status.Journal),
		ReadRequest:        readRequest(migration.ObjectMeta, status.LastReadRequest),
	}
	if realm := migration.Spec.Target.RealmRef; realm != nil {
		out.Target.Realm = realm.Name
	}
	if history := status.History; history != nil {
		out.History = &consolev1.MigrationHistory{
			ObservedAt: timestamp(&history.ObservedAt), CurrentVersion: history.CurrentVersion,
			CheckpointVersion: history.CheckpointVersion, AppliedCount: history.AppliedCount,
			PendingCount: history.PendingCount, Dirty: history.Dirty,
			ModifiedVersions: history.ModifiedVersions, OutOfOrderVersions: history.OutOfOrderVersions,
		}
	}
	if plan := status.Plan; plan != nil {
		out.PlanName, out.PlanUid = plan.Name, string(plan.UID)
	}
	if run := status.LastRun; run != nil {
		out.LastRun = &consolev1.MigrationRun{
			Outcome: string(run.Outcome), JobName: run.JobName, JobUid: string(run.JobUID),
			StartedAt: timestamp(&run.StartedAt), FinishedAt: timestamp(run.FinishedAt),
			AppliedVersions: run.AppliedVersions, Message: run.Message,
			Failure: runFailure(run.Failure),
		}
	}
	if run := status.UnresolvedRun; run != nil {
		out.UnresolvedRun = &consolev1.UnresolvedRun{
			Outcome: string(run.Outcome), OperationId: run.OperationID, JobName: run.JobName,
			JobUid: string(run.JobUID), PlanName: run.PlanRef.Name, PlanUid: string(run.PlanRef.UID),
			RecordedAt: timestamp(&run.RecordedAt),
		}
	}
	if run := status.ResolvedRun; run != nil {
		resolved := &consolev1.ResolvedRun{
			OperationId: run.OperationID, Outcome: string(run.Outcome),
			Resolution: string(run.Resolution), ResolvedAt: timestamp(&run.ResolvedAt),
		}
		if run.AcknowledgedBy != nil {
			resolved.AcknowledgedBy = run.AcknowledgedBy.Username
		}
		out.ResolvedRun = resolved
	}
	if operation := status.ActiveOperation; operation != nil {
		out.ActiveOperation = &consolev1.Operation{
			Type: string(operation.Type), Id: operation.ID, JobName: operation.JobName,
			JobUid: string(operation.JobUID), StartedAt: timestamp(&operation.StartedAt), Attempt: operation.Attempt,
		}
	}
	out.Stages = migrationStages(migration, c)
	out.Attention = migrationAttention(migration, c)
	for _, attention := range out.Attention {
		attention.Command = ptahCommand("migration", migration.Name, migration.Namespace)
	}
	return out
}

func migrationStages(migration *operatorv1alpha1.PtahMigration, c conditions) []*consolev1.Stage {
	status := migration.Status
	ready := c.is(operatorv1alpha1.ConditionMigrationReady, metav1.ConditionTrue)
	return []*consolev1.Stage{
		migrationResolve(migration, c),
		migrationReadHistory(status, c),
		migrationPlan(status, ready),
		migrationApprove(c, ready),
		migrationApply(status, ready),
		migrationVerify(status, c, ready),
	}
}

func migrationResolve(migration *operatorv1alpha1.PtahMigration, c conditions) *consolev1.Stage {
	status := migration.Status
	if source := migrationSource(migration, c); unverifiedSource(source) {
		// A ConfigMap or Git source has no verification policy: the artifact
		// binding the history read takes is the whole of Resolve.
		switch {
		case source.GetResolved() != "":
			return stage(StageResolve, consolev1.StageState_STAGE_STATE_DONE, resolvedDetail(source))
		case status.Phase == operatorv1alpha1.MigrationPhaseResolving:
			return stage(StageResolve, consolev1.StageState_STAGE_STATE_ACTIVE, "")
		default:
			return stage(StageResolve, consolev1.StageState_STAGE_STATE_WAITING, "")
		}
	}
	switch {
	case c.is(operatorv1alpha1.ConditionArtifactVerified, metav1.ConditionFalse):
		return stage(StageResolve, consolev1.StageState_STAGE_STATE_FAILED, c.message(operatorv1alpha1.ConditionArtifactVerified))
	case c.is(operatorv1alpha1.ConditionArtifactVerified, metav1.ConditionTrue):
		detail := ""
		if status.Artifact != nil {
			detail = status.Artifact.ResolvedReference
		}
		return stage(StageResolve, consolev1.StageState_STAGE_STATE_DONE, detail)
	case status.Phase == operatorv1alpha1.MigrationPhaseResolving || status.Phase == operatorv1alpha1.MigrationPhaseVerifying ||
		c.is(operatorv1alpha1.ConditionArtifactVerified, metav1.ConditionUnknown):
		return stage(StageResolve, consolev1.StageState_STAGE_STATE_ACTIVE, "")
	default:
		return stage(StageResolve, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

func migrationReadHistory(status operatorv1alpha1.PtahMigrationStatus, c conditions) *consolev1.Stage {
	reason := c.reason(operatorv1alpha1.ConditionMigrationBlocked)
	switch {
	case c.is(operatorv1alpha1.ConditionMigrationBlocked, metav1.ConditionTrue) && historyBlockers[reason] != "":
		return stage(StageReadHistory, consolev1.StageState_STAGE_STATE_FAILED, historyBlockers[reason])
	case status.Phase == operatorv1alpha1.MigrationPhaseReading:
		return stage(StageReadHistory, consolev1.StageState_STAGE_STATE_ACTIVE, "")
	case status.History != nil:
		return stage(StageReadHistory, consolev1.StageState_STAGE_STATE_DONE, fmt.Sprintf("at version %d", status.History.CurrentVersion))
	default:
		return stage(StageReadHistory, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

func migrationPlan(status operatorv1alpha1.PtahMigrationStatus, ready bool) *consolev1.Stage {
	switch {
	case status.Plan != nil:
		pending := ""
		if status.History != nil {
			pending = fmt.Sprintf("%d pending", status.History.PendingCount)
		}
		return stage(StagePlan, consolev1.StageState_STAGE_STATE_DONE, pending)
	case ready:
		return stage(StagePlan, consolev1.StageState_STAGE_STATE_DONE, "nothing pending")
	case status.Phase == operatorv1alpha1.MigrationPhasePlanning:
		return stage(StagePlan, consolev1.StageState_STAGE_STATE_ACTIVE, "")
	default:
		return stage(StagePlan, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

func migrationApprove(c conditions, ready bool) *consolev1.Stage {
	switch {
	case c.is(operatorv1alpha1.ConditionMigrationApprovalRequired, metav1.ConditionTrue):
		return stage(StageApprove, consolev1.StageState_STAGE_STATE_WAITING, c.message(operatorv1alpha1.ConditionMigrationApprovalRequired))
	case ready, c.reason(operatorv1alpha1.ConditionMigrationApprovalRequired) == "Satisfied":
		return stage(StageApprove, consolev1.StageState_STAGE_STATE_DONE, "")
	default:
		return stage(StageApprove, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

func migrationApply(status operatorv1alpha1.PtahMigrationStatus, ready bool) *consolev1.Stage {
	switch {
	case unresolved(status):
		return stage(StageApply, consolev1.StageState_STAGE_STATE_UNKNOWN, "a run ended without proof of what it applied")
	case status.ActiveOperation != nil && status.ActiveOperation.Type == operatorv1alpha1.MigrationOperationApply:
		return stage(StageApply, consolev1.StageState_STAGE_STATE_ACTIVE, "")
	case failedRun(status.LastRun) && !settled(status) && !ready:
		return stage(StageApply, consolev1.StageState_STAGE_STATE_FAILED, status.LastRun.Message)
	case ready:
		return stage(StageApply, consolev1.StageState_STAGE_STATE_DONE, "")
	default:
		return stage(StageApply, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

func migrationVerify(status operatorv1alpha1.PtahMigrationStatus, c conditions, ready bool) *consolev1.Stage {
	switch {
	case ready:
		return stage(StageVerifyHistory, consolev1.StageState_STAGE_STATE_DONE, c.message(operatorv1alpha1.ConditionMigrationReady))
	case status.Phase == operatorv1alpha1.MigrationPhaseVerifyingHistory:
		return stage(StageVerifyHistory, consolev1.StageState_STAGE_STATE_ACTIVE, "")
	default:
		return stage(StageVerifyHistory, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

// settled says a person, or the operator, accounted for the last run after
// it ended. The operator holds one run unresolved at a time and records its
// resolution, so a last run that ended before the resolution is the one that
// was settled; a run that ended after it is a new failure.
func settled(status operatorv1alpha1.PtahMigrationStatus) bool {
	run, resolved := status.LastRun, status.ResolvedRun
	if run == nil || resolved == nil {
		return false
	}
	ended := run.StartedAt.Time
	if run.FinishedAt != nil {
		ended = run.FinishedAt.Time
	}
	return !resolved.ResolvedAt.Time.Before(ended)
}

// unresolved is a run the migration recorded as unresolved and that nothing
// has settled since.
func unresolved(status operatorv1alpha1.PtahMigrationStatus) bool {
	run := status.UnresolvedRun
	if run == nil {
		return false
	}
	return status.ResolvedRun == nil || status.ResolvedRun.OperationID != run.OperationID
}

func migrationAttention(migration *operatorv1alpha1.PtahMigration, c conditions) []*consolev1.Attention {
	status := migration.Status
	current := statusCurrent(migration.Generation, status.ObservedGeneration)
	var out []*consolev1.Attention
	add := func(kind consolev1.AttentionKind, reason, next, condition string) {
		out = append(out, &consolev1.Attention{Kind: kind, Reason: reason, NextStep: next, Since: c.since(condition)})
	}
	if unresolved(status) {
		add(consolev1.AttentionKind_ATTENTION_KIND_UNRESOLVED_RUN,
			fmt.Sprintf("Run %s ended without proof of what it applied.", status.UnresolvedRun.OperationID),
			"Read the database's revision table. When you know what the run did, acknowledge exactly this run; the acknowledgment settles the run and does not verify the SQL.",
			operatorv1alpha1.ConditionMigrationReady)
	}
	blocked := false
	if reason := c.reason(operatorv1alpha1.ConditionMigrationBlocked); c.is(operatorv1alpha1.ConditionMigrationBlocked, metav1.ConditionTrue) && historyBlockers[reason] != "" {
		blocked = true
		why := historyBlockers[reason]
		if run := status.LastRun; failedRun(run) {
			why += " The last run said: " + run.Message
		}
		add(consolev1.AttentionKind_ATTENTION_KIND_BLOCKED, why,
			"The operator never repairs the revision table. Fix the database, for example with ptah migrations repair, and the next history read continues on its own.",
			operatorv1alpha1.ConditionMigrationBlocked)
	}
	// A failed run that left the history blocked is said once, as the block.
	if run := status.LastRun; !blocked && failedRun(run) && !unresolved(status) && !settled(status) &&
		!c.is(operatorv1alpha1.ConditionMigrationReady, metav1.ConditionTrue) {
		add(consolev1.AttentionKind_ATTENTION_KIND_FAILED, run.Message,
			"Fix the cause the run reported; the operator reads the history again at spec.interval and plans from there.",
			operatorv1alpha1.ConditionMigrationReady)
	}
	if c.is(operatorv1alpha1.ConditionMigrationApprovalRequired, metav1.ConditionTrue) {
		add(consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL,
			c.message(operatorv1alpha1.ConditionMigrationApprovalRequired),
			"Review the steps and approve them. Nothing runs until someone does.",
			operatorv1alpha1.ConditionMigrationApprovalRequired)
	}
	if migration.Spec.Suspend {
		add(consolev1.AttentionKind_ATTENTION_KIND_SUSPENDED,
			"Reconciliation is suspended; no new database operation starts.",
			"Resume it to let the operator continue.", operatorv1alpha1.ConditionMigrationProgressing)
	}
	if !current {
		add(consolev1.AttentionKind_ATTENTION_KIND_STALE_STATUS,
			"The status describes an older version of the spec.",
			"Wait for the operator to reconcile the latest change.", operatorv1alpha1.ConditionMigrationReady)
	}
	return out
}

func failedRun(run *operatorv1alpha1.MigrationRunStatus) bool {
	return run != nil && (run.Outcome == operatorv1alpha1.MigrationRunOutcomeFailed || run.Outcome == operatorv1alpha1.MigrationRunOutcomePartial)
}
