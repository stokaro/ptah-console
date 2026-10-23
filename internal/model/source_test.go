package model_test

import (
	"testing"

	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/model"
)

// The fields below are the ones the operator writes for a ConfigMap and a Git
// source. Its acceptance runs record no full status document for either
// kind yet, so these are built from its API types rather than read.
const (
	commit        = "3f1c2b0a9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a"
	contentDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	policyDigest  = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	ociDigest     = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	snapshotName  = "ptah-source-0a1b2c3d4e5f6a7b8c9d0e1f"
)

func gitSpec() *operatorv1alpha1.GitSourceSpec {
	return &operatorv1alpha1.GitSourceSpec{
		URL: "https://git.example.com/acme/app.git", Ref: operatorv1alpha1.GitRef{Branch: "main"}, Path: "db/schema.hcl",
	}
}

func policyFrom() *corev1.ConfigMapKeySelector {
	return &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "team-sources"}, Key: "policy.yaml"}
}

// A Git or ConfigMap source has no verification policy: Resolve is done once
// its files are read, and the source says what was read, where from and under
// which policy.
func TestASchemaSourceWithoutVerificationResolvesOnceRead(t *testing.T) {
	t.Parallel()
	resolved := condition("ArtifactResolved", metav1.ConditionTrue, "CommitResolved")
	tests := []struct {
		name   string
		schema func() *operatorv1alpha1.PtahSchema
		want   *consolev1.Source
		state  consolev1.StageState
		detail string
	}{
		{
			name: "a Git commit",
			schema: func() *operatorv1alpha1.PtahSchema {
				schema := schemaWith(resolved)
				schema.Spec.Desired = operatorv1alpha1.SourceSpec{Git: gitSpec(), SourcePolicyFrom: policyFrom()}
				schema.Status.Source = operatorv1alpha1.SchemaSourceStatus{
					Git: &operatorv1alpha1.GitSourceRevision{
						URL: gitSpec().URL, Ref: gitSpec().Ref, Path: gitSpec().Path, Commit: commit, ContentDigest: contentDigest,
					},
					SourcePolicy: &operatorv1alpha1.SourcePolicyBinding{UID: "policy-uid", Digest: policyDigest},
				}
				return schema
			},
			want: &consolev1.Source{
				Kind: consolev1.SourceKind_SOURCE_KIND_GIT, Requested: gitSpec().URL, Ref: "branch main", Path: "db/schema.hcl",
				Resolved: commit, ContentDigest: contentDigest,
				Policy: &consolev1.SourcePolicy{Name: "team-sources", Uid: "policy-uid", Digest: policyDigest},
			},
			state: done, detail: "commit 3f1c2b0a9d8e",
		},
		{
			name: "a ConfigMap snapshot",
			schema: func() *operatorv1alpha1.PtahSchema {
				schema := schemaWith(condition("ArtifactResolved", metav1.ConditionTrue, "SnapshotTaken"))
				schema.Spec.Desired = operatorv1alpha1.SourceSpec{ConfigMap: &operatorv1alpha1.ConfigMapSourceSpec{Name: "app-schema"}}
				schema.Status.Source = operatorv1alpha1.SchemaSourceStatus{
					ConfigMap: &operatorv1alpha1.ConfigMapSourceRevision{Name: "app-schema", UID: "configmap-uid", ResourceVersion: "3"},
					Snapshot:  &operatorv1alpha1.SourceSnapshotBinding{Name: snapshotName, UID: "snapshot-uid", ContentDigest: contentDigest},
				}
				return schema
			},
			want: &consolev1.Source{
				Kind: consolev1.SourceKind_SOURCE_KIND_CONFIG_MAP, Requested: "app-schema", Resolved: snapshotName, ContentDigest: contentDigest,
			},
			state: done, detail: "snapshot " + snapshotName,
		},
		{
			// The source moved from OCI to Git and has not been read since:
			// what the status holds answers the earlier source.
			name: "a Git source whose status still holds an OCI resolution",
			schema: func() *operatorv1alpha1.PtahSchema {
				schema := schemaWith(resolved)
				schema.Spec.Desired = operatorv1alpha1.SourceSpec{Git: gitSpec()}
				schema.Status.Source = operatorv1alpha1.SchemaSourceStatus{
					RequestedReference: "oci://registry.example/team/schema:stable", Digest: ociDigest, Verified: true,
				}
				return schema
			},
			want:  &consolev1.Source{Kind: consolev1.SourceKind_SOURCE_KIND_GIT, Requested: gitSpec().URL, Ref: "branch main", Path: "db/schema.hcl"},
			state: waiting,
		},
		{
			name: "a Git source being read",
			schema: func() *operatorv1alpha1.PtahSchema {
				schema := schemaWith()
				schema.Spec.Desired = operatorv1alpha1.SourceSpec{Git: gitSpec()}
				schema.Status.Phase = operatorv1alpha1.PhaseResolving
				return schema
			},
			want:  &consolev1.Source{Kind: consolev1.SourceKind_SOURCE_KIND_GIT, Requested: gitSpec().URL, Ref: "branch main", Path: "db/schema.hcl"},
			state: active,
		},
		{
			name: "a ConfigMap that could not be read",
			schema: func() *operatorv1alpha1.PtahSchema {
				failure := condition("ArtifactResolved", metav1.ConditionFalse, "ResolveFailed")
				failure.Message = "The ConfigMap source could not be read"
				schema := schemaWith(failure)
				schema.Spec.Desired = operatorv1alpha1.SourceSpec{ConfigMap: &operatorv1alpha1.ConfigMapSourceSpec{Name: "app-schema"}}
				return schema
			},
			want:  &consolev1.Source{Kind: consolev1.SourceKind_SOURCE_KIND_CONFIG_MAP, Requested: "app-schema"},
			state: failed, detail: "The ConfigMap source could not be read",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			out := model.Schema(test.schema())

			c.Assert(out.GetSource(), qt.CmpEquals(protocmp.Transform()), test.want)
			resolve := out.GetStages()[0]
			c.Assert(resolve.GetName(), qt.Equals, model.StageResolve)
			c.Assert(resolve.GetState(), qt.Equals, test.state)
			c.Assert(resolve.GetDetail(), qt.Equals, test.detail)
		})
	}
}

// The same for a migration, whose Resolve is the artifact binding the history
// read takes.
func TestAMigrationSourceWithoutVerificationResolvesOnceRead(t *testing.T) {
	t.Parallel()
	gitMigration := func() *operatorv1alpha1.GitSourceSpec {
		spec := gitSpec()
		spec.Path = "db/migrations"
		return spec
	}
	tests := []struct {
		name   string
		spec   operatorv1alpha1.SourceSpec
		status operatorv1alpha1.PtahMigrationStatus
		want   *consolev1.Source
		state  consolev1.StageState
		detail string
	}{
		{
			name: "a Git commit",
			spec: operatorv1alpha1.SourceSpec{Git: gitMigration(), SourcePolicyFrom: policyFrom()},
			status: operatorv1alpha1.PtahMigrationStatus{
				Phase: operatorv1alpha1.MigrationPhaseReading,
				Artifact: &operatorv1alpha1.SourceAccessBinding{Git: &operatorv1alpha1.GitSourceBinding{
					URL: gitSpec().URL, Ref: gitSpec().Ref, Path: "db/migrations", Commit: commit, ContentDigest: contentDigest,
				}},
				SourcePolicy: &operatorv1alpha1.SourcePolicyBinding{UID: "policy-uid", Digest: policyDigest},
			},
			want: &consolev1.Source{
				Kind: consolev1.SourceKind_SOURCE_KIND_GIT, Requested: gitSpec().URL, Ref: "branch main", Path: "db/migrations",
				Resolved: commit, ContentDigest: contentDigest,
				Policy: &consolev1.SourcePolicy{Name: "team-sources", Uid: "policy-uid", Digest: policyDigest},
			},
			state: done, detail: "commit 3f1c2b0a9d8e",
		},
		{
			name: "a ConfigMap snapshot",
			spec: operatorv1alpha1.SourceSpec{ConfigMap: &operatorv1alpha1.ConfigMapSourceSpec{Name: "orders-migrations"}},
			status: operatorv1alpha1.PtahMigrationStatus{
				Phase:     operatorv1alpha1.MigrationPhaseReading,
				Artifact:  &operatorv1alpha1.SourceAccessBinding{Snapshot: &operatorv1alpha1.SourceSnapshotBinding{Name: snapshotName, UID: "snapshot-uid", ContentDigest: contentDigest}},
				ConfigMap: &operatorv1alpha1.ConfigMapSourceRevision{Name: "orders-migrations", UID: "configmap-uid", ResourceVersion: "3"},
			},
			want: &consolev1.Source{
				Kind: consolev1.SourceKind_SOURCE_KIND_CONFIG_MAP, Requested: "orders-migrations", Resolved: snapshotName, ContentDigest: contentDigest,
			},
			state: done, detail: "snapshot " + snapshotName,
		},
		{
			name: "a Git source whose status still holds an OCI artifact",
			spec: operatorv1alpha1.SourceSpec{Git: gitMigration()},
			status: operatorv1alpha1.PtahMigrationStatus{
				Phase:    operatorv1alpha1.MigrationPhaseInSync,
				Artifact: &operatorv1alpha1.SourceAccessBinding{ResolvedReference: "oci://registry.example/team/migrations@" + ociDigest, Digest: ociDigest},
			},
			want:  &consolev1.Source{Kind: consolev1.SourceKind_SOURCE_KIND_GIT, Requested: gitSpec().URL, Ref: "branch main", Path: "db/migrations"},
			state: waiting,
		},
		{
			name:   "a Git source being read",
			spec:   operatorv1alpha1.SourceSpec{Git: gitMigration()},
			status: operatorv1alpha1.PtahMigrationStatus{Phase: operatorv1alpha1.MigrationPhaseResolving},
			want:   &consolev1.Source{Kind: consolev1.SourceKind_SOURCE_KIND_GIT, Requested: gitSpec().URL, Ref: "branch main", Path: "db/migrations"},
			state:  active,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			migration := migrationWith(test.status)
			migration.Spec.Artifact = test.spec

			out := model.Migration(migration)

			c.Assert(out.GetSource(), qt.CmpEquals(protocmp.Transform()), test.want)
			resolve := out.GetStages()[0]
			c.Assert(resolve.GetName(), qt.Equals, model.StageResolve)
			c.Assert(resolve.GetState(), qt.Equals, test.state)
			c.Assert(resolve.GetDetail(), qt.Equals, test.detail)
		})
	}
}

// An OCI source keeps its own reading: verified, with the manifest digest,
// and no content digest or policy.
func TestAnOCISourceIsReadAsBefore(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	schema := schemaWith(
		condition("ArtifactResolved", metav1.ConditionTrue, "DigestPinned"),
		condition("ArtifactVerified", metav1.ConditionTrue, "PolicySatisfied"),
	)
	schema.Spec.Desired = operatorv1alpha1.SourceSpec{OCIRef: "oci://registry.example/team/schema:stable"}
	schema.Status.Source = operatorv1alpha1.SchemaSourceStatus{
		RequestedReference: "oci://registry.example/team/schema:stable",
		ResolvedReference:  "oci://registry.example/team/schema@" + ociDigest, Digest: ociDigest, Verified: true,
	}

	out := model.Schema(schema)

	c.Assert(out.GetSource(), qt.CmpEquals(protocmp.Transform()), &consolev1.Source{
		Kind: consolev1.SourceKind_SOURCE_KIND_OCI, Requested: "oci://registry.example/team/schema:stable",
		Resolved: "oci://registry.example/team/schema@" + ociDigest, Digest: ociDigest, Verified: true,
	})
	c.Assert(out.GetStages()[0].GetState(), qt.Equals, done)
}

// A plan computed from a ConfigMap or Git source is bound to the revision,
// the content and the policy, and the console reads its contract.
func TestAPlanBoundToASourceSaysWhatItIsBoundTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		contract  int32
		source    *operatorv1alpha1.PlanSourceBinding
		want      *consolev1.PlanSource
		supported bool
	}{
		{name: "an OCI artifact", contract: 1, supported: true},
		{
			name: "a Git commit under a policy", contract: 2, supported: true,
			source: &operatorv1alpha1.PlanSourceBinding{
				Kind: operatorv1alpha1.SourceKindGit, Revision: commit, ContentDigest: contentDigest,
				Policy: &operatorv1alpha1.SourcePolicyBinding{UID: "policy-uid", Digest: policyDigest},
			},
			want: &consolev1.PlanSource{
				Kind: consolev1.SourceKind_SOURCE_KIND_GIT, Revision: commit, ContentDigest: contentDigest,
				PolicyUid: "policy-uid", PolicyDigest: policyDigest,
			},
		},
		{
			name: "a ConfigMap", contract: 2, supported: true,
			source: &operatorv1alpha1.PlanSourceBinding{Kind: operatorv1alpha1.SourceKindConfigMap, Revision: "configmap-uid", ContentDigest: contentDigest},
			want:   &consolev1.PlanSource{Kind: consolev1.SourceKind_SOURCE_KIND_CONFIG_MAP, Revision: "configmap-uid", ContentDigest: contentDigest},
		},
		{name: "a contract this build does not read", contract: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			plan := &operatorv1alpha1.PtahSchemaPlan{Spec: operatorv1alpha1.PtahSchemaPlanSpec{ContractVersion: test.contract, Source: test.source}}

			out := model.SchemaPlanManifest(plan, consolev1.PlanSelection_PLAN_SELECTION_CURRENT)

			c.Assert(out.GetSource(), qt.CmpEquals(protocmp.Transform()), test.want)
			c.Assert(out.GetContractSupported(), qt.Equals, test.supported)
			migration := model.MigrationPlan(&operatorv1alpha1.PtahMigrationPlan{Spec: operatorv1alpha1.PtahMigrationPlanSpec{Source: test.source}})
			c.Assert(migration.GetSource(), qt.CmpEquals(protocmp.Transform()), test.want)
		})
	}
}
