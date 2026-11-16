package model

import (
	"strings"

	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// schemaSource is the schema's source as its spec names it and as its last
// resolution recorded it. The kind comes from the spec: a status written for
// an earlier kind of source leaves resolved empty until the new one is read.
func schemaSource(schema *operatorv1alpha1.PtahSchema) *consolev1.Source {
	spec, status := schema.Spec.Desired, schema.Status.Source
	out := &consolev1.Source{ResolvedAt: timestamp(status.ResolvedAt), Policy: sourcePolicy(spec, status.SourcePolicy)}
	switch {
	case spec.ConfigMap != nil:
		out.Kind, out.Requested = consolev1.SourceKind_SOURCE_KIND_CONFIG_MAP, spec.ConfigMap.Name
		if snapshot := status.Snapshot; snapshot != nil {
			out.Resolved, out.ContentDigest = snapshot.Name, snapshot.ContentDigest
		}
	case spec.Git != nil:
		out.Kind, out.Requested, out.Ref, out.Path = consolev1.SourceKind_SOURCE_KIND_GIT, spec.Git.URL, gitRef(spec.Git.Ref), spec.Git.Path
		if git := status.Git; git != nil {
			out.Resolved, out.ContentDigest = git.Commit, git.ContentDigest
		}
	case spec.HTTPS != nil:
		httpsSource(out, spec.HTTPS)
		if download := status.HTTPS; download != nil {
			out.Resolved, out.ContentDigest = download.Digest, download.ContentDigest
		}
	default:
		out.Kind, out.SpecReference = consolev1.SourceKind_SOURCE_KIND_OCI, spec.OCIRef
		out.Requested = firstOf(status.RequestedReference, spec.OCIRef)
		out.Resolved, out.Digest, out.Verified = status.ResolvedReference, status.Digest, status.Verified
		out.VerifiedAt = timestamp(status.VerifiedAt)
	}
	return out
}

// migrationSource is the same for a migration, whose resolution is the
// artifact binding every later operation reads.
func migrationSource(migration *operatorv1alpha1.PtahMigration, c conditions) *consolev1.Source {
	spec, artifact := migration.Spec.Artifact, migration.Status.Artifact
	out := &consolev1.Source{Policy: sourcePolicy(spec, migration.Status.SourcePolicy)}
	switch {
	case spec.ConfigMap != nil:
		out.Kind, out.Requested = consolev1.SourceKind_SOURCE_KIND_CONFIG_MAP, spec.ConfigMap.Name
		if artifact != nil && artifact.Snapshot != nil {
			out.Resolved, out.ContentDigest = artifact.Snapshot.Name, artifact.Snapshot.ContentDigest
		}
	case spec.Git != nil:
		out.Kind, out.Requested, out.Ref, out.Path = consolev1.SourceKind_SOURCE_KIND_GIT, spec.Git.URL, gitRef(spec.Git.Ref), spec.Git.Path
		if artifact != nil && artifact.Git != nil {
			out.Resolved, out.ContentDigest = artifact.Git.Commit, artifact.Git.ContentDigest
		}
	case spec.HTTPS != nil:
		httpsSource(out, spec.HTTPS)
		if artifact != nil && artifact.HTTPS != nil {
			out.Resolved, out.ContentDigest = artifact.HTTPS.Digest, artifact.HTTPS.ContentDigest
		}
	default:
		out.Kind, out.Requested, out.SpecReference = consolev1.SourceKind_SOURCE_KIND_OCI, spec.OCIRef, spec.OCIRef
		if artifact != nil {
			out.Resolved, out.Digest = artifact.ResolvedReference, artifact.Digest
			out.Verified = c.is(operatorv1alpha1.ConditionArtifactVerified, metav1.ConditionTrue)
		}
	}
	return out
}

// httpsSource is what the spec of an HTTPS source names: the download, the
// digest it is pinned to, and what it selects.
func httpsSource(out *consolev1.Source, spec *operatorv1alpha1.HTTPSSourceSpec) {
	out.Kind, out.Requested, out.Path = consolev1.SourceKind_SOURCE_KIND_HTTPS, spec.URL, spec.Path
	out.Format, out.Digest = string(spec.Format), spec.Digest
}

// unverifiedSource reports a source with no verification policy: a ConfigMap,
// a Git repository or an HTTPS download. The operator writes no
// ArtifactVerified for one, so its resolution is the whole of the Resolve
// stage.
func unverifiedSource(source *consolev1.Source) bool {
	switch source.GetKind() {
	case consolev1.SourceKind_SOURCE_KIND_CONFIG_MAP, consolev1.SourceKind_SOURCE_KIND_GIT, consolev1.SourceKind_SOURCE_KIND_HTTPS:
		return true
	default:
		return false
	}
}

// resolvedDetail is what a source without verification resolved to, for the
// Resolve stage.
func resolvedDetail(source *consolev1.Source) string {
	switch {
	case source.GetResolved() == "":
		return ""
	case source.GetKind() == consolev1.SourceKind_SOURCE_KIND_GIT:
		return "commit " + shortCommit(source.GetResolved())
	case source.GetKind() == consolev1.SourceKind_SOURCE_KIND_HTTPS:
		return "download " + shortDigest(source.GetResolved())
	default:
		return "snapshot " + source.GetResolved()
	}
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

// shortDigest keeps a digest's algorithm and the first twelve characters of
// its value, as a commit is shortened.
func shortDigest(digest string) string {
	algorithm, value, found := strings.Cut(digest, ":")
	if !found {
		return shortCommit(digest)
	}
	return algorithm + ":" + shortCommit(value)
}

// gitRef is the branch, tag or commit a Git source asks for.
func gitRef(ref operatorv1alpha1.GitRef) string {
	switch {
	case ref.Branch != "":
		return "branch " + ref.Branch
	case ref.Tag != "":
		return "tag " + ref.Tag
	case ref.Commit != "":
		return "commit " + ref.Commit
	default:
		return ""
	}
}

// sourcePolicy is the source policy the spec names and the one the last
// resolution was checked against. Either may be missing: a policy named and
// never yet read, or one recorded before the spec stopped naming it.
func sourcePolicy(spec operatorv1alpha1.SourceSpec, bound *operatorv1alpha1.SourcePolicyBinding) *consolev1.SourcePolicy {
	if spec.SourcePolicyFrom == nil && bound == nil {
		return nil
	}
	out := &consolev1.SourcePolicy{}
	if named := spec.SourcePolicyFrom; named != nil {
		out.Name = named.Name
	}
	if bound != nil {
		out.Uid, out.Digest = string(bound.UID), bound.Digest
	}
	return out
}

// planSource is what a plan computed from a ConfigMap, Git or HTTPS source is
// bound to. A plan computed from an OCI artifact has none.
func planSource(source *operatorv1alpha1.PlanSourceBinding) *consolev1.PlanSource {
	if source == nil {
		return nil
	}
	out := &consolev1.PlanSource{Revision: source.Revision, ContentDigest: source.ContentDigest}
	switch source.Kind {
	case operatorv1alpha1.SourceKindConfigMap:
		out.Kind = consolev1.SourceKind_SOURCE_KIND_CONFIG_MAP
	case operatorv1alpha1.SourceKindGit:
		out.Kind = consolev1.SourceKind_SOURCE_KIND_GIT
	case operatorv1alpha1.SourceKindHTTPS:
		out.Kind = consolev1.SourceKind_SOURCE_KIND_HTTPS
	}
	if policy := source.Policy; policy != nil {
		out.PolicyUid, out.PolicyDigest = string(policy.UID), policy.Digest
	}
	return out
}
