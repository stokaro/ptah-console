package model

import (
	"fmt"

	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// Schema stage names.
const (
	StageResolve = "Resolve"
	StagePlan    = "Plan"
	StageApprove = "Approve"
	StageApply   = "Apply"
	StageVerify  = "Verify"
)

// Schema turns a PtahSchema into the console's message.
func Schema(schema *operatorv1alpha1.PtahSchema) *consolev1.Schema {
	status := schema.Status
	c := index(status.Conditions)
	out := &consolev1.Schema{
		Ref:                          objectRef(schema.ObjectMeta),
		Phase:                        string(status.Phase),
		StatusCurrent:                statusCurrent(schema.Generation, status.ObservedGeneration),
		Suspended:                    schema.Spec.Suspend,
		ApplyPolicy:                  string(schema.Spec.Policy.Apply),
		AllowDestructive:             schema.Spec.Policy.AllowDestructive,
		Conditions:                   toConditions(status.Conditions),
		Source:                       schemaSource(schema),
		Target:                       schemaTarget(schema),
		LastAttempt:                  timestamp(status.LastAttemptTime),
		LastSuccessfulReconciliation: timestamp(status.LastSuccessfulReconciliation),
		NextReconciliation:           timestamp(status.NextReconciliationTime),
		Gitops:                       gitOps(schema.ObjectMeta),
	}
	if plan := status.Plan; plan != nil {
		out.CurrentPlan = &consolev1.SchemaPlanSummary{
			Name:             plan.Name,
			Uid:              string(plan.UID),
			Fingerprint:      plan.Fingerprint,
			ArtifactDigest:   plan.ArtifactDigest,
			Destructive:      plan.Destructive,
			PrivilegeChanges: privilegeChanges(plan.PrivilegeChanges),
			StatementCount:   plan.StatementCount,
			CreatedAt:        timestamp(&plan.CreatedAt),
		}
		if approval := plan.Approval; approval != nil {
			out.CurrentPlan.Approval = &consolev1.ConsumedApproval{
				Name: approval.Name, Uid: string(approval.UID),
				Approver: approval.Approver.Username, ApprovedAt: timestamp(&approval.ApprovedAt),
			}
		}
	}
	if applied := status.Applied; applied != nil {
		out.Applied = &consolev1.SchemaApplied{
			PlanName:        applied.PlanRef.Name,
			PlanUid:         string(applied.PlanRef.UID),
			PlanFingerprint: applied.PlanFingerprint,
			ArtifactDigest:  applied.ArtifactDigest,
			CompletedAt:     timestamp(&applied.CompletedAt),
		}
	}
	if operation := status.ActiveOperation; operation != nil {
		out.ActiveOperation = &consolev1.Operation{
			Type: string(operation.Type), Id: operation.ID, JobName: operation.JobName,
			JobUid: string(operation.JobUID), StartedAt: timestamp(&operation.StartedAt), Attempt: operation.Attempt,
		}
	}
	if pending := status.PendingObservation; pending != nil {
		out.PendingObservation = &consolev1.PendingObservation{
			Outcome: string(pending.Outcome), ApplyJobName: pending.ApplyJobName,
			ApplyJobUid: string(pending.ApplyJobUID), ObserveAfter: timestamp(pending.ObserveAfter),
		}
	}
	out.Stages = schemaStages(schema, c)
	out.Attention = schemaAttention(schema, c)
	return out
}

func schemaSource(schema *operatorv1alpha1.PtahSchema) *consolev1.Source {
	source := schema.Status.Source
	return &consolev1.Source{
		Requested:  firstOf(source.RequestedReference, schema.Spec.Desired.OCIRef),
		Resolved:   source.ResolvedReference,
		Digest:     source.Digest,
		Verified:   source.Verified,
		ResolvedAt: timestamp(source.ResolvedAt),
		VerifiedAt: timestamp(source.VerifiedAt),
	}
}

func schemaTarget(schema *operatorv1alpha1.PtahSchema) *consolev1.Target {
	target := schema.Status.Target
	out := &consolev1.Target{
		Engine:               string(schema.Spec.Target.Engine),
		SecretName:           schema.Spec.Target.URLFrom.Name,
		SecretKey:            schema.Spec.Target.URLFrom.Key,
		LastObservedAt:       timestamp(target.LastObservedAt),
		HighestDriftSeverity: target.HighestDriftSeverity,
		DriftFindingCount:    target.DriftFindingCount,
	}
	if realm := schema.Spec.Target.RealmRef; realm != nil {
		out.Realm = realm.Name
	}
	for _, finding := range target.DriftFindings {
		out.DriftFindings = append(out.DriftFindings, &consolev1.DriftFinding{
			Category: finding.Category, Count: finding.Count, Severity: finding.Severity,
		})
	}
	return out
}

func privilegeChanges(changes []operatorv1alpha1.PrivilegeChange) []string {
	out := make([]string, 0, len(changes))
	for _, change := range changes {
		out = append(out, string(change))
	}
	return out
}

func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// planBlockers are the PlanReady reasons that hold a schema until a person
// changes something; the others clear on their own.
var planBlockers = map[string]string{
	"ProtectedTable":             "The artifact changes a table spec.policy.protectedTables fences off.",
	"DestructiveChangesDisabled": "The plan drops data and spec.policy.allowDestructive is false.",
	"PolicyBlocked":              "The destructive-change policy blocks the plan.",
	"ArtifactUnverified":         "The artifact did not pass verification.",
}

func schemaStages(schema *operatorv1alpha1.PtahSchema, c conditions) []*consolev1.Stage {
	status := schema.Status
	inSync := c.is(operatorv1alpha1.ConditionInSync, metav1.ConditionTrue)
	return []*consolev1.Stage{
		schemaResolve(schema, c),
		schemaPlan(status, c, inSync),
		schemaApprove(status, c, inSync),
		schemaApply(status, c, inSync),
		schemaVerify(status, c, inSync),
	}
}

func schemaResolve(schema *operatorv1alpha1.PtahSchema, c conditions) *consolev1.Stage {
	status := schema.Status
	switch {
	case c.is(operatorv1alpha1.ConditionArtifactResolved, metav1.ConditionFalse):
		return stage(StageResolve, consolev1.StageState_STAGE_STATE_FAILED, c.message(operatorv1alpha1.ConditionArtifactResolved))
	case c.is(operatorv1alpha1.ConditionArtifactVerified, metav1.ConditionFalse) &&
		c.reason(operatorv1alpha1.ConditionArtifactVerified) != "Pending":
		return stage(StageResolve, consolev1.StageState_STAGE_STATE_FAILED, c.message(operatorv1alpha1.ConditionArtifactVerified))
	case c.is(operatorv1alpha1.ConditionArtifactResolved, metav1.ConditionTrue) &&
		c.is(operatorv1alpha1.ConditionArtifactVerified, metav1.ConditionTrue):
		return stage(StageResolve, consolev1.StageState_STAGE_STATE_DONE, status.Source.ResolvedReference)
	case status.Phase == operatorv1alpha1.PhaseResolving || status.Phase == operatorv1alpha1.PhaseVerifying ||
		c.is(operatorv1alpha1.ConditionArtifactResolved, metav1.ConditionUnknown):
		return stage(StageResolve, consolev1.StageState_STAGE_STATE_ACTIVE, "")
	default:
		return stage(StageResolve, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

func schemaPlan(status operatorv1alpha1.PtahSchemaStatus, c conditions, inSync bool) *consolev1.Stage {
	reason := c.reason(operatorv1alpha1.ConditionPlanReady)
	switch {
	case inSync:
		return stage(StagePlan, consolev1.StageState_STAGE_STATE_DONE, "nothing left to change")
	case c.is(operatorv1alpha1.ConditionPlanReady, metav1.ConditionTrue) && status.Plan != nil:
		return stage(StagePlan, consolev1.StageState_STAGE_STATE_DONE, statements(status.Plan.StatementCount))
	case c.is(operatorv1alpha1.ConditionPlanReady, metav1.ConditionFalse) && planBlockers[reason] != "":
		return stage(StagePlan, consolev1.StageState_STAGE_STATE_FAILED, planBlockers[reason])
	case status.Phase == operatorv1alpha1.PhaseObserving || status.Phase == operatorv1alpha1.PhasePlanning:
		return stage(StagePlan, consolev1.StageState_STAGE_STATE_ACTIVE, "")
	default:
		return stage(StagePlan, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

func statements(count int32) string {
	if count == 1 {
		return "1 statement"
	}
	return fmt.Sprintf("%d statements", count)
}

func schemaApprove(status operatorv1alpha1.PtahSchemaStatus, c conditions, inSync bool) *consolev1.Stage {
	reason := c.reason(operatorv1alpha1.ConditionApprovalRequired)
	switch {
	case status.Plan != nil && status.Plan.Approval != nil:
		return stage(StageApprove, consolev1.StageState_STAGE_STATE_DONE, "approved by "+status.Plan.Approval.Approver.Username)
	case c.is(operatorv1alpha1.ConditionApprovalRequired, metav1.ConditionTrue):
		return stage(StageApprove, consolev1.StageState_STAGE_STATE_WAITING, c.message(operatorv1alpha1.ConditionApprovalRequired))
	case reason == "NotRequired":
		return stage(StageApprove, consolev1.StageState_STAGE_STATE_NOT_APPLICABLE, "the policy needs no approval for this plan")
	case reason == "ApplyDisabled":
		return stage(StageApprove, consolev1.StageState_STAGE_STATE_NOT_APPLICABLE, "apply: Never records plans without applying them")
	case inSync, reason == "Satisfied", reason == "CurrentPlan":
		return stage(StageApprove, consolev1.StageState_STAGE_STATE_DONE, "")
	default:
		return stage(StageApprove, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

func schemaApply(status operatorv1alpha1.PtahSchemaStatus, c conditions, inSync bool) *consolev1.Stage {
	reason := c.reason(operatorv1alpha1.ConditionApplying)
	switch {
	case outcomeUnknown(status, c):
		return stage(StageApply, consolev1.StageState_STAGE_STATE_UNKNOWN, "the operator does not know whether the SQL committed")
	case c.is(operatorv1alpha1.ConditionApplying, metav1.ConditionTrue):
		return stage(StageApply, consolev1.StageState_STAGE_STATE_ACTIVE, "")
	case reason == "JobCompleted":
		return stage(StageApply, consolev1.StageState_STAGE_STATE_DONE, "the Job exited; the operator is checking the database")
	case reason == "ApplyDisabled" || c.reason(operatorv1alpha1.ConditionApprovalRequired) == "ApplyDisabled":
		return stage(StageApply, consolev1.StageState_STAGE_STATE_NOT_APPLICABLE, "apply: Never")
	case inSync && status.Applied != nil:
		return stage(StageApply, consolev1.StageState_STAGE_STATE_DONE, "")
	case inSync:
		return stage(StageApply, consolev1.StageState_STAGE_STATE_NOT_APPLICABLE, "nothing to apply")
	case c.is(operatorv1alpha1.ConditionReconciliationFailed, metav1.ConditionTrue) && status.Plan != nil && status.Plan.Approval != nil:
		return stage(StageApply, consolev1.StageState_STAGE_STATE_FAILED, c.message(operatorv1alpha1.ConditionReconciliationFailed))
	default:
		return stage(StageApply, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

func schemaVerify(status operatorv1alpha1.PtahSchemaStatus, c conditions, inSync bool) *consolev1.Stage {
	switch {
	case inSync:
		return stage(StageVerify, consolev1.StageState_STAGE_STATE_DONE, "a fresh read shows no differences")
	case status.Phase == operatorv1alpha1.PhaseVerifyingConvergence ||
		c.reason(operatorv1alpha1.ConditionReady) == "VerifyingConvergence":
		return stage(StageVerify, consolev1.StageState_STAGE_STATE_ACTIVE, "")
	default:
		return stage(StageVerify, consolev1.StageState_STAGE_STATE_WAITING, "")
	}
}

// outcomeUnknown is an Apply the operator cannot attribute, as the current
// status says. An Applying condition left from an older generation is not
// evidence of it: the operator rewrites ReconciliationFailed and the pending
// observation when it settles the outcome.
func outcomeUnknown(status operatorv1alpha1.PtahSchemaStatus, c conditions) bool {
	if status.PendingObservation != nil && status.PendingObservation.Outcome == operatorv1alpha1.PendingObservationOutcomeUnknown {
		return true
	}
	if c.is(operatorv1alpha1.ConditionReconciliationFailed, metav1.ConditionTrue) &&
		c.reason(operatorv1alpha1.ConditionReconciliationFailed) == "ApplyOutcomeUnknown" {
		return true
	}
	switch c.reason(operatorv1alpha1.ConditionApplying) {
	case "ApplyOutcomeUnknown", "OutcomeUnknown":
		return c.newest(operatorv1alpha1.ConditionApplying)
	}
	return false
}

func schemaAttention(schema *operatorv1alpha1.PtahSchema, c conditions) []*consolev1.Attention {
	status := schema.Status
	current := statusCurrent(schema.Generation, status.ObservedGeneration)
	var out []*consolev1.Attention
	add := func(kind consolev1.AttentionKind, reason, next, condition string) {
		out = append(out, &consolev1.Attention{Kind: kind, Reason: reason, NextStep: next, Since: c.since(condition)})
	}
	if outcomeUnknown(status, c) {
		add(consolev1.AttentionKind_ATTENTION_KIND_OUTCOME_UNKNOWN,
			"An Apply ended without proof of whether it changed the database.",
			"The operator reads the database again before anything else runs. Wait for that read; there is nothing to retry from the console.",
			operatorv1alpha1.ConditionReconciliationFailed)
	} else if c.is(operatorv1alpha1.ConditionReconciliationFailed, metav1.ConditionTrue) {
		add(consolev1.AttentionKind_ATTENTION_KIND_FAILED,
			c.message(operatorv1alpha1.ConditionReconciliationFailed),
			"The operator tries again after spec.execution.failureRetryInterval. The operation's Job and its log say what failed.",
			operatorv1alpha1.ConditionReconciliationFailed)
	}
	if c.is(operatorv1alpha1.ConditionApprovalRequired, metav1.ConditionTrue) {
		add(consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL,
			c.message(operatorv1alpha1.ConditionApprovalRequired),
			"Review the plan and approve it. Nothing changes in the database until someone does.",
			operatorv1alpha1.ConditionApprovalRequired)
	}
	if reason := c.reason(operatorv1alpha1.ConditionPlanReady); c.is(operatorv1alpha1.ConditionPlanReady, metav1.ConditionFalse) && planBlockers[reason] != "" {
		add(consolev1.AttentionKind_ATTENTION_KIND_BLOCKED, planBlockers[reason], schemaBlockedNext(reason), operatorv1alpha1.ConditionPlanReady)
	}
	switch reason := c.reason(operatorv1alpha1.ConditionReady); reason {
	case "RealmConflict", "RealmNotAuthorized", "UnsupportedEngine":
		add(consolev1.AttentionKind_ATTENTION_KIND_BLOCKED, c.message(operatorv1alpha1.ConditionReady), schemaBlockedNext(reason), operatorv1alpha1.ConditionReady)
	}
	if c.is(operatorv1alpha1.ConditionSuspended, metav1.ConditionTrue) || schema.Spec.Suspend {
		add(consolev1.AttentionKind_ATTENTION_KIND_SUSPENDED,
			"Reconciliation is suspended; no new database operation starts.",
			"Resume it to let the operator continue.", operatorv1alpha1.ConditionSuspended)
	}
	if len(out) == 0 && c.is(operatorv1alpha1.ConditionInSync, metav1.ConditionFalse) &&
		c.reason(operatorv1alpha1.ConditionInSync) == "ScopedChanges" && c.reason(operatorv1alpha1.ConditionApprovalRequired) == "ApplyDisabled" {
		add(consolev1.AttentionKind_ATTENTION_KIND_DRIFT,
			"The database differs from the artifact, and apply: Never only records the plan.",
			"Read the plan, then change the policy or the database.", operatorv1alpha1.ConditionInSync)
	}
	if !current {
		add(consolev1.AttentionKind_ATTENTION_KIND_STALE_STATUS,
			"The status describes an older version of the spec.",
			"Wait for the operator to reconcile the latest change.", operatorv1alpha1.ConditionReady)
	}
	return out
}

func schemaBlockedNext(reason string) string {
	switch reason {
	case "ProtectedTable":
		return "Take the table out of spec.policy.protectedTables, or express the change as a versioned migration."
	case "DestructiveChangesDisabled", "PolicyBlocked":
		return "Set spec.policy.allowDestructive if dropping the data is intended, or change the artifact."
	case "ArtifactUnverified":
		return "Publish an artifact the verification policy accepts, or correct the policy."
	case "RealmConflict":
		return "Give the database one owner, or set spec.target.sharedRealm and a PtahRealm that admits every claimant."
	case "RealmNotAuthorized":
		return "Ask a realm administrator to admit this namespace in the PtahRealm, or name another realm."
	case "UnsupportedEngine":
		return "Set spec.target.engine to PostgreSQL or MySQL."
	default:
		return ""
	}
}
