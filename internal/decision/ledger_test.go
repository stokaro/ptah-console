package decision_test

import (
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/decision"
	"github.com/stokaro/ptah-console/internal/kube"
)

var (
	alice = kube.Principal{Mode: kube.ModeHosted, AccountID: "a-1", Context: "this-cluster", Generation: 1}
	bob   = kube.Principal{Mode: kube.ModeHosted, AccountID: "a-2", Context: "this-cluster", Generation: 1}
	plan  = decision.Key{Action: "approve-schema-plan", Namespace: "application", TargetUID: "uid-schema", Payload: "uid-plan/sha256:aa"}
)

// Sending the same decision again names the same object, so a retry after a
// lost response finds the first instead of creating a second.
func TestTheSameDecisionNamesTheSameObject(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ledger := decision.NewLedger()

	first := ledger.Begin(alice, plan, "ptah-console-")
	again := ledger.Begin(alice, plan, "ptah-console-")
	otherPlan := plan
	otherPlan.Payload = "uid-plan/sha256:bb"
	different := ledger.Begin(alice, otherPlan, "ptah-console-")

	c.Assert(again.Name, qt.Equals, first.Name)
	c.Assert(again.RequestID, qt.Equals, first.RequestID)
	c.Assert(different.Name, qt.Not(qt.Equals), first.Name)
	c.Assert(first.Name, qt.Matches, `ptah-console-[0-9a-f]{20}`)
}

// Two people deciding the same thing make two decisions, and neither can
// read the other's request.
func TestAnotherPersonsRequestIsRefused(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ledger := decision.NewLedger()
	mine := ledger.Begin(alice, plan, "ptah-console-")
	theirs := ledger.Begin(bob, plan, "ptah-console-")

	_, readOwn := ledger.Get(alice, mine.RequestID)
	_, readOther := ledger.Get(bob, mine.RequestID)
	_, readMissing := ledger.Get(alice, "0000")

	c.Assert(theirs.Name, qt.Not(qt.Equals), mine.Name)
	c.Assert(readOwn, qt.IsNil)
	c.Assert(readOther, qt.ErrorIs, decision.ErrNotYours)
	c.Assert(readMissing, qt.ErrorIs, decision.ErrNotFound)
}
