package model_test

import (
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/model"
)

var journalStart = time.Date(2026, 10, 24, 7, 0, 0, 0, time.UTC)

func at(minutes int) metav1.Time {
	return metav1.NewTime(journalStart.Add(time.Duration(minutes) * time.Minute))
}

func pb(minutes int) *timestamppb.Timestamp {
	return timestamppb.New(journalStart.Add(time.Duration(minutes) * time.Minute))
}

// The journal reaches the console as the operator kept it, entry for entry
// and in its order; the console makes up no event and drops none.
func TestTheJournalIsCarriedAsTheOperatorKeptIt(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	started, last := at(3), at(12)
	schema := &operatorv1alpha1.PtahSchema{}
	schema.Status.Journal = &operatorv1alpha1.LifecycleJournal{
		CoveredSince: at(0),
		Entries: []operatorv1alpha1.JournalEntry{
			{Type: operatorv1alpha1.JournalPlanPublished, At: at(1), Plan: "ptah-plan-a", Statements: 3, Destructive: true},
			{Type: operatorv1alpha1.JournalApprovalAccepted, At: at(2), Plan: "ptah-plan-a", Approval: "approve-a", Approver: "alice"},
			{Type: operatorv1alpha1.JournalApplyFinished, At: at(4), StartedAt: &started, Plan: "ptah-plan-a", Outcome: operatorv1alpha1.JournalSucceeded},
			{Type: operatorv1alpha1.JournalVerified, At: at(5), Plan: "ptah-plan-a", Outcome: operatorv1alpha1.JournalInSync},
			{Type: operatorv1alpha1.JournalOperationFailed, At: at(10), LastAt: &last, Count: 3, Operation: "Observe", Reason: "DatabaseUnreachable"},
		},
	}

	got := model.Schema(schema).GetJournal()

	c.Assert(got, qt.CmpEquals(protocmp.Transform()), &consolev1.Journal{
		CoveredSince: pb(0),
		Entries: []*consolev1.JournalEntry{
			{Type: "PlanPublished", At: pb(1), Plan: "ptah-plan-a", Statements: 3, Destructive: true},
			{Type: "ApprovalAccepted", At: pb(2), Plan: "ptah-plan-a", Approval: "approve-a", Approver: "alice"},
			{Type: "ApplyFinished", At: pb(4), StartedAt: pb(3), Plan: "ptah-plan-a", Outcome: "Succeeded"},
			{Type: "Verified", At: pb(5), Plan: "ptah-plan-a", Outcome: "InSync"},
			{Type: "OperationFailed", At: pb(10), LastAt: pb(12), Count: 3, Operation: "Observe", Reason: "DatabaseUnreachable"},
		},
	})
}

// A run that stopped says where in its entry and in the last run, and never
// with the engine's message.
func TestAStoppedRunSaysWhereItStopped(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	failure := &operatorv1alpha1.MigrationRunFailureStatus{Version: 2, Statement: 1, Statements: 3, SQLState: "23505"}
	finished := at(9)
	migration := &operatorv1alpha1.PtahMigration{}
	migration.Status.LastRun = &operatorv1alpha1.MigrationRunStatus{
		Outcome: operatorv1alpha1.MigrationRunOutcomePartial, StartedAt: at(8), FinishedAt: &finished,
		AppliedVersions: []int64{1}, Message: "migration 2 failed", Failure: failure,
	}
	migration.Status.Journal = &operatorv1alpha1.LifecycleJournal{
		CoveredSince: at(0),
		Entries: []operatorv1alpha1.JournalEntry{{
			Type: operatorv1alpha1.JournalRunFinished, At: at(9), StartedAt: &migration.Status.LastRun.StartedAt,
			Outcome: operatorv1alpha1.JournalPartial, AppliedVersions: 1, Failure: failure,
		}, {
			Type: operatorv1alpha1.JournalRunAcknowledged, At: at(20), Acknowledgment: "ack-run", AcknowledgedBy: "bob",
		}},
	}

	got := model.Migration(migration)

	want := &consolev1.RunFailure{Version: 2, Statement: 1, Statements: 3, SqlState: "23505"}
	c.Assert(got.GetLastRun().GetFailure(), qt.CmpEquals(protocmp.Transform()), want)
	c.Assert(got.GetJournal().GetEntries(), qt.CmpEquals(protocmp.Transform()), []*consolev1.JournalEntry{
		{Type: "RunFinished", At: pb(9), StartedAt: pb(8), Outcome: "Partial", AppliedVersions: 1, Failure: want},
		{Type: "RunAcknowledged", At: pb(20), Acknowledgment: "ack-run", AcknowledgedBy: "bob"},
	})
}

// An operator that keeps no journal gives none, which a reader tells from an
// empty one.
func TestNoJournalIsNotAnEmptyOne(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	c.Assert(model.Schema(&operatorv1alpha1.PtahSchema{}).GetJournal(), qt.IsNil)
	c.Assert(model.Migration(&operatorv1alpha1.PtahMigration{}).GetJournal(), qt.IsNil)
	empty := &operatorv1alpha1.PtahSchema{}
	empty.Status.Journal = &operatorv1alpha1.LifecycleJournal{CoveredSince: at(0)}
	c.Assert(model.Schema(empty).GetJournal(), qt.CmpEquals(protocmp.Transform()),
		&consolev1.Journal{CoveredSince: pb(0), Entries: make([]*consolev1.JournalEntry, 0)})
}
