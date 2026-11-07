package decisions_test

import (
	"bytes"
	"encoding/json"
	"testing"

	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"github.com/stokaro/ptah-operator/pkg/planread/planreadtest"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/audit"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// storePlan stores a readable plan for the schema, as the operator's store
// does: the manifest, its chunks, the commit in the plan's status and the
// schema's status naming it. The operator's own lab writes the objects, with
// the UIDs its in-memory API server gave them; this API server gives its own.
func storePlan(t *testing.T, stored *operatorv1alpha1.PtahSchema) (*operatorv1alpha1.PtahSchemaPlan, []*operatorv1alpha1.PtahSchemaPlanChunk) {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	lab := planreadtest.NewLab(t, stored.Name)
	published, _ := lab.Publish(t, []string{"CREATE TABLE customers (id BIGINT PRIMARY KEY)"}, "v1")

	plan := &operatorv1alpha1.PtahSchemaPlan{
		ObjectMeta: metav1.ObjectMeta{Namespace: stored.Namespace, Name: published.Name, Labels: published.Labels},
		Spec:       published.Spec,
	}
	plan.Spec.SchemaRef.UID = stored.UID
	c.Assert(plane.Admin.Create(ctx, plan), qt.IsNil)

	var chunks []*operatorv1alpha1.PtahSchemaPlanChunk
	committed := make([]operatorv1alpha1.PublishedPlanChunkStatus, 0, len(plan.Spec.Chunks))
	for _, ref := range plan.Spec.Chunks {
		written := &operatorv1alpha1.PtahSchemaPlanChunk{}
		c.Assert(lab.Client().Get(ctx, client.ObjectKey{Namespace: planreadtest.Namespace, Name: ref.Name}, written), qt.IsNil)
		chunk := &operatorv1alpha1.PtahSchemaPlanChunk{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: stored.Namespace, Name: written.Name, Labels: written.Labels, OwnerReferences: written.OwnerReferences,
			},
			Spec: written.Spec,
		}
		chunk.OwnerReferences[0].UID = plan.UID
		c.Assert(plane.Admin.Create(ctx, chunk), qt.IsNil)
		chunks = append(chunks, chunk)
		committed = append(committed, operatorv1alpha1.PublishedPlanChunkStatus{Name: chunk.Name, UID: chunk.UID, Index: ref.Index})
	}
	c.Assert(chunks, qt.Not(qt.HasLen), 0)

	plan.Status = published.Status
	plan.Status.ObservedGeneration = plan.Generation
	plan.Status.PublishedChunks = committed
	for i := range plan.Status.Conditions {
		plan.Status.Conditions[i].ObservedGeneration = plan.Generation
	}
	c.Assert(plane.Admin.Status().Update(ctx, plan), qt.IsNil)

	spec := plan.Spec
	stored.Status.Plan = &operatorv1alpha1.CurrentPlanStatus{
		Name: plan.Name, UID: plan.UID, Fingerprint: spec.Fingerprint, ContentDigest: spec.ContentDigest,
		ArtifactDigest: spec.ArtifactDigest, CoordinationDigest: spec.CoordinationDigest,
		TargetIdentityDigest:   spec.TargetIdentityDigest,
		ActualStateFingerprint: spec.ActualStateFingerprint, DesiredStateFingerprint: spec.DesiredStateFingerprint,
		PolicyFingerprint: spec.PolicyFingerprint, VerificationPolicyUID: spec.VerificationPolicyUID,
		VerificationPolicyDigest: spec.VerificationPolicyDigest, ExecutionBindingID: spec.ExecutionBindingID,
		ControllerImage: spec.ControllerImage, ControllerRevision: spec.ControllerRevision,
		ControllerStateVersion: spec.ControllerStateVersion, PtahVersion: spec.PtahVersion,
		ExecutorImage: spec.ExecutorImage, RunnerImage: spec.RunnerImage, RunnerProtocolVersion: spec.RunnerProtocolVersion,
		StatementCount: spec.StatementCount, CreatedAt: plan.CreationTimestamp,
	}
	c.Assert(plane.Admin.Status().Update(ctx, stored), qt.IsNil)
	return plan, chunks
}

// grant gives the user the verbs on the operator's resources in the namespace.
func grant(t *testing.T, namespace, user string, resources []string, verbs ...string) {
	t.Helper()
	c := qt.New(t)
	name := "grant-" + resources[0]
	c.Assert(plane.Admin.Create(t.Context(), &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{"operator.ptah.run"}, Resources: resources, Verbs: verbs}},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(t.Context(), &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: user}},
	}), qt.IsNil)
}

func auditLine(t *testing.T, log *bytes.Buffer) map[string]any {
	t.Helper()
	line := make(map[string]any)
	qt.New(t).Assert(json.Unmarshal(log.Bytes(), &line), qt.IsNil)
	return line
}

// An approval goes out only for a plan the person's own identity could read
// whole, on a real API server with real RBAC. The API server's own answer to
// the approval that does go out shows it left the console: no operator runs
// here, so no webhook stamps it and the CRD's schema refuses it.
func TestAnApprovalWaitsForThePlanReadOnARealAPIServer(t *testing.T) {
	t.Parallel()
	const approvals, chunksResource = "ptahschemaapprovals", "ptahschemaplanchunks"
	tests := []struct {
		name    string
		user    string
		arrange func(t *testing.T, namespace string, chunks []*operatorv1alpha1.PtahSchemaPlanChunk, user string)
		want    consolev1.DecisionOutcome
		says    string
		audited string
		reached bool
	}{
		{
			name: "may approve and may not read the chunks", user: "signer",
			arrange: func(t *testing.T, namespace string, _ []*operatorv1alpha1.PtahSchemaPlanChunk, user string) {
				grant(t, namespace, user, []string{approvals}, "create", "get")
			},
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, says: "may not read the whole plan", audited: audit.Refused,
		},
		{
			name: "may read and approve, and a chunk is gone", user: "approver-of-a-broken-plan",
			arrange: func(t *testing.T, namespace string, chunks []*operatorv1alpha1.PtahSchemaPlanChunk, user string) {
				grant(t, namespace, user, []string{approvals}, "create", "get")
				grant(t, namespace, user, []string{chunksResource}, "get")
				qt.New(t).Assert(plane.Admin.Delete(t.Context(), chunks[0]), qt.IsNil)
			},
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, says: "failed verification", audited: audit.Refused,
		},
		{
			name: "may read and not approve", user: "plan-reader",
			arrange: func(t *testing.T, namespace string, _ []*operatorv1alpha1.PtahSchemaPlanChunk, user string) {
				grant(t, namespace, user, []string{chunksResource}, "get")
			},
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_FORBIDDEN, says: `cannot create resource "ptahschemaapprovals"`, audited: audit.Forbidden,
			reached: true,
		},
		{
			name: "may read and approve a whole plan", user: "approver-of-a-whole-plan",
			arrange: func(t *testing.T, namespace string, _ []*operatorv1alpha1.PtahSchemaPlanChunk, user string) {
				grant(t, namespace, user, []string{approvals}, "create", "get")
				grant(t, namespace, user, []string{chunksResource}, "get")
			},
			want: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, says: "spec.approvedAt", audited: audit.Refused,
			reached: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			namespace, stored := arrange(t, test.user)
			plan, chunks := storePlan(t, stored)
			test.arrange(t, namespace, chunks, test.user)
			decisions, ctx, log := service(t, test.user)

			response, err := decisions.ApproveSchemaPlan(ctx, &consolev1.ApproveSchemaPlanRequest{
				Namespace: namespace, SchemaName: stored.Name, SchemaUid: string(stored.UID),
				PlanName: plan.Name, PlanUid: string(plan.UID), PlanFingerprint: plan.Spec.Fingerprint,
				ContractVersion: plan.Spec.ContractVersion,
			})

			c.Assert(err, qt.IsNil)
			c.Assert(response.GetResult().GetOutcome(), qt.Equals, test.want, qt.Commentf("%s", response.GetResult().GetMessage()))
			c.Assert(response.GetResult().GetMessage(), qt.Contains, test.says)
			kept := &operatorv1alpha1.PtahSchemaApprovalList{}
			c.Assert(plane.Admin.List(t.Context(), kept, client.InNamespace(namespace)), qt.IsNil)
			c.Assert(kept.Items, qt.HasLen, 0)
			line := auditLine(t, log)
			c.Assert(line["outcome"], qt.Equals, test.audited)
			// Only a create the API server answered carries its audit ID.
			c.Assert(line["auditID"] != "", qt.Equals, test.reached)
		})
	}
}
