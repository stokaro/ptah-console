package model_test

import (
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/model"
)

// The fields below are the ones the operator writes for a ConfigMap, a Git
// and an HTTPS source. Its acceptance runs record no full status document of
// a migration for any of them, nor of a schema for the first two, so these
// are built from its API types rather than read.
const (
	commit         = "3f1c2b0a9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a"
	contentDigest  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	policyDigest   = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	ociDigest      = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	downloadDigest = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
	snapshotName   = "ptah-source-0a1b2c3d4e5f6a7b8c9d0e1f"
)

func gitSpec() *operatorv1alpha1.GitSourceSpec {
	return &operatorv1alpha1.GitSourceSpec{
		URL: "https://git.example.com/acme/app.git", Ref: operatorv1alpha1.GitRef{Branch: "main"}, Path: "db/schema.hcl",
	}
}

func policyFrom() *corev1.ConfigMapKeySelector {
	return &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "team-sources"}, Key: "policy.yaml"}
}

func httpsMigration() *operatorv1alpha1.HTTPSSourceSpec {
	return &operatorv1alpha1.HTTPSSourceSpec{
		URL: "https://downloads.example/acme/migrations.tar.gz", Digest: downloadDigest,
		Format: operatorv1alpha1.HTTPSFormatTarGzip, Path: "db/migrations",
	}
}

// A schema with an HTTPS source, as the operator's https-ca-pin-refused row
// read it once the source selected its CA: the download resolved, no
// ArtifactVerified, and a plan waiting for an approval. The reading is a
// status, so the spec is the one that status resolved. A console that read
// the source as an OCI artifact waited for a verification the operator never
// writes, and left Resolve waiting.
func TestASchemaWithAnHTTPSSourceResolvesOnceTheDownloadMatched(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	var schema operatorv1alpha1.PtahSchema
	reading(t, "https-ca-pin-recovered-schema.json", &schema)
	asWritten(&schema.ObjectMeta, schema.Status.ObservedGeneration)
	download := schema.Status.Source.HTTPS
	c.Assert(download, qt.IsNotNil)
	c.Assert(apimeta.FindStatusCondition(schema.Status.Conditions, "ArtifactVerified"), qt.IsNil)
	schema.Spec.Desired = operatorv1alpha1.SourceSpec{HTTPS: &operatorv1alpha1.HTTPSSourceSpec{
		URL: download.URL, Digest: download.Digest, Format: download.Format, Path: download.Path,
	}}

	out := model.Schema(&schema)

	c.Assert(out.GetSource(), qt.CmpEquals(protocmp.Transform()), &consolev1.Source{
		Kind: consolev1.SourceKind_SOURCE_KIND_HTTPS, Requested: download.URL, Path: "schema.sql", Format: "File",
		Digest:        "sha256:74c65b2a7497ceacb00e7fb087c38062e92c3f6399ce980247de0a826f7efad4",
		Resolved:      "sha256:74c65b2a7497ceacb00e7fb087c38062e92c3f6399ce980247de0a826f7efad4",
		ResolvedAt:    timestamppb.New(schema.Status.Source.ResolvedAt.Time),
		ContentDigest: "sha256:479db95d1e119641e67a5592f9e977a1c600ebbf7cf1a3121b7ef4063d1ac221",
	})
	c.Assert(states(out.GetStages()), qt.DeepEquals, []consolev1.StageState{done, done, waiting, waiting, waiting})
	c.Assert(out.GetStages()[0].GetDetail(), qt.Equals, "download sha256:74c65b2a7497")
	c.Assert(kinds(out.GetAttention()), qt.DeepEquals, []consolev1.AttentionKind{consolev1.AttentionKind_ATTENTION_KIND_WAITING_FOR_APPROVAL})
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
		{
			name: "an HTTPS download",
			spec: operatorv1alpha1.SourceSpec{HTTPS: httpsMigration(), SourcePolicyFrom: policyFrom()},
			status: operatorv1alpha1.PtahMigrationStatus{
				Phase: operatorv1alpha1.MigrationPhaseReading,
				Artifact: &operatorv1alpha1.SourceAccessBinding{HTTPS: &operatorv1alpha1.HTTPSSourceBinding{
					URL: httpsMigration().URL, Digest: downloadDigest, Format: operatorv1alpha1.HTTPSFormatTarGzip,
					Path: "db/migrations", ContentDigest: contentDigest,
				}},
				SourcePolicy: &operatorv1alpha1.SourcePolicyBinding{UID: "policy-uid", Digest: policyDigest},
			},
			want: &consolev1.Source{
				Kind: consolev1.SourceKind_SOURCE_KIND_HTTPS, Requested: httpsMigration().URL, Path: "db/migrations",
				Format: "TarGzip", Digest: downloadDigest, Resolved: downloadDigest, ContentDigest: contentDigest,
				Policy: &consolev1.SourcePolicy{Name: "team-sources", Uid: "policy-uid", Digest: policyDigest},
			},
			state: done, detail: "download sha256:555555555555",
		},
		{
			// The source moved from OCI to HTTPS and has not been read since.
			name: "an HTTPS source whose status still holds an OCI artifact",
			spec: operatorv1alpha1.SourceSpec{HTTPS: httpsMigration()},
			status: operatorv1alpha1.PtahMigrationStatus{
				Phase:    operatorv1alpha1.MigrationPhaseInSync,
				Artifact: &operatorv1alpha1.SourceAccessBinding{ResolvedReference: "oci://registry.example/team/migrations@" + ociDigest, Digest: ociDigest},
			},
			want: &consolev1.Source{
				Kind: consolev1.SourceKind_SOURCE_KIND_HTTPS, Requested: httpsMigration().URL, Path: "db/migrations",
				Format: "TarGzip", Digest: downloadDigest,
			},
			state: waiting,
		},
		{
			name:   "an HTTPS source being read",
			spec:   operatorv1alpha1.SourceSpec{HTTPS: httpsMigration()},
			status: operatorv1alpha1.PtahMigrationStatus{Phase: operatorv1alpha1.MigrationPhaseResolving},
			want: &consolev1.Source{
				Kind: consolev1.SourceKind_SOURCE_KIND_HTTPS, Requested: httpsMigration().URL, Path: "db/migrations",
				Format: "TarGzip", Digest: downloadDigest,
			},
			state: active,
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
		SpecReference: "oci://registry.example/team/schema:stable",
	})
	c.Assert(out.GetStages()[0].GetState(), qt.Equals, done)
}

// After a change of source and before the operator observes it, a schema's
// status still names the reference it resolved. What a further change
// replaces is the spec's reference, so the source carries both.
func TestAChangedSourceNamesTheSpecsReferenceBesideTheResolvedOne(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	next := "oci://registry.example/team/schema@sha256:" + strings.Repeat("4", 64)
	schema := schemaWith(condition("ArtifactResolved", metav1.ConditionTrue, "DigestPinned"))
	schema.Spec.Desired = operatorv1alpha1.SourceSpec{OCIRef: next}
	schema.Status.Source = operatorv1alpha1.SchemaSourceStatus{
		RequestedReference: "oci://registry.example/team/schema@" + ociDigest,
		ResolvedReference:  "oci://registry.example/team/schema@" + ociDigest, Digest: ociDigest,
	}
	migration := &operatorv1alpha1.PtahMigration{Spec: operatorv1alpha1.PtahMigrationSpec{
		Artifact: operatorv1alpha1.SourceSpec{OCIRef: next},
	}}
	migration.Status.Artifact = &operatorv1alpha1.SourceAccessBinding{
		ResolvedReference: "oci://registry.example/team/schema@" + ociDigest, Digest: ociDigest,
	}

	fromSchema := model.Schema(schema).GetSource()
	fromMigration := model.Migration(migration).GetSource()

	c.Assert(fromSchema.GetRequested(), qt.Equals, "oci://registry.example/team/schema@"+ociDigest)
	c.Assert(fromSchema.GetSpecReference(), qt.Equals, next)
	c.Assert(fromMigration.GetRequested(), qt.Equals, next)
	c.Assert(fromMigration.GetSpecReference(), qt.Equals, next)
	c.Assert(fromMigration.GetResolved(), qt.Equals, "oci://registry.example/team/schema@"+ociDigest)
}

// A plan computed from a ConfigMap, Git or HTTPS source is bound to the
// revision, the content and the policy, and the console reads its contract.
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
		{
			// The revision of an HTTPS plan is the digest of the URL and the
			// path, never of the download.
			name: "an HTTPS download", contract: 2, supported: true,
			source: &operatorv1alpha1.PlanSourceBinding{Kind: operatorv1alpha1.SourceKindHTTPS, Revision: policyDigest, ContentDigest: contentDigest},
			want:   &consolev1.PlanSource{Kind: consolev1.SourceKind_SOURCE_KIND_HTTPS, Revision: policyDigest, ContentDigest: contentDigest},
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

// Whether an operation's Job may exist follows the controller's own rule: a
// claim that began its dispatch, or that recorded a Job UID, may have one,
// whatever the phase says. A change of source refuses an Apply that may.
func TestAnOperationMayHaveItsJobOnceItsDispatchBegan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		started    bool
		jobUID     string
		dispatched bool
	}{
		{name: "a claim with no dispatch yet"},
		{name: "a dispatch that began", started: true, dispatched: true},
		{name: "a Job recorded", started: true, jobUID: "uid-job", dispatched: true},
		{name: "a Job UID alone", jobUID: "uid-job", dispatched: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			schema := schemaWith()
			schema.Status.ActiveOperation = &operatorv1alpha1.ActiveOperationStatus{
				Type: operatorv1alpha1.OperationApply, ID: "apply-1", DispatchStarted: test.started, JobUID: types.UID(test.jobUID),
			}
			migration := migrationWith(operatorv1alpha1.PtahMigrationStatus{ActiveOperation: &operatorv1alpha1.MigrationOperationStatus{
				Type: operatorv1alpha1.MigrationOperationApply, ID: "apply-1", DispatchStarted: test.started, JobUID: types.UID(test.jobUID),
			}})

			c.Assert(model.Schema(schema).GetActiveOperation().GetDispatched(), qt.Equals, test.dispatched)
			c.Assert(model.Migration(migration).GetActiveOperation().GetDispatched(), qt.Equals, test.dispatched)
		})
	}
}
