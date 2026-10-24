package model

import (
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// journal carries the lifecycle journal the operator keeps in status as it
// is: the console adds nothing to it and drops nothing from it. An operator
// that keeps none gives none.
func journal(kept *operatorv1alpha1.LifecycleJournal) *consolev1.Journal {
	if kept == nil {
		return nil
	}
	out := &consolev1.Journal{
		CoveredSince: timestamp(&kept.CoveredSince),
		Entries:      make([]*consolev1.JournalEntry, 0, len(kept.Entries)),
	}
	for i := range kept.Entries {
		entry := &kept.Entries[i]
		out.Entries = append(out.Entries, &consolev1.JournalEntry{
			Type:            string(entry.Type),
			At:              timestamp(&entry.At),
			StartedAt:       timestamp(entry.StartedAt),
			LastAt:          timestamp(entry.LastAt),
			Count:           entry.Count,
			Plan:            entry.Plan,
			Statements:      entry.Statements,
			Destructive:     entry.Destructive,
			Approval:        entry.Approval,
			Approver:        entry.Approver,
			Outcome:         string(entry.Outcome),
			Operation:       entry.Operation,
			Reason:          entry.Reason,
			AppliedVersions: entry.AppliedVersions,
			Failure:         runFailure(entry.Failure),
			Acknowledgment:  entry.Acknowledgment,
			AcknowledgedBy:  entry.AcknowledgedBy,
		})
	}
	return out
}

// runFailure is where a migration run stopped, without the engine's message.
func runFailure(failure *operatorv1alpha1.MigrationRunFailureStatus) *consolev1.RunFailure {
	if failure == nil {
		return nil
	}
	return &consolev1.RunFailure{
		Version: failure.Version, Statement: failure.Statement, Statements: failure.Statements,
		SqlState: failure.SQLState, ErrorNumber: failure.ErrorNumber,
	}
}
