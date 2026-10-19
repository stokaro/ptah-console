package model

import (
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// SchemaApproval is a PtahSchemaApproval as a decision.
func SchemaApproval(approval *operatorv1alpha1.PtahSchemaApproval) *consolev1.Decision {
	spec := approval.Spec
	out := &consolev1.Decision{
		Kind:            consolev1.DecisionKind_DECISION_KIND_SCHEMA_APPROVAL,
		Ref:             objectRef(approval.ObjectMeta),
		OwnerName:       spec.SchemaRef.Name,
		OwnerUid:        string(spec.SchemaRef.UID),
		PlanName:        spec.PlanRef.Name,
		PlanUid:         string(spec.PlanRef.UID),
		PlanFingerprint: spec.PlanFingerprint,
		DecidedBy:       spec.Approver.Username,
		DecidedByGroups: spec.Approver.Groups,
		DecidedAt:       timestamp(&spec.ApprovedAt),
		Conditions:      toConditions(approval.Status.Conditions),
	}
	out.State, out.StateReason = decisionState(approval.Status.Conditions, true)
	return out
}

// MigrationApproval is a PtahMigrationApproval as a decision.
func MigrationApproval(approval *operatorv1alpha1.PtahMigrationApproval) *consolev1.Decision {
	spec := approval.Spec
	out := &consolev1.Decision{
		Kind:            consolev1.DecisionKind_DECISION_KIND_MIGRATION_APPROVAL,
		Ref:             objectRef(approval.ObjectMeta),
		OwnerName:       spec.MigrationRef.Name,
		OwnerUid:        string(spec.MigrationRef.UID),
		PlanName:        spec.PlanRef.Name,
		PlanUid:         string(spec.PlanRef.UID),
		PlanFingerprint: spec.PlanFingerprint,
		DecidedBy:       spec.Approver.Username,
		DecidedByGroups: spec.Approver.Groups,
		DecidedAt:       timestamp(&spec.ApprovedAt),
		Conditions:      toConditions(approval.Status.Conditions),
	}
	out.State, out.StateReason = decisionState(approval.Status.Conditions, false)
	return out
}

// RunAcknowledgment is a PtahMigrationRunAcknowledgment as a decision.
func RunAcknowledgment(acknowledgment *operatorv1alpha1.PtahMigrationRunAcknowledgment) *consolev1.Decision {
	spec := acknowledgment.Spec
	out := &consolev1.Decision{
		Kind:            consolev1.DecisionKind_DECISION_KIND_RUN_ACKNOWLEDGMENT,
		Ref:             objectRef(acknowledgment.ObjectMeta),
		OwnerName:       spec.MigrationRef.Name,
		OwnerUid:        string(spec.MigrationRef.UID),
		OperationId:     spec.OperationID,
		DecidedBy:       spec.AcknowledgedBy.Username,
		DecidedByGroups: spec.AcknowledgedBy.Groups,
		DecidedAt:       timestamp(&spec.AcknowledgedAt),
		Conditions:      toConditions(acknowledgment.Status.Conditions),
	}
	out.State, out.StateReason = decisionState(acknowledgment.Status.Conditions, false)
	return out
}

// decisionState reads the decision's conditions. A migration approval and an
// acknowledgment publish no Accepted condition; whether they matched is said
// on the migration.
func decisionState(list []metav1.Condition, accepts bool) (consolev1.DecisionState, string) {
	c := index(list)
	switch {
	case c.is(operatorv1alpha1.ConditionApprovalConsumed, metav1.ConditionTrue):
		return consolev1.DecisionState_DECISION_STATE_CONSUMED, c.message(operatorv1alpha1.ConditionApprovalConsumed)
	case c.is(operatorv1alpha1.ConditionApprovalStale, metav1.ConditionTrue):
		return consolev1.DecisionState_DECISION_STATE_STALE, c.message(operatorv1alpha1.ConditionApprovalStale)
	case accepts && c.is(operatorv1alpha1.ConditionApprovalAccepted, metav1.ConditionFalse):
		return consolev1.DecisionState_DECISION_STATE_REFUSED, c.message(operatorv1alpha1.ConditionApprovalAccepted)
	case accepts && c.is(operatorv1alpha1.ConditionApprovalAccepted, metav1.ConditionTrue):
		return consolev1.DecisionState_DECISION_STATE_ACCEPTED, c.message(operatorv1alpha1.ConditionApprovalAccepted)
	default:
		return consolev1.DecisionState_DECISION_STATE_PENDING, ""
	}
}
