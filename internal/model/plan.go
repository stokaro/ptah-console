package model

import (
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"github.com/stokaro/ptah-operator/pkg/planread"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// SchemaPlan is a plan whose every check held, SQL included.
func SchemaPlan(read planread.Plan, selection consolev1.PlanSelection) *consolev1.SchemaPlan {
	out := SchemaPlanManifest(&read.Manifest, selection)
	out.Namespace, out.Schema, out.SchemaUid = read.Namespace, read.Schema, string(read.SchemaUID)
	out.UnrecordedPrivilegeChanges = read.UnrecordedPrivilegeChanges
	out.CompletedAt = timestamp(read.CompletedAt)
	out.SqlAccess = consolev1.SqlAccess_SQL_ACCESS_AVAILABLE
	for i, statement := range read.Statements {
		out.Statements = append(out.Statements, &consolev1.Statement{
			Index: int32(i), Sql: statement.SQL, Severity: statement.Severity, Reason: statement.Reason,
		})
	}
	out.Chunks = nil
	for _, chunk := range read.Chunks {
		out.Chunks = append(out.Chunks, &consolev1.PlanChunk{Name: chunk.Name, Index: chunk.Index, Size: chunk.Size, Digest: chunk.Digest})
	}
	return out
}

// SchemaPlanManifest is what the plan object says about itself, without SQL.
func SchemaPlanManifest(plan *operatorv1alpha1.PtahSchemaPlan, selection consolev1.PlanSelection) *consolev1.SchemaPlan {
	spec := plan.Spec
	out := &consolev1.SchemaPlan{
		Namespace:          plan.Namespace,
		Schema:             spec.SchemaRef.Name,
		SchemaUid:          string(spec.SchemaRef.UID),
		Selection:          selection,
		Name:               plan.Name,
		Uid:                string(plan.UID),
		Fingerprint:        spec.Fingerprint,
		ContentDigest:      spec.ContentDigest,
		Dialect:            spec.Dialect,
		ContractVersion:    spec.ContractVersion,
		ContractSupported:  planread.SupportedContract(spec.ContractVersion),
		Destructive:        spec.Destructive,
		PrivilegeChanges:   privilegeChanges(spec.PrivilegeChanges),
		StatementCount:     spec.StatementCount,
		ArtifactDigest:     spec.ArtifactDigest,
		PolicyFingerprint:  spec.PolicyFingerprint,
		ExecutionBindingId: spec.ExecutionBindingID,
		PtahVersion:        spec.PtahVersion,
		CreatedAt:          timestamp(&plan.CreationTimestamp),
		Source:             planSource(spec.Source),
	}
	for _, chunk := range spec.Chunks {
		out.Chunks = append(out.Chunks, &consolev1.PlanChunk{Name: chunk.Name, Index: chunk.Index, Size: chunk.Size, Digest: chunk.Digest})
	}
	return out
}

// MigrationPlan is a migration plan: files and checksums, never SQL.
func MigrationPlan(plan *operatorv1alpha1.PtahMigrationPlan) *consolev1.MigrationPlan {
	spec := plan.Spec
	out := &consolev1.MigrationPlan{
		Namespace:          plan.Namespace,
		Migration:          spec.MigrationRef.Name,
		MigrationUid:       string(spec.MigrationRef.UID),
		Name:               plan.Name,
		Uid:                string(plan.UID),
		Fingerprint:        spec.Fingerprint,
		ContractVersion:    spec.ContractVersion,
		HistoryFingerprint: spec.HistoryFingerprint,
		CurrentVersion:     spec.CurrentVersion,
		ArtifactDigest:     spec.ArtifactDigest,
		PolicyFingerprint:  spec.PolicyFingerprint,
		CreatedAt:          timestamp(&spec.CreatedAt),
		Source:             planSource(spec.Source),
	}
	for _, migration := range spec.Migrations {
		out.Migrations = append(out.Migrations, &consolev1.PlannedMigration{
			Version: migration.Version, VersionKey: migration.VersionKey, Description: migration.Description,
			Checksum: migration.Checksum, Checkpoint: migration.Checkpoint, TransactionMode: migration.TransactionMode,
		})
	}
	return out
}
